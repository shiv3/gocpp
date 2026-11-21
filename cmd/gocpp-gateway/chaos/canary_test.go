//go:build !short

package chaos

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	registryredis "github.com/shiv3/gocpp/addons/registry-redis"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/gateway/routepolicy"
	"github.com/stretchr/testify/require"
)

// Backend canary chaos (docs/design/gateway.md 5.1.6): a routing policy sends
// chargers to a canary pool; the canary is killed, flaps, or injects errors.

func startPoolBackend(t *testing.T, id, pool, redisAddr string, link string, extra ...string) *proc {
	t.Helper()
	args := append([]string{"-id", id, "-pool", pool, "-gateways", link, "-redis", redisAddr}, extra...)
	b := start(t, "testbackend:"+id, args...)
	require.Eventually(t, func() bool { return b.count("READY") > 0 }, 10*time.Second, 20*time.Millisecond)
	return b
}

func controller(t *testing.T, addr string) routepolicy.Controller {
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	reg := registryredis.New(rdb)
	return routepolicy.Controller{Store: reg, Registry: reg}
}

func canaryRule(mut func(*routepolicy.Rule)) routepolicy.Policy {
	pc := 100.0
	r := routepolicy.Rule{Name: "canary", Pool: "canary", Match: routepolicy.Match{Percent: &pc}}
	if mut != nil {
		mut(&r)
	}
	return routepolicy.Policy{Salt: "chaos", Rules: []routepolicy.Rule{r}}
}

func hb(t *testing.T, c *charger) error {
	ctx, cancel := ctxT(10 * time.Second)
	defer cancel()
	return c.heartbeat(ctx)
}

// kill -9 every canary backend: a message in flight gets InternalError once, the
// next messages reach the default pool at once (no 20 s hold), and the charger
// stays connected.
func TestChaos_CanaryKilledFallsBackWithoutHold(t *testing.T) {
	mr := miniredis.RunT(t)
	g := startGateway(t, "gw-1", mr.Addr(), "--upstream-wait", "10s")
	def := startPoolBackend(t, "be-d", "", mr.Addr(), g.link)
	can := startPoolBackend(t, "be-c", "canary", mr.Addr(), g.link)
	require.Eventually(t, g.ready, 5*time.Second, 50*time.Millisecond)
	ctl := controller(t, mr.Addr())
	rev, err := ctl.Put(context.Background(), canaryRule(nil), routepolicy.PutOptions{})
	require.NoError(t, err)
	st, err := ctl.WaitConverged(context.Background(), rev, 5*time.Second)
	require.NoError(t, err, "%+v", st)

	c := newCharger(t, g.ws, "CP_1")
	require.NoError(t, hb(t, c))
	require.Equal(t, 1, can.count("HANDLED Heartbeat be-c CP_1"))
	require.Zero(t, def.count("HANDLED Heartbeat"))

	// In flight on the canary when it dies.
	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := ctxT(20 * time.Second)
		defer cancel()
		errCh <- c.boot(ctx, "hold")
	}()
	require.Eventually(t, func() bool { return can.count("HANDLED BootNotification be-c CP_1 hold") == 1 }, 10*time.Second, 20*time.Millisecond)
	can.kill9()
	require.Equal(t, ocppj.ErrorCodeInternalError, callErrCode(<-errCh))
	require.Zero(t, def.count("HANDLED BootNotification"), "no redispatch after dispatch")

	start := time.Now()
	require.NoError(t, hb(t, c))
	require.Less(t, time.Since(start), 2*time.Second, "fallback must not wait for the upstream hold")
	require.Equal(t, 1, def.count("HANDLED Heartbeat be-d CP_1"))
	require.True(t, c.IsConnected())
	require.Zero(t, c.disconnects.get())

	// The suspension is visible in the lease.
	require.Eventually(t, func() bool {
		s, err := ctl.Status(context.Background())
		return err == nil && len(s.Gateways) == 1 && len(s.Gateways[0].Suspended) == 1
	}, 3*time.Second, 50*time.Millisecond)
}

// A canary in a crash loop: after the first fallback the rule is suspended, so
// the charger changes pool once per window, not once per restart.
func TestChaos_CanaryFlappingIsSuspended(t *testing.T) {
	mr := miniredis.RunT(t)
	g := startGateway(t, "gw-1", mr.Addr())
	def := startPoolBackend(t, "be-d", "", mr.Addr(), g.link)
	can := startPoolBackend(t, "be-c1", "canary", mr.Addr(), g.link)
	require.Eventually(t, g.ready, 5*time.Second, 50*time.Millisecond)
	ctl := controller(t, mr.Addr())
	rev, err := ctl.Put(context.Background(), canaryRule(func(r *routepolicy.Rule) { r.SuspendFor = routepolicy.Duration(4 * time.Second) }), routepolicy.PutOptions{})
	require.NoError(t, err)
	_, err = ctl.WaitConverged(context.Background(), rev, 5*time.Second)
	require.NoError(t, err)
	c := newCharger(t, g.ws, "CP_1")
	require.NoError(t, hb(t, c))
	require.Equal(t, 1, can.count("HANDLED Heartbeat"))

	can.kill9()
	require.NoError(t, hb(t, c)) // falls back, suspends the rule
	// Crash loop: the canary restarts three times inside the window; none of it
	// attracts the charger.
	var restarted []*proc
	for i := 0; i < 3; i++ {
		restarted = append(restarted, startPoolBackend(t, "be-c"+string(rune('2'+i)), "canary", mr.Addr(), g.link))
		require.NoError(t, hb(t, c))
	}
	for _, b := range restarted {
		require.Zero(t, b.count("HANDLED Heartbeat"), "suspended rule: the charger stays on the default pool")
	}
	require.GreaterOrEqual(t, def.count("HANDLED Heartbeat"), 4)

	time.Sleep(4 * time.Second)
	require.NoError(t, hb(t, c))
	n := 0
	for _, b := range restarted {
		n += b.count("HANDLED Heartbeat")
	}
	require.Equal(t, 1, n, "the window passed: back on the canary")
}

// An error-injecting canary trips the guard within its window; the charger's
// later messages are answered by the default pool and the lease says why.
func TestChaos_CanaryGuardTripsOnErrors(t *testing.T) {
	mr := miniredis.RunT(t)
	g := startGateway(t, "gw-1", mr.Addr())
	def := startPoolBackend(t, "be-d", "", mr.Addr(), g.link)
	can := startPoolBackend(t, "be-c", "canary", mr.Addr(), g.link, "-fail-heartbeat")
	require.Eventually(t, g.ready, 5*time.Second, 50*time.Millisecond)
	ctl := controller(t, mr.Addr())
	rev, err := ctl.Put(context.Background(), canaryRule(func(r *routepolicy.Rule) {
		r.Guard = &routepolicy.Guard{MinRequests: 5, MaxErrorRatio: 0.5, Window: routepolicy.Duration(30 * time.Second)}
	}), routepolicy.PutOptions{})
	require.NoError(t, err)
	_, err = ctl.WaitConverged(context.Background(), rev, 5*time.Second)
	require.NoError(t, err)
	c := newCharger(t, g.ws, "CP_1")

	failed := 0
	for i := 0; i < 5; i++ {
		if err := hb(t, c); err != nil {
			require.Equal(t, ocppj.ErrorCodeInternalError, callErrCode(err))
			failed++
		}
	}
	require.Equal(t, 5, failed)
	require.NoError(t, hb(t, c), "after the trip the default pool answers")
	require.Equal(t, 5, can.count("HANDLED Heartbeat"))
	require.Equal(t, 1, def.count("HANDLED Heartbeat"))
	require.Eventually(t, func() bool {
		s, err := ctl.Status(context.Background())
		return err == nil && len(s.Gateways) == 1 && strings.Join(s.Gateways[0].Suspended, ",") == "canary"
	}, 3*time.Second, 50*time.Millisecond)

	// The guard holds until the next revision; rolling back clears it.
	_, _, err = ctl.Rollback(context.Background(), 0)
	require.Error(t, err, "only one revision stored: nothing to roll back to")
	rev, err = ctl.Put(context.Background(), routepolicy.Policy{}, routepolicy.PutOptions{})
	require.NoError(t, err)
	_, err = ctl.WaitConverged(context.Background(), rev, 5*time.Second)
	require.NoError(t, err)
	require.NoError(t, hb(t, c))
	require.Equal(t, 5, can.count("HANDLED Heartbeat"), "empty policy: everything on the default pool")
}

// Policy store down: a running gateway keeps its policy; a gateway that starts
// while the store is down routes to the default pool and says so. An invalid
// policy written behind the CLI's back is rejected and the previous one stays.
func TestChaos_PolicyStoreDownAndInvalidPolicy(t *testing.T) {
	mr := miniredis.RunT(t)
	g := startGateway(t, "gw-1", mr.Addr())
	startPoolBackend(t, "be-d", "", "", g.link)
	can := startPoolBackend(t, "be-c", "canary", "", g.link)
	require.Eventually(t, g.ready, 5*time.Second, 50*time.Millisecond)
	ctl := controller(t, mr.Addr())
	rev, err := ctl.Put(context.Background(), canaryRule(nil), routepolicy.PutOptions{})
	require.NoError(t, err)
	_, err = ctl.WaitConverged(context.Background(), rev, 5*time.Second)
	require.NoError(t, err)
	c := newCharger(t, g.ws, "CP_1")
	require.NoError(t, hb(t, c))
	require.Equal(t, 1, can.count("HANDLED Heartbeat"))

	// Invalid document stored directly (a buggy writer): refused, revision stays.
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	ok, err := registryredis.New(rdb).PutPolicy(context.Background(), rev, rev+1, []byte(`{"rules":[{"name":"x"}]}`))
	require.NoError(t, err)
	require.True(t, ok)
	require.Eventually(t, func() bool {
		return strings.Contains(metrics(t, g), "gocpp_gateway_route_policy_rejected_total{") && strings.Contains(metrics(t, g), "} 1\n")
	}, 5*time.Second, 100*time.Millisecond)
	require.NoError(t, hb(t, c))
	require.Equal(t, 2, can.count("HANDLED Heartbeat"), "still on the previous policy")

	// Store down.
	addr := mr.Addr()
	mr.Close()
	time.Sleep(500 * time.Millisecond)
	require.NoError(t, hb(t, c))
	require.Equal(t, 3, can.count("HANDLED Heartbeat"), "the running gateway keeps its last policy")

	// A restarted gateway has no policy: default pool, gauge raised.
	g2 := startGateway(t, "gw-2", addr)
	def2 := startPoolBackend(t, "be-d2", "", "", g2.link)
	require.Eventually(t, g2.ready, 5*time.Second, 50*time.Millisecond)
	c2 := newCharger(t, g2.ws, "CP_2")
	require.NoError(t, hb(t, c2))
	require.Equal(t, 1, def2.count("HANDLED Heartbeat"))
	require.Contains(t, metrics(t, g2), "gocpp_gateway_route_policy_missing{")
	require.Regexp(t, `gocpp_gateway_route_policy_missing\{[^}]*\} 1\n`, metrics(t, g2))
}

func metrics(t *testing.T, g *gw) string {
	t.Helper()
	return httpBody("http://" + g.admin + "/metrics")
}

func httpBody(u string) string {
	resp, err := http.Get(u)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}
