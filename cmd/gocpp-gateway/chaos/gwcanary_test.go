//go:build !short

package chaos

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	registryredis "github.com/shiv3/gocpp/addons/registry-redis"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/gateway"
	"github.com/stretchr/testify/require"
)

// Gateway canary chaos (docs/design/gateway.md 5.2): three real gateway
// processes behind a small L4 load balancer that, like a cloud passthrough
// balancer with a readiness check, spreads new connections over the gateways
// that report ready (and fails open when none does). A canary gateway with a soft
// cap is filled by shedding, then the fleet is rebalanced, all with gocppctl.

const adminToken = "tok"

// l4 is the load balancer: ready gateways only, fail open.
type l4 struct {
	l      net.Listener
	mu     sync.Mutex
	member []*gw
	ready  map[*gw]bool
	conns  []net.Conn
	closed atomic.Bool
}

func newL4(t *testing.T) *l4 {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	b := &l4{l: l, ready: map[*gw]bool{}}
	t.Cleanup(func() {
		b.closed.Store(true)
		_ = l.Close()
		b.mu.Lock()
		for _, c := range b.conns {
			_ = c.Close()
		}
		b.mu.Unlock()
	})
	go b.poll()
	go b.accept()
	return b
}

func (b *l4) Addr() string { return b.l.Addr().String() }

func (b *l4) add(g *gw) {
	b.mu.Lock()
	b.member = append(b.member, g)
	b.mu.Unlock()
}

func (b *l4) poll() {
	for !b.closed.Load() {
		b.mu.Lock()
		members := append([]*gw(nil), b.member...)
		b.mu.Unlock()
		state := map[*gw]bool{}
		for _, g := range members {
			state[g] = g.ready()
		}
		b.mu.Lock()
		b.ready = state
		b.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
}

func (b *l4) pick() *gw {
	b.mu.Lock()
	defer b.mu.Unlock()
	var up []*gw
	for _, g := range b.member {
		if b.ready[g] {
			up = append(up, g)
		}
	}
	if len(up) == 0 {
		up = b.member // fail open
	}
	if len(up) == 0 {
		return nil
	}
	return up[rand.IntN(len(up))]
}

func (b *l4) accept() {
	for {
		c, err := b.l.Accept()
		if err != nil {
			return
		}
		g := b.pick()
		if g == nil {
			_ = c.Close()
			continue
		}
		up, err := net.Dial("tcp", g.ws)
		if err != nil {
			_ = c.Close()
			continue
		}
		b.mu.Lock()
		b.conns = append(b.conns, c, up)
		b.mu.Unlock()
		go func() { _, _ = io.Copy(up, c); _ = up.Close() }()
		go func() { _, _ = io.Copy(c, up); _ = c.Close() }()
	}
}

// fleet is the set of chargers; it records when each (re)connected.
type fleet struct {
	mu        sync.Mutex
	connected map[string]bool
	connects  []time.Time // every connect after the first of its charger
	first     map[string]bool
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	resets    atomic.Int64 // CSMS Reset calls the chargers executed
}

func (f *fleet) up() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, v := range f.connected {
		if v {
			n++
		}
	}
	return n
}

// maxPerSecond is the highest number of reconnects inside any one-second window.
func (f *fleet) maxPerSecond() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	ts := append([]time.Time(nil), f.connects...)
	sort.Slice(ts, func(i, j int) bool { return ts[i].Before(ts[j]) })
	best := 0
	for i := range ts {
		n := 0
		for j := i; j < len(ts) && ts[j].Sub(ts[i]) < time.Second; j++ {
			n++
		}
		best = max(best, n)
	}
	return best
}

func (f *fleet) resetReconnects() { f.mu.Lock(); f.connects = nil; f.mu.Unlock() }

func startFleet(t *testing.T, lbAddr string, n int) *fleet {
	f := &fleet{connected: map[string]bool{}, first: map[string]bool{}}
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("CP_%03d", i)
		c := cp.NewClient(id, "ws://"+lbAddr+"/ocpp/"+id, cp.WithSubProtocols("ocpp1.6"),
			cp.WithReconnectBackoff(100*time.Millisecond, time.Second, 0),
			cp.WithOnConnect(func() {
				f.mu.Lock()
				f.connected[id] = true
				if f.first[id] {
					f.connects = append(f.connects, time.Now())
				}
				f.first[id] = true
				f.mu.Unlock()
			}),
			cp.WithOnDisconnect(func(error) { f.mu.Lock(); f.connected[id] = false; f.mu.Unlock() }))
		_ = cp.On(c, resetMsg, func(context.Context, resetReq) (resetResp, error) {
			f.resets.Add(1)
			return resetResp{Status: "Accepted"}, nil
		})
		f.wg.Add(1)
		go func() { defer f.wg.Done(); _ = c.Run(ctx) }()
	}
	t.Cleanup(func() { cancel(); f.wg.Wait() })
	return f
}

type canaryRig struct {
	mr       *miniredis.Miniredis
	reg      *registryredis.Registry
	gws      []*gw
	lb       *l4
	fleet    *fleet
	redisSrv string
}

// newCanaryRig starts gw-0, gw-1 (serving) and gw-2 (the canary: soft cap, not
// yet in the balancer), one backend on all three, and n chargers on gw-0/gw-1.
func newCanaryRig(t *testing.T, n, cap int, redisForGateways func(real string) string) *canaryRig {
	t.Helper()
	if linkKind == "nats" {
		t.Skip("gateway canary scenarios do not depend on the link")
	}
	r := &canaryRig{mr: miniredis.RunT(t)}
	rdb := redis.NewClient(&redis.Options{Addr: r.mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	r.reg = registryredis.New(rdb)
	gwRedis := r.mr.Addr()
	if redisForGateways != nil {
		gwRedis = redisForGateways(gwRedis)
	}
	var links []string
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("gw-%d", i)
		tokFile := filepath.Join(t.TempDir(), "admin-token")
		require.NoError(t, os.WriteFile(tokFile, []byte(adminToken), 0o600))
		extra := []string{"--admin-token-file", tokFile, "--accept-rate", "0"}
		g := &gw{id: id, ws: freeAddr(t), link: freeAddr(t), admin: freeAddr(t), api: freeAddr(t)}
		extra = append(extra, "--advertise-admin", g.api)
		if i == 2 {
			extra = append(extra, "--soft-max-sessions", fmt.Sprint(cap))
		}
		args := []string{"serve", "--id", id, "--listen", g.ws, "--link-listen", g.link, "--link-insecure", "--metrics-listen", g.admin, "--admin", g.api,
			"--subprotocols", "ocpp1.6", "--upstream-wait", "1s", "--drain-window", "2s",
			"--lease-ttl", "3s", "--lease-heartbeat", "500ms", "--reconcile-interval", "1s", "--claim-timeout", "500ms",
			"--link-keepalive", "1s", "--log-level", "warn", "--redis-addr", gwRedis}
		g.proc = start(t, "gocpp-gateway:"+id, append(args, extra...)...)
		require.Eventually(t, func() bool { return httpOK("http://" + g.admin + "/healthz") }, 10*time.Second, 50*time.Millisecond)
		r.gws = append(r.gws, g)
		links = append(links, g.link)
	}
	startBackend(t, "be-1", r.mr.Addr(), links...)
	for _, g := range r.gws {
		require.Eventually(t, g.ready, 10*time.Second, 50*time.Millisecond)
	}
	r.lb = newL4(t)
	r.lb.add(r.gws[0])
	r.lb.add(r.gws[1])
	r.fleet = startFleet(t, r.lb.Addr(), n)
	require.Eventually(t, func() bool { return r.fleet.up() == n }, 20*time.Second, 50*time.Millisecond)
	return r
}

func ctl(t *testing.T, redisAddr string, args ...string) (string, error) {
	t.Helper()
	full := append([]string{args[0], args[1], "--redis-addr", redisAddr}, args[2:]...)
	cmd := exec.Command(filepath.Join(binDir, "gocppctl"), full...)
	cmd.Env = append(os.Environ(), "GOCPPCTL_ADMIN_TOKEN="+adminToken)
	out, err := cmd.CombinedOutput()
	t.Logf("gocppctl %s\n%s", strings.Join(full[:2], " "), out)
	return string(out), err
}

// sessions per gateway as the registry's leases report them (they refresh every
// 500 ms), so wait for settling before reading.
func (r *canaryRig) counts(t *testing.T) map[string]int {
	gws, err := r.reg.Gateways(context.Background())
	require.NoError(t, err)
	out := map[string]int{}
	for _, g := range gws {
		out[g.ID] = g.Sessions
	}
	return out
}

func (r *canaryRig) settled(t *testing.T, total int, within time.Duration) map[string]int {
	var c map[string]int
	require.Eventually(t, func() bool {
		c = r.counts(t)
		sum := 0
		for _, n := range c {
			sum += n
		}
		return sum == total
	}, within, 200*time.Millisecond, "sessions per gateway never added up to %d: %v", total, c)
	return c
}

func (r *canaryRig) epochs(t *testing.T, n int) map[string]uint64 {
	out := map[string]uint64{}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("CP_%03d", i)
		s, ok, err := r.reg.Lookup(context.Background(), id)
		require.NoError(t, err)
		if ok {
			out[id] = s.Epoch
		}
	}
	return out
}

// Canary pod fill, completion and rebalance: the canary takes about its share
// (the soft cap) from the displaced sessions, the reconnect rate stays at the
// configured pace, epochs only grow, and after the cap is lifted the rebalance
// brings every gateway to its fair share.
func TestChaos_GatewayCanaryFillAndRebalance(t *testing.T) {
	const n, capN = 120, 12
	r := newCanaryRig(t, n, capN, nil)
	r.settled(t, n, 10*time.Second)
	before := r.epochs(t, n)
	require.Len(t, before, n)
	r.fleet.resetReconnects()
	calls := startCallLoad(t, r, n)

	// The canary joins the balancer and the old gateways shed into it.
	r.lb.add(r.gws[2])
	for _, id := range []string{"gw-0", "gw-1"} {
		out, err := ctl(t, r.mr.Addr(), "gateway", "shed", "--gateway", id, "--count", "16", "--min-age", "-1s", "--rate", "20")
		require.NoError(t, err, out)
	}
	require.Eventually(t, func() bool { return r.fleet.up() == n }, 20*time.Second, 50*time.Millisecond)
	c := r.settled(t, n, 10*time.Second)
	require.GreaterOrEqual(t, c["gw-2"], 1, "the canary received sessions: %v", c)
	require.LessOrEqual(t, c["gw-2"], capN+4, "soft cap respected up to the readiness lag: %v", c)
	require.False(t, r.gws[2].ready() && c["gw-2"] >= capN+4)

	// Pace: never more reconnects per second than the shed rate allows.
	require.LessOrEqual(t, r.fleet.maxPerSecond(), 30, "reconnect rate above the configured pace")

	// Complete: lift the cap and rebalance.
	out, err := ctl(t, r.mr.Addr(), "gateway", "cap", "--gateway", "gw-2", "--off")
	require.NoError(t, err, out)
	out, err = ctl(t, r.mr.Addr(), "gateway", "rebalance", "--min-age", "-1s", "--rate", "20", "--settle", "3s", "--rounds", "6", "--tolerance", "1.15")
	require.NoError(t, err, out)
	require.Eventually(t, func() bool { return r.fleet.up() == n }, 20*time.Second, 50*time.Millisecond)
	c = r.settled(t, n, 10*time.Second)
	for id, got := range c {
		require.InDelta(t, n/3, got, 10, "gateway %s is far from its fair share (the CLI judged it balanced within 15%%): %v", id, c)
	}

	// Epochs only ever grow per charger.
	after := r.epochs(t, n)
	require.Len(t, after, n)
	moved := 0
	for id, e := range after {
		require.GreaterOrEqual(t, e, before[id], "epoch of %s went backwards", id)
		if e > before[id] {
			moved++
		}
	}
	require.Positive(t, moved)
	require.Less(t, r.fleet.maxPerSecond(), 31)

	// No call that the gateways reported as NotSent was lost: every logical call
	// issued while sessions moved ended up on its charger (after the retries a
	// NotSent permits), and a NotSent call never ran.
	calls.stop()
	t.Logf("calls: %d logical, %d answered, %d written without an answer, %d NotSent retries, %d lost; chargers executed %d",
		calls.logical.Load(), calls.answered.Load(), calls.noResponse.Load(), calls.notSentSeen.Load(), calls.lost.Load(), r.fleet.resets.Load())
	require.Positive(t, calls.logical.Load())
	require.Zero(t, calls.lost.Load(), "calls that stayed NotSent after retries: %s", calls.detail())
	require.GreaterOrEqual(t, r.fleet.resets.Load(), calls.answered.Load(), "an answered call was not executed")
	require.LessOrEqual(t, r.fleet.resets.Load(), calls.answered.Load()+calls.noResponse.Load(),
		"a call reported NotSent was executed anyway (answered %d, no response %d, executed %d)",
		calls.answered.Load(), calls.noResponse.Load(), r.fleet.resets.Load())
}

// callLoad issues CSMS Reset calls to random chargers from a processing-tier
// caller while the fleet moves, retrying only what the library reports as NotSent.
type callLoad struct {
	cancel                                           context.CancelFunc
	done                                             chan struct{}
	logical, answered, noResponse, lost, notSentSeen atomic.Int64
	mu                                               sync.Mutex
	lastLost                                         string
}

func (c *callLoad) detail() string { c.mu.Lock(); defer c.mu.Unlock(); return c.lastLost }

func (c *callLoad) stop() { c.cancel(); <-c.done }

func startCallLoad(t *testing.T, r *canaryRig, n int) *callLoad {
	var links []string
	for _, g := range r.gws {
		links = append(links, g.link)
	}
	caller := newCaller(t, r.mr.Addr(), links...)
	ctx, cancel := context.WithCancel(context.Background())
	c := &callLoad{cancel: cancel, done: make(chan struct{})}
	t.Cleanup(func() { cancel(); <-c.done })
	go func() {
		defer close(c.done)
		for ctx.Err() == nil {
			id := fmt.Sprintf("CP_%03d", rand.IntN(n))
			c.logical.Add(1)
			var last error
			finished := false
			// A NotSent call is safe to retry (the charger never saw it); give a
			// moving session a few seconds to be reachable again.
			for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline) && ctx.Err() == nil; {
				cctx, ccancel := context.WithTimeout(ctx, 10*time.Second)
				h, err := caller.Lookup(cctx, id)
				if err == nil {
					_, err = csms.Call(cctx, h, resetMsg, resetReq{Type: "Soft"})
				} else {
					err = fmt.Errorf("lookup: %w", err) // never written to a charger
					err = gateway.WithSent(err, gateway.NotSent)
				}
				ccancel()
				if err == nil {
					c.answered.Add(1)
					finished = true
					break
				}
				last = err
				if gateway.SentOf(err) != gateway.NotSent {
					c.noResponse.Add(1) // written (or unknown): it may have run, no retry
					finished = true
					break
				}
				c.notSentSeen.Add(1)
				time.Sleep(100 * time.Millisecond)
			}
			if !finished && ctx.Err() == nil {
				c.lost.Add(1)
				c.mu.Lock()
				c.lastLost = fmt.Sprintf("%s: %v", id, last)
				c.mu.Unlock()
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	return c
}

// kill -9 the canary gateway while it is being filled: its chargers reconnect
// to the others (the balancer drops the dead member), nobody is lost.
func TestChaos_GatewayCanaryKilledDuringFill(t *testing.T) {
	const n = 40
	r := newCanaryRig(t, n, 8, nil)
	r.settled(t, n, 10*time.Second)
	before := r.epochs(t, n)
	r.lb.add(r.gws[2])
	out, err := ctl(t, r.mr.Addr(), "gateway", "shed", "--gateway", "gw-0", "--count", "10", "--min-age", "-1s", "--rate", "10", "--wait=false")
	require.NoError(t, err, out)
	require.Eventually(t, func() bool { return r.counts(t)["gw-2"] > 0 }, 15*time.Second, 100*time.Millisecond)
	r.gws[2].kill9()

	require.Eventually(t, func() bool { return r.fleet.up() == n }, 30*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool {
		c := r.counts(t)
		return c["gw-0"]+c["gw-1"] == n
	}, 20*time.Second, 200*time.Millisecond, "the dead gateway's lease lapses and everyone is on the others")
	for id, e := range r.epochs(t, n) {
		require.GreaterOrEqual(t, e, before[id])
	}
}

// Rollback of the canary (a full drain) while another gateway sheds: the fleet
// squeezes onto the one gateway that is ready and nobody is lost.
func TestChaos_GatewayCanaryRollbackWhileAnotherSheds(t *testing.T) {
	const n = 40
	r := newCanaryRig(t, n, 10, nil)
	r.lb.add(r.gws[2])
	out, err := ctl(t, r.mr.Addr(), "gateway", "shed", "--gateway", "gw-1", "--count", "10", "--min-age", "-1s", "--rate", "20")
	require.NoError(t, err, out)
	require.Eventually(t, func() bool { return r.fleet.up() == n && r.counts(t)["gw-2"] > 0 }, 20*time.Second, 100*time.Millisecond)

	// Concurrently: gw-0 sheds, the canary gets SIGTERM (full drain, 2 s window).
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = ctl(t, r.mr.Addr(), "gateway", "shed", "--gateway", "gw-0", "--count", "8", "--min-age", "-1s", "--rate", "10")
	}()
	r.gws[2].term()
	<-done
	require.Eventually(t, func() bool { return r.fleet.up() == n }, 30*time.Second, 100*time.Millisecond)
	r.settled(t, n, 15*time.Second)
}

// Registry outage during a shed: the shed itself is local; the chargers are
// accepted unclaimed while the registry is away and claimed after it returns.
func TestChaos_GatewayShedDuringRegistryOutage(t *testing.T) {
	const n = 30
	var px *faultProxy
	r := newCanaryRig(t, n, 100, func(real string) string {
		px = newProxy(t, real)
		return px.Addr()
	})
	r.settled(t, n, 10*time.Second)
	px.Blackhole(true)
	// The CLI talks to the registry directly (an operator's path), so it still
	// finds the gateways; the gateways cannot reach it.
	out, err := ctl(t, r.mr.Addr(), "gateway", "shed", "--gateway", "gw-0", "--count", "5", "--min-age", "-1s", "--rate", "20")
	require.NoError(t, err, out)
	require.Eventually(t, func() bool { return r.fleet.up() == n }, 30*time.Second, 100*time.Millisecond, "chargers reconnect while the registry is unreachable")
	px.Blackhole(false)
	px.Reset()
	require.Eventually(t, func() bool {
		found := 0
		for i := 0; i < n; i++ {
			if _, ok, _ := r.reg.Lookup(context.Background(), fmt.Sprintf("CP_%03d", i)); ok {
				found++
			}
		}
		return found == n
	}, 30*time.Second, 300*time.Millisecond, "every session is claimed again after the outage")
	require.Equal(t, n, r.fleet.up())
}
