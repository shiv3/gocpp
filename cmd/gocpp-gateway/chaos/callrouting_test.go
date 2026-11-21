//go:build !short

package chaos

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	registryredis "github.com/shiv3/gocpp/addons/registry-redis"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/gateway/routepolicy"
	"github.com/stretchr/testify/require"
)

// Chaos around CSMS-initiated calls and held sessions during a canary
// (docs/design/gateway.md 5.1.5, 5.4, 5.5 phase 7a).

// resetCharger connects a charger that answers CSMS Reset calls: it counts them,
// reports each arrival on entered, and holds the answer until gate is closed (a
// nil gate answers at once).
func resetCharger(t *testing.T, ws, id string, executed *atomic.Int64, entered chan<- struct{}, gate <-chan struct{}) *charger {
	t.Helper()
	ch := &charger{disconnects: &int32Counter{}}
	ch.Client = cp.NewClient(id, "ws://"+ws+"/ocpp/"+id, cp.WithSubProtocols("ocpp1.6"),
		cp.WithOnDisconnect(func(error) { ch.disconnects.inc() }), cp.WithCallTimeout(20*time.Second))
	require.NoError(t, cp.On(ch.Client, resetMsg, func(ctx context.Context, _ resetReq) (resetResp, error) {
		executed.Add(1)
		if entered != nil {
			select {
			case entered <- struct{}{}:
			default:
			}
		}
		if gate != nil {
			select {
			case <-gate:
			case <-ctx.Done():
			}
		}
		return resetResp{Status: "Accepted"}, nil
	}))
	ctx, cancel := ctxT(5 * time.Second)
	defer cancel()
	require.NoError(t, ch.Connect(ctx))
	t.Cleanup(ch.Close)
	return ch
}

// backendCall asks a test backend to issue a CSMS Reset to the charger and returns
// its one-line verdict ("OK Accepted" or "ERR <sent> <error>").
func backendCall(admin, cpID string) string {
	cl := &http.Client{Timeout: 40 * time.Second}
	resp, err := cl.Get("http://" + admin + "/call?cp=" + cpID)
	if err != nil {
		return "ERR http " + err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(b))
}

func routePool(t *testing.T, reg *registryredis.Registry, cpID string) string {
	t.Helper()
	s, ok, err := reg.Lookup(context.Background(), cpID)
	if err != nil || !ok {
		return ""
	}
	return s.Meta["route.pool"]
}

func newReg(t *testing.T, addr string) *registryredis.Registry {
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	return registryredis.New(rdb)
}

// A CSMS call reaches the charger whichever pool it is issued from: with the
// charger routed to the canary, a call from the canary backend and one from the
// stable backend both arrive; after the rollback, with the charger back on the
// default pool, both still do. The registry copy of the decision follows.
func TestChaos_CrossPoolCallsReachTheCharger(t *testing.T) {
	mr := miniredis.RunT(t)
	g := startGateway(t, "gw-1", mr.Addr())
	adminD, adminC := freeAddr(t), freeAddr(t)
	startPoolBackend(t, "be-d", "", mr.Addr(), g.link, "-admin", adminD)
	startPoolBackend(t, "be-c", "canary", mr.Addr(), g.link, "-admin", adminC)
	require.Eventually(t, g.ready, 5*time.Second, 50*time.Millisecond)
	ctl := controller(t, mr.Addr())
	reg := newReg(t, mr.Addr())
	waitLeases(t, ctl)
	var executed atomic.Int64
	c := resetCharger(t, g.ws, "CP_1", &executed, nil, nil)

	moveTo := func(p routepolicy.Policy, pool string) {
		rev, err := ctl.Put(context.Background(), p, routepolicy.PutOptions{})
		require.NoError(t, err)
		_, err = ctl.WaitConverged(context.Background(), rev, 5*time.Second)
		require.NoError(t, err)
		require.Eventually(t, func() bool { _ = hb(c); return routePool(t, reg, "CP_1") == pool }, 5*time.Second, 50*time.Millisecond)
	}
	callBoth := func() {
		before := executed.Load()
		require.Equal(t, "OK Accepted", backendCall(adminC, "CP_1"), "call from the canary backend")
		require.Equal(t, "OK Accepted", backendCall(adminD, "CP_1"), "call from the stable backend")
		require.Equal(t, before+2, executed.Load(), "both reached the charger exactly once")
	}
	moveTo(canaryRule(nil), "canary")
	callBoth()
	moveTo(routepolicy.Policy{}, "default")
	callBoth()
	require.True(t, c.IsConnected())
	require.Zero(t, c.disconnects.get())
}

// The calling canary backend dies after the frame reached the charger. The
// charger's answer is not lost: it goes to the canary pool's remaining member as
// an orphan result (or, when the pool is empty, to the default pool), and the
// gateway counts it.
func TestChaos_OrphanResultWhenCallingCanaryDies(t *testing.T) {
	if linkKind == "nats" {
		// Over NATS the gateway does not learn that the requester died (the answer
		// is published to a reply subject nobody reads), so there is no orphan
		// path to exercise.
		t.Skip("orphan results are a gRPC link feature: a NATS requester's death is not visible to the gateway")
	}
	for _, tc := range []struct {
		name         string
		secondCanary bool
		outcome      string
	}{{"canary pool has another member", true, "delivered"}, {"canary pool empty", false, "fallback"}} {
		t.Run(tc.name, func(t *testing.T) {
			mr := miniredis.RunT(t)
			g := startGateway(t, "gw-1", mr.Addr())
			def := startPoolBackend(t, "be-d", "", mr.Addr(), g.link)
			admin := freeAddr(t)
			c1 := startPoolBackend(t, "be-c1", "canary", mr.Addr(), g.link, "-admin", admin)
			var c2 *proc
			if tc.secondCanary {
				c2 = startPoolBackend(t, "be-c2", "canary", mr.Addr(), g.link)
			}
			require.Eventually(t, g.ready, 5*time.Second, 50*time.Millisecond)
			ctl := controller(t, mr.Addr())
			waitLeases(t, ctl)
			rev, err := ctl.Put(context.Background(), canaryRule(nil), routepolicy.PutOptions{})
			require.NoError(t, err)
			_, err = ctl.WaitConverged(context.Background(), rev, 5*time.Second)
			require.NoError(t, err)

			var executed atomic.Int64
			entered, gate := make(chan struct{}, 1), make(chan struct{})
			c := resetCharger(t, g.ws, "CP_1", &executed, entered, gate)
			require.NoError(t, hb(c))

			verdict := make(chan string, 1)
			go func() { verdict <- backendCall(admin, "CP_1") }()
			select {
			case <-entered: // the frame is on the charger
			case <-time.After(10 * time.Second):
				t.Fatal("the call never reached the charger")
			}
			c1.kill9() // the caller is gone
			<-verdict
			// The gateway has to have seen the caller go before the charger answers:
			// an answer that races the gateway noticing the broken stream is written
			// to the dead stream and is not an orphan (no one can tell it from a
			// reply the caller will read). The outcome of the abandoned call shows it.
			require.Eventually(t, func() bool {
				return strings.Contains(grepLines(metrics(t, g), "downstream_calls_total"), `outcome="no_response"`)
			}, 10*time.Second, 20*time.Millisecond, "the gateway did not notice the caller die")
			close(gate) // the charger answers now

			want, other := def, c2
			if tc.secondCanary {
				want, other = c2, def
			}
			require.Eventually(t, func() bool { return want.count("ORPHAN") == 1 }, 10*time.Second, 50*time.Millisecond,
				"the orphan result did not arrive")
			if other != nil {
				require.Zero(t, other.count("ORPHAN"), "the result went to the wrong pool")
			}
			require.Eventually(t, func() bool {
				return regexOrphan(metrics(t, g), "canary", tc.outcome)
			}, 5*time.Second, 100*time.Millisecond, "metric: %s", grepLines(metrics(t, g), "orphan_results"))
			require.EqualValues(t, 1, executed.Load(), "the charger executed the call once")
			require.True(t, c.IsConnected())
			require.Zero(t, c.disconnects.get())
		})
	}
}

// A busy session (gocpp.busy set by its backend, rule `hold: busy`) is held on
// its pool when the canary percentage goes up and moves once it is idle again; a
// rollback moves it at once, ignoring the hold.
func TestChaos_HeldSessionDuringPromotionAndRollback(t *testing.T) {
	mr := miniredis.RunT(t)
	g := startGateway(t, "gw-1", mr.Addr())
	def := startPoolBackend(t, "be-d", "", mr.Addr(), g.link)
	can := startPoolBackend(t, "be-c", "canary", mr.Addr(), g.link)
	require.Eventually(t, g.ready, 5*time.Second, 50*time.Millisecond)
	ctl := controller(t, mr.Addr())
	waitLeases(t, ctl)
	put := func(percent float64) uint64 {
		rev, err := ctl.Put(context.Background(), canaryRule(func(r *routepolicy.Rule) { r.Match.Percent = &percent; r.Hold = "busy" }), routepolicy.PutOptions{})
		require.NoError(t, err)
		_, err = ctl.WaitConverged(context.Background(), rev, 5*time.Second)
		require.NoError(t, err)
		return rev
	}
	boot := func(c *charger, vendor string) {
		ctx, cancel := ctxT(10 * time.Second)
		defer cancel()
		require.NoError(t, c.boot(ctx, vendor))
	}

	// Promotion. The charger is on the default pool (0%) and becomes busy there.
	r1 := put(0)
	c := newCharger(t, g.ws, "CP_1")
	boot(c, "busy")
	require.Equal(t, 1, def.count("HANDLED BootNotification be-d CP_1 busy"))
	put(100)
	for i := 0; i < 3; i++ {
		require.NoError(t, hb(c))
	}
	require.Equal(t, 3, def.count("HANDLED Heartbeat be-d CP_1"), "busy: held on the default pool across the increase")
	require.Zero(t, can.count("HANDLED"))
	boot(c, "idle") // still held while this message is handled, and it clears the flag
	require.NoError(t, hb(c))
	require.Equal(t, 1, can.count("HANDLED Heartbeat be-c CP_1"), "idle again: the new percentage applies")

	// Rollback. Busy on the canary; a plain revision back to 0% holds it there,
	// the rollback (which ignores the hold) moves it.
	boot(c, "busy")
	require.Equal(t, 1, can.count("HANDLED BootNotification be-c CP_1 busy"))
	put(0)
	require.NoError(t, hb(c))
	require.Equal(t, 2, can.count("HANDLED Heartbeat be-c CP_1"), "held on the canary by hold: busy")
	rev, restored, err := ctl.Rollback(context.Background(), r1)
	require.NoError(t, err)
	require.Equal(t, r1, restored)
	_, err = ctl.WaitConverged(context.Background(), rev, 5*time.Second)
	require.NoError(t, err)
	defBefore := def.count("HANDLED Heartbeat be-d CP_1")
	require.NoError(t, hb(c))
	require.Equal(t, defBefore+1, def.count("HANDLED Heartbeat be-d CP_1"), "rollback ignores the hold")
	require.Equal(t, 2, can.count("HANDLED Heartbeat be-c CP_1"))
	require.True(t, c.IsConnected())
	require.Zero(t, c.disconnects.get())
}

func grepLines(s, sub string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, sub) && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// regexOrphan reports whether the gateway's metrics hold a nonzero
// orphan_results_total series for the pool and outcome.
func regexOrphan(body, pool, outcome string) bool {
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(l, "gocpp_gateway_orphan_results_total{") &&
			strings.Contains(l, fmt.Sprintf(`outcome=%q`, outcome)) && strings.Contains(l, fmt.Sprintf(`pool=%q`, pool)) &&
			!strings.HasSuffix(l, " 0") {
			return true
		}
	}
	return false
}
