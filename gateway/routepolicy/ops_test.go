package routepolicy_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/storage/memory"
	"github.com/shiv3/gocpp/gateway"
	"github.com/shiv3/gocpp/gateway/routepolicy"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// simGateway is an engine fed by the store whose applied revision shows in a lease.
type simGateway struct {
	e      *routepolicy.Engine
	cancel context.CancelFunc
	done   chan struct{}
}

func startSim(t *testing.T, id string, store storage.PolicyStore, reg storage.SessionRegistry, caps string, watch bool) *simGateway {
	t.Helper()
	labels := map[string]string{"version": "v1", "caps": caps, "policy_rev": "0"}
	beat := func() {
		cp := map[string]string{}
		for k, v := range labels {
			cp[k] = v
		}
		require.NoError(t, reg.Heartbeat(context.Background(), storage.GatewayInfo{ID: id, Boot: "b", State: storage.GatewayServing, Labels: cp}, time.Hour))
	}
	beat()
	g := &simGateway{done: make(chan struct{})}
	g.e = routepolicy.NewEngine(routepolicy.WithOnApply(func(rev uint64) { labels["policy_rev"] = strconv.FormatUint(rev, 10); beat() }))
	ctx, cancel := context.WithCancel(context.Background())
	g.cancel = cancel
	go func() {
		defer close(g.done)
		if watch {
			_ = g.e.Watch(ctx, routepolicy.StoreSource{Store: store})
		}
	}()
	t.Cleanup(func() { cancel(); <-g.done })
	return g
}

func TestControllerPromoteRollbackConvergence(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	ctx := context.Background()
	store, reg := memory.NewPolicyStore(), memory.NewSessionRegistry()
	ctl := routepolicy.Controller{Store: store, Registry: reg}
	gws := []*simGateway{
		startSim(t, "gw-0", store, reg, "route.v1,attrs.set", true),
		startSim(t, "gw-1", store, reg, "route.v1", true),
	}

	// step 1: canary 10% with hold
	rev, err := ctl.Put(ctx, canaryPolicy(0, func(r *routepolicy.Rule) { r.Match.Percent = pct(10); r.Hold = "busy" }), routepolicy.PutOptions{})
	require.NoError(t, err)
	require.EqualValues(t, 1, rev)
	st, err := ctl.WaitConverged(ctx, rev, 2*time.Second)
	require.NoError(t, err)
	require.True(t, st.Converged)
	require.Len(t, st.Gateways, 2)

	// step 2: 50%
	rev, err = ctl.Put(ctx, canaryPolicy(0, func(r *routepolicy.Rule) { r.Match.Percent = pct(50); r.Hold = "busy" }), routepolicy.PutOptions{ExpectRev: 1})
	require.NoError(t, err)
	require.EqualValues(t, 2, rev)
	_, err = ctl.WaitConverged(ctx, rev, 2*time.Second)
	require.NoError(t, err)

	// both gateways route identically
	for i := 0; i < 200; i++ {
		in := gateway.RouteInput{CPID: "cp" + strconv.Itoa(i), Action: "Heartbeat"}
		require.Equal(t, gws[0].e.Route(in), gws[1].e.Route(in))
	}

	// a stale writer loses
	_, err = ctl.Put(ctx, canaryPolicy(0, nil), routepolicy.PutOptions{ExpectRev: 1})
	require.ErrorIs(t, err, routepolicy.ErrConflict)

	// rollback to the previous revision: new revision, IgnoreHold, 10% again
	nrev, restored, err := ctl.Rollback(ctx, 0)
	require.NoError(t, err)
	require.EqualValues(t, 3, nrev)
	require.EqualValues(t, 1, restored)
	_, err = ctl.WaitConverged(ctx, nrev, 2*time.Second)
	require.NoError(t, err)
	p, ok := gws[0].e.Policy()
	require.True(t, ok)
	require.True(t, p.IgnoreHold)
	require.Equal(t, 10.0, *p.Rules[0].Match.Percent)

	// invalid policy: refused before the store
	_, err = ctl.Put(ctx, routepolicy.Policy{Rules: []routepolicy.Rule{{Name: "x"}}}, routepolicy.PutOptions{})
	require.Error(t, err)
	_, err = ctl.Put(ctx, canaryPolicy(0, nil), routepolicy.PutOptions{KnownPools: []string{"default", "other"}})
	require.ErrorContains(t, err, "not one of the known pools")
}

func TestControllerRefusesWhenAGatewayLagsOrLacksRouting(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	ctx := context.Background()
	store, reg := memory.NewPolicyStore(), memory.NewSessionRegistry()
	ctl := routepolicy.Controller{Store: store, Registry: reg}
	startSim(t, "gw-0", store, reg, "route.v1", true)
	startSim(t, "gw-cut-off", store, reg, "route.v1", false) // partitioned from the store

	rev, err := ctl.Put(ctx, canaryPolicy(0, nil), routepolicy.PutOptions{})
	require.NoError(t, err)
	st, err := ctl.WaitConverged(ctx, rev, 500*time.Millisecond)
	require.ErrorIs(t, err, routepolicy.ErrNotConverged)
	require.ErrorContains(t, err, "gw-cut-off")
	require.False(t, st.Converged)
	require.Equal(t, []string{"gw-cut-off (rev 0)"}, st.Lagging())

	startSim(t, "gw-old", store, reg, "", true)
	_, err = ctl.Put(ctx, canaryPolicy(0, nil), routepolicy.PutOptions{})
	require.ErrorContains(t, err, "gw-old")
	// an empty policy (no rules) is always allowed; so is --force
	_, err = ctl.Put(ctx, routepolicy.Policy{}, routepolicy.PutOptions{})
	require.NoError(t, err)
	_, err = ctl.Put(ctx, canaryPolicy(0, nil), routepolicy.PutOptions{Force: true})
	require.NoError(t, err)
}
