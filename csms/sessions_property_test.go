package csms

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/storage/memory"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// fakeSock is a charge point socket as the session manager sees it.
type fakeSock struct {
	cp       string
	gw       int
	sess     storage.Session
	dead     bool
	released bool
}

func (f *fakeSock) holderID() string                   { return f.cp }
func (f *fakeSock) holderSession() storage.Session     { return f.sess }
func (f *fakeSock) setHolderSession(s storage.Session) { f.sess = s }
func (f *fakeSock) alive() bool                        { return !f.dead }
func (f *fakeSock) displace()                          { f.dead = true }

type simClock struct{ t time.Time }

func (c *simClock) now() time.Time { return c.t }

// TestSessionRegistry_Property drives several gateways over one registry with
// arbitrary interleavings of connects, delayed claims, delayed releases,
// reconcile passes, heartbeats, clock jumps and crashes. After the system
// settles (live gateways heartbeat, every pending claim and release lands, one
// reconcile pass runs on each live gateway) every live socket must be routed
// to, and after lease expiry nothing may route to a crashed gateway.
func TestSessionRegistry_Property(t *testing.T) {
	const (
		gateways = 3
		leaseTTL = 15 * time.Second
	)
	cps := []string{"CP_1", "CP_2", "CP_3"}
	rapid.Check(t, func(rt *rapid.T) {
		ctx := context.Background()
		clk := &simClock{t: time.Unix(10_000, 0)}
		reg := memory.NewSessionRegistry(memory.WithClock(clk.now))
		log := slog.New(slog.NewTextHandler(io.Discard, nil))

		var socks []*fakeSock
		crashed := make([]bool, gateways)
		mgrs := make([]*sessionManager, gateways)
		for g := range mgrs {
			mgrs[g] = newSessionManager(reg, SessionConfig{GatewayID: fmt.Sprintf("G%d", g), LeaseTTL: leaseTTL}, log, true,
				func() []sessionHolder {
					var hs []sessionHolder
					for _, s := range socks {
						if s.gw == g && !s.dead {
							hs = append(hs, s)
						}
					}
					return hs
				})
			mgrs[g].heartbeat(ctx)
		}
		liveSocks := func() []*fakeSock {
			var out []*fakeSock
			for _, s := range socks {
				if !s.dead {
					out = append(out, s)
				}
			}
			return out
		}

		steps := rapid.IntRange(1, 60).Draw(rt, "steps")
		for i := 0; i < steps; i++ {
			g := rapid.IntRange(0, gateways-1).Draw(rt, "gw")
			cp := rapid.SampledFrom(cps).Draw(rt, "cp")
			switch rapid.IntRange(0, 7).Draw(rt, "op") {
			case 0: // a socket is established; its claim is still in flight
				if crashed[g] {
					continue
				}
				for _, s := range socks { // local replacement on the same gateway
					if s.gw == g && s.cp == cp && !s.dead {
						s.dead = true
					}
				}
				socks = append(socks, &fakeSock{cp: cp, gw: g, sess: storage.Session{CPID: cp, GatewayID: fmt.Sprintf("G%d", g), Boot: mgrs[g].boot}})
			case 1: // a pending claim lands (possibly after newer sockets claimed)
				for _, s := range socks {
					if s.gw == g && s.cp == cp && !s.dead && s.sess.Epoch == 0 && !crashed[g] {
						s.sess = mgrs[g].claim(ctx, s.sess)
						break
					}
				}
			case 2: // a socket ends
				for _, s := range socks {
					if s.gw == g && s.cp == cp && !s.dead {
						s.dead = true
						break
					}
				}
			case 3: // a delayed release of an ended socket lands
				for _, s := range socks {
					if s.dead && !s.released && s.gw == g && s.cp == cp && !crashed[g] {
						s.released = true
						mgrs[g].release(ctx, s.sess)
						break
					}
				}
			case 4:
				if !crashed[g] {
					mgrs[g].reconcile(ctx, false)
				}
			case 5:
				if !crashed[g] {
					mgrs[g].heartbeat(ctx)
				}
			case 6:
				clk.t = clk.t.Add(time.Duration(rapid.IntRange(1, 20).Draw(rt, "secs")) * time.Second)
			case 7: // crash: sockets vanish without any release; the lease stops
				if rapid.Bool().Draw(rt, "really") {
					crashed[g] = true
					for _, s := range socks {
						if s.gw == g {
							s.dead = true
						}
					}
				}
			}
		}

		// Settle.
		for g, m := range mgrs {
			if !crashed[g] {
				m.heartbeat(ctx)
			}
		}
		for _, s := range socks {
			if !s.dead && s.sess.Epoch == 0 {
				s.sess = mgrs[s.gw].claim(ctx, s.sess)
			}
		}
		for _, s := range socks {
			if s.dead && !s.released && !crashed[s.gw] {
				s.released = true
				mgrs[s.gw].release(ctx, s.sess)
			}
		}
		for g, m := range mgrs {
			if !crashed[g] {
				m.reconcile(ctx, false)
			}
		}

		live := map[string][]*fakeSock{}
		for _, s := range liveSocks() {
			live[s.cp] = append(live[s.cp], s)
		}
		for _, cp := range cps {
			got, found, err := reg.Lookup(ctx, cp)
			require.NoError(rt, err)
			ss := live[cp]
			if len(ss) == 0 {
				continue // nothing to route to; a record may linger until a lease or release removes it
			}
			require.Len(rt, ss, 1, "a superseded socket survived one reconcile pass: %+v", ss)
			require.True(rt, found, "live socket %+v unrouted", ss[0].sess)
			require.Equal(rt, ss[0].sess.Epoch, got.Epoch)
			require.Equal(rt, ss[0].sess.GatewayID, got.GatewayID)
		}

		// After lease expiry nothing routes to a crashed gateway.
		clk.t = clk.t.Add(leaseTTL + time.Second)
		for g, m := range mgrs {
			if !crashed[g] {
				m.heartbeat(ctx)
			}
		}
		for _, cp := range cps {
			got, found, err := reg.Lookup(ctx, cp)
			require.NoError(rt, err)
			if found {
				var gi int
				_, _ = fmt.Sscanf(got.GatewayID, "G%d", &gi)
				require.False(rt, crashed[gi], "routes to crashed gateway: %+v", got)
			}
		}
	})
}
