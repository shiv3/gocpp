package routepolicy_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/storage/memory"
	"github.com/shiv3/gocpp/gateway"
	"github.com/shiv3/gocpp/gateway/routepolicy"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"pgregory.net/rapid"
)

type clock struct{ t atomic.Int64 }

func newClock() *clock {
	c := &clock{}
	c.t.Store(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano())
	return c
}
func (c *clock) now() time.Time          { return time.Unix(0, c.t.Load()).UTC() }
func (c *clock) advance(d time.Duration) { c.t.Add(int64(d)) }

type rec struct {
	mu                         sync.Mutex
	decisions                  map[string]int
	fallbacks, suspend, reject int
	rev                        uint64
	missing                    bool
}

func (r *rec) Decision(rule, pool string, _ uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.decisions == nil {
		r.decisions = map[string]int{}
	}
	r.decisions[rule+"/"+pool]++
}
func (r *rec) Fallback(string, string) { r.mu.Lock(); r.fallbacks++; r.mu.Unlock() }
func (r *rec) Suspended(_ string, on bool) {
	if on {
		r.mu.Lock()
		r.suspend++
		r.mu.Unlock()
	}
}
func (r *rec) PolicyApplied(rev uint64) { r.mu.Lock(); r.rev = rev; r.mu.Unlock() }
func (r *rec) PolicyRejected()          { r.mu.Lock(); r.reject++; r.mu.Unlock() }
func (r *rec) PolicyMissing(m bool)     { r.mu.Lock(); r.missing = m; r.mu.Unlock() }

func pct(p float64) *float64 { return &p }

func canaryPolicy(rev uint64, mut func(*routepolicy.Rule)) routepolicy.Policy {
	r := routepolicy.Rule{Name: "step", Pool: "canary", Match: routepolicy.Match{Percent: pct(100)}}
	if mut != nil {
		mut(&r)
	}
	return routepolicy.Policy{Revision: rev, Salt: "s", Rules: []routepolicy.Rule{r}}
}

func TestMetricsAndPolicyMissing(t *testing.T) {
	m := &rec{}
	e := routepolicy.NewEngine(routepolicy.WithMetrics(m))
	require.True(t, m.missing)
	require.NoError(t, e.Apply(canaryPolicy(3, nil)))
	require.False(t, m.missing)
	require.EqualValues(t, 3, m.rev)
	e.Route(gateway.RouteInput{CPID: "a", Action: "Heartbeat"})
	require.Equal(t, 1, m.decisions["step/canary"])
	require.Error(t, e.Apply(routepolicy.Policy{Revision: 4, Rules: []routepolicy.Rule{{Name: "x"}}}))
	require.Equal(t, 1, m.reject)
}

func TestHoldBusyKeepsMembershipUntilCleared(t *testing.T) {
	clk := newClock()
	e := routepolicy.NewEngine(routepolicy.WithClock(clk.now))
	// rev1: 0% canary with hold; rev2: 100%.
	p := canaryPolicy(1, func(r *routepolicy.Rule) { r.Hold = "busy"; r.Match.Percent = pct(0) })
	require.NoError(t, e.Apply(p))
	in := gateway.RouteInput{CPID: "cp", Action: "MeterValues", Attrs: map[string]string{}}
	require.Equal(t, "default", e.Route(in).Pool)
	in.Attrs[routepolicy.BusyAttr] = "1"
	require.Equal(t, "default", e.Route(in).Pool) // busy, still default

	p = canaryPolicy(2, func(r *routepolicy.Rule) { r.Hold = "busy" })
	require.NoError(t, e.Apply(p))
	require.Equal(t, "default", e.Route(in).Pool, "busy session is held on its previous pool across the increase")
	require.Equal(t, map[string]int(nil), nilIfEmpty(e.HeldSessions()))
	delete(in.Attrs, routepolicy.BusyAttr)
	require.Equal(t, "canary", e.Route(in).Pool, "moves once the transaction ends")

	// max_hold bounds the hold.
	in.Attrs[routepolicy.BusyAttr] = "1"
	require.Equal(t, "canary", e.Route(in).Pool)
	p = canaryPolicy(3, func(r *routepolicy.Rule) {
		r.Hold = "busy"
		r.Match.Percent = pct(0)
		r.MaxHold = routepolicy.Duration(time.Hour)
	})
	require.NoError(t, e.Apply(p))
	require.Equal(t, "canary", e.Route(in).Pool, "held in the canary while busy")
	clk.advance(2 * time.Hour)
	require.Equal(t, "default", e.Route(in).Pool, "max_hold passed")
}

func nilIfEmpty(m map[string]int) map[string]int {
	if len(m) == 0 {
		return nil
	}
	return m
}

func TestRollbackIgnoresHold(t *testing.T) {
	e := routepolicy.NewEngine()
	require.NoError(t, e.Apply(canaryPolicy(1, func(r *routepolicy.Rule) { r.Hold = "busy" })))
	in := gateway.RouteInput{CPID: "cp", Action: "MeterValues", Attrs: map[string]string{routepolicy.BusyAttr: "1"}}
	require.Equal(t, "canary", e.Route(in).Pool)
	rb := canaryPolicy(2, func(r *routepolicy.Rule) { r.Hold = "busy"; r.Match.Percent = pct(0) })
	require.NoError(t, e.Apply(rb))
	require.Equal(t, "canary", e.Route(in).Pool, "a plain revision is held")
	rb.Revision, rb.IgnoreHold = 3, true
	require.NoError(t, e.Apply(rb))
	require.Equal(t, "default", e.Route(in).Pool, "rollback ignores the hold")
}

func TestClosedForgetsSession(t *testing.T) {
	e := routepolicy.NewEngine()
	require.NoError(t, e.Apply(canaryPolicy(1, func(r *routepolicy.Rule) { r.Hold = "busy" })))
	busy := map[string]string{routepolicy.BusyAttr: "1"}
	e.Route(gateway.RouteInput{CPID: "cp", Action: "Heartbeat", Attrs: busy})
	require.Equal(t, 1, e.HeldSessions()["step"])
	e.Route(gateway.RouteInput{CPID: "cp", Action: "$closed", Attrs: busy})
	require.Empty(t, e.HeldSessions())
}

func TestFallbackSuspendsRuleForWindow(t *testing.T) {
	clk := newClock()
	var susp [][]string
	var smu sync.Mutex
	e := routepolicy.NewEngine(routepolicy.WithClock(clk.now), routepolicy.WithOnSuspend(func(s []string) { smu.Lock(); susp = append(susp, s); smu.Unlock() }))
	require.NoError(t, e.Apply(canaryPolicy(1, func(r *routepolicy.Rule) { r.SuspendFor = routepolicy.Duration(30 * time.Second) })))
	in := gateway.RouteInput{CPID: "cp", Action: "Heartbeat"}
	d := e.Route(in)
	require.Equal(t, "canary", d.Pool)
	e.RouteFallback(d, "unavailable")
	require.Equal(t, "default", e.Route(in).Pool)
	require.Equal(t, []string{"step"}, e.Suspended())
	clk.advance(29 * time.Second)
	e.RouteFallback(d, "unavailable") // does not extend
	require.Equal(t, "default", e.Route(in).Pool)
	clk.advance(2 * time.Second)
	require.Equal(t, "canary", e.Route(in).Pool, "resumes after suspend_for")
	require.Empty(t, e.Suspended())
	smu.Lock()
	require.NotEmpty(t, susp)
	smu.Unlock()

	// A fallback of an older revision is ignored.
	e.RouteFallback(gateway.Route{Rule: "step", Rev: 99}, "unavailable")
	require.Equal(t, "canary", e.Route(in).Pool)
}

func TestGuardSuspendsUntilNextRevision(t *testing.T) {
	clk := newClock()
	e := routepolicy.NewEngine(routepolicy.WithClock(clk.now))
	g := &routepolicy.Guard{MinRequests: 10, MaxErrorRatio: 0.2, Window: routepolicy.Duration(10 * time.Second)}
	require.NoError(t, e.Apply(canaryPolicy(1, func(r *routepolicy.Rule) { r.Guard = g })))
	in := gateway.RouteInput{CPID: "cp", Action: "Heartbeat"}
	d := e.Route(in)
	for i := 0; i < 9; i++ {
		e.RouteOutcome(d, "callerror_InternalError")
	}
	require.Equal(t, "canary", e.Route(in).Pool, "below min_requests")
	e.RouteOutcome(d, "result")
	require.Equal(t, "default", e.Route(in).Pool, "9/10 failed")
	clk.advance(time.Hour)
	require.Equal(t, "default", e.Route(in).Pool, "stays suspended until the next revision")
	require.NoError(t, e.Apply(canaryPolicy(2, func(r *routepolicy.Rule) { r.Guard = g })))
	require.Equal(t, "canary", e.Route(in).Pool)

	// healthy traffic and non-failure outcomes do not trip; old samples age out.
	d = e.Route(in)
	for i := 0; i < 100; i++ {
		e.RouteOutcome(d, "result")
		e.RouteOutcome(d, "callerror_FormationViolation")
	}
	require.Equal(t, "canary", e.Route(in).Pool)
	for i := 0; i < 5; i++ {
		e.RouteOutcome(d, "timeout")
	}
	clk.advance(30 * time.Second)
	for i := 0; i < 20; i++ {
		e.RouteOutcome(d, "result")
	}
	require.Equal(t, "canary", e.Route(in).Pool, "old failures left the window")
}

func TestSuspendedErrorRuleRefuses(t *testing.T) {
	e := routepolicy.NewEngine()
	require.NoError(t, e.Apply(canaryPolicy(1, func(r *routepolicy.Rule) {
		r.OnUnavailable = "error"
		r.Actions = []string{"DataTransfer"}
		r.Guard = &routepolicy.Guard{MinRequests: 1, MaxErrorRatio: 0.5, Window: routepolicy.Duration(time.Minute)}
	})))
	in := gateway.RouteInput{CPID: "cp", Action: "DataTransfer"}
	d := e.Route(in)
	require.Equal(t, gateway.Route{Pool: "canary", Rule: "step", Rev: 1}, d, "error mode has no fallback")
	e.RouteOutcome(d, "timeout")
	require.Equal(t, gateway.PoolReject, e.Route(in).Pool)
	require.Equal(t, "default", e.Route(gateway.RouteInput{CPID: "cp", Action: "Heartbeat"}).Pool)
}

// With a fixed revision, a (charger, action) pair changes pool only at a
// suspension edge, and never more than once per suspend_for toward the default.
func TestPropertyFlappingBoundsPoolChanges(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		clk := newClock()
		const window = 60 * time.Second
		e := routepolicy.NewEngine(routepolicy.WithClock(clk.now))
		require.NoError(t, e.Apply(canaryPolicy(1, nil)))
		in := gateway.RouteInput{CPID: "cp", Action: "Heartbeat"}
		var (
			last      string
			edges     []time.Time // suspension starts
			moved     int
			suspended bool
		)
		steps := rapid.IntRange(1, 300).Draw(t, "steps")
		for i := 0; i < steps; i++ {
			switch rapid.IntRange(0, 2).Draw(t, "op") {
			case 0:
				d := e.Route(in)
				if last != "" && d.Pool != last {
					moved++
					// A change is only possible after a suspension began (to default)
					// or ended (back to the canary).
					require.NotEmpty(t, edges)
				}
				last = d.Pool
			case 1:
				if d := e.Route(in); d.Pool == "canary" {
					e.RouteFallback(d, "unavailable")
					suspended = len(e.Suspended()) > 0
					if suspended {
						edges = append(edges, clk.now())
					}
				}
			case 2:
				clk.advance(time.Duration(rapid.IntRange(0, 40).Draw(t, "dt")) * time.Second)
			}
		}
		for i := 1; i < len(edges); i++ {
			require.GreaterOrEqual(t, edges[i].Sub(edges[i-1]), window, "at most one suspension per window")
		}
		require.LessOrEqual(t, moved, 2*len(edges), "at most two pool changes per suspension")
	})
}

// ---- PoolSet integration -----------------------------------------------------

type up struct {
	tag    string
	calls  atomic.Int64
	result func(env gateway.Envelope) (gateway.Reply, error)
}

func (u *up) Connect(context.Context, gateway.ConnectRequest) (gateway.ConnectReply, error) {
	return gateway.ConnectReply{}, nil
}

func (u *up) Handle(_ context.Context, env gateway.Envelope) (gateway.Reply, error) {
	u.calls.Add(1)
	if u.result != nil {
		return u.result(env)
	}
	return gateway.Reply{Payload: []byte(`{"from":"` + u.tag + `"}`)}, nil
}
func (u *up) Notify(context.Context, gateway.Envelope) error { return nil }

func route(e *routepolicy.Engine) *gateway.Route {
	d := e.Route(gateway.RouteInput{CPID: "cp", Action: "Heartbeat", Kind: gateway.KindCall})
	return &d
}

func TestPoolSetFallbackSuspendsAndRecovers(t *testing.T) {
	defer goleak.VerifyNone(t)
	clk := newClock()
	e := routepolicy.NewEngine(routepolicy.WithClock(clk.now))
	require.NoError(t, e.Apply(canaryPolicy(1, func(r *routepolicy.Rule) { r.SuspendFor = routepolicy.Duration(10 * time.Second) })))
	ps := gateway.NewPoolSet(5 * time.Second)
	ps.SetObserver(e)
	def, can := &up{tag: "def"}, &up{tag: "can"}
	ps.Add(gateway.BackendInfo{ID: "d", Pool: "default"}, def)
	m := ps.Add(gateway.BackendInfo{ID: "c", Pool: "canary"}, can)

	rep, err := ps.Handle(context.Background(), gateway.Envelope{Kind: gateway.KindCall, Route: route(e)})
	require.NoError(t, err)
	require.Contains(t, string(rep.Payload), "can")

	// Canary dies (kill -9): the next message falls back at once, no 20 s hold.
	ps.Remove(m)
	start := time.Now()
	rep, err = ps.Handle(context.Background(), gateway.Envelope{Kind: gateway.KindCall, Route: route(e)})
	require.NoError(t, err)
	require.Contains(t, string(rep.Payload), "def")
	require.Less(t, time.Since(start), time.Second)
	require.Equal(t, []string{"step"}, e.Suspended())

	// The canary returns, but the rule stays suspended for its window.
	ps.Add(gateway.BackendInfo{ID: "c2", Pool: "canary"}, can)
	rep, _ = ps.Handle(context.Background(), gateway.Envelope{Kind: gateway.KindCall, Route: route(e)})
	require.Contains(t, string(rep.Payload), "def")
	clk.advance(11 * time.Second)
	rep, _ = ps.Handle(context.Background(), gateway.Envelope{Kind: gateway.KindCall, Route: route(e)})
	require.Contains(t, string(rep.Payload), "can")
}

func TestPoolSetGuardTripsOnErrorInjectingCanary(t *testing.T) {
	defer goleak.VerifyNone(t)
	e := routepolicy.NewEngine()
	g := &routepolicy.Guard{MinRequests: 20, MaxErrorRatio: 0.1, Window: routepolicy.Duration(time.Minute)}
	require.NoError(t, e.Apply(canaryPolicy(1, func(r *routepolicy.Rule) { r.Guard = g })))
	ps := gateway.NewPoolSet(time.Second)
	ps.SetObserver(e)
	bad := &up{result: func(gateway.Envelope) (gateway.Reply, error) {
		return gateway.Reply{Error: &gateway.WireError{Kind: gateway.ErrKindCallError, Code: "InternalError"}}, nil
	}}
	good := &up{tag: "def"}
	ps.Add(gateway.BackendInfo{ID: "d"}, good)
	ps.Add(gateway.BackendInfo{ID: "c", Pool: "canary"}, bad)
	n := 0
	for ; n < 100 && len(e.Suspended()) == 0; n++ {
		_, err := ps.Handle(context.Background(), gateway.Envelope{Kind: gateway.KindCall, Route: route(e)})
		require.NoError(t, err)
	}
	require.Equal(t, 20, n, "tripped at min_requests")
	rep, _ := ps.Handle(context.Background(), gateway.Envelope{Kind: gateway.KindCall, Route: route(e)})
	require.Contains(t, string(rep.Payload), "def")
}

func TestPoolSetNackFallsBackAndCounts(t *testing.T) {
	defer goleak.VerifyNone(t)
	m := &rec{}
	e := routepolicy.NewEngine(routepolicy.WithMetrics(m))
	require.NoError(t, e.Apply(canaryPolicy(1, nil)))
	ps := gateway.NewPoolSet(time.Second)
	ps.SetObserver(e)
	nack := &up{result: func(gateway.Envelope) (gateway.Reply, error) { return gateway.Reply{Nack: true}, nil }}
	ps.Add(gateway.BackendInfo{ID: "d"}, &up{tag: "def"})
	ps.Add(gateway.BackendInfo{ID: "c", Pool: "canary"}, nack)
	rep, err := ps.Handle(context.Background(), gateway.Envelope{Kind: gateway.KindCall, Route: route(e), Deadline: time.Now().Add(time.Second)})
	require.NoError(t, err)
	require.Contains(t, string(rep.Payload), "def")
	require.Equal(t, 1, m.fallbacks)
	require.NotEmpty(t, e.Suspended())
}

func TestPoolRejectIsImmediate(t *testing.T) {
	ps := gateway.NewPoolSet(5 * time.Second)
	start := time.Now()
	_, err := ps.Handle(context.Background(), gateway.Envelope{Route: &gateway.Route{Pool: gateway.PoolReject}})
	require.True(t, errors.Is(err, gateway.ErrNoBackend))
	require.Less(t, time.Since(start), time.Second)
}

// ---- sources -------------------------------------------------------------------

func TestFileSource(t *testing.T) {
	defer goleak.VerifyNone(t)
	path := filepath.Join(t.TempDir(), "policy.yaml")
	require.NoError(t, os.WriteFile(path, []byte("revision: 1\ndefault: stable\n"), 0o600))
	e := routepolicy.NewEngine()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = e.Watch(ctx, routepolicy.FileSource{Path: path}) }()
	require.Eventually(t, func() bool { return e.Revision() == 1 }, 3*time.Second, 20*time.Millisecond)
	require.NoError(t, os.WriteFile(path, []byte("revision: 2\ndefault: next\n"), 0o600))
	require.Eventually(t, func() bool { return e.Revision() == 2 }, 4*time.Second, 50*time.Millisecond)
	// invalid file: the previous policy stays
	require.NoError(t, os.WriteFile(path, []byte("revision: 3\nrules: [{name: x}]\n"), 0o600))
	time.Sleep(1500 * time.Millisecond)
	require.EqualValues(t, 2, e.Revision())
	cancel()
	<-done
}

func TestStoreSourceAndCAS(t *testing.T) {
	defer goleak.VerifyNone(t)
	store := memory.NewPolicyStore()
	ok, err := store.PutPolicy(context.Background(), 0, 1, []byte(`{"default":"a"}`))
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = store.PutPolicy(context.Background(), 0, 2, []byte(`{"default":"b"}`))
	require.NoError(t, err)
	require.False(t, ok, "CAS miss")
	_, err = store.PutPolicy(context.Background(), 1, 1, nil)
	require.Error(t, err)

	var applied atomic.Uint64
	e := routepolicy.NewEngine(routepolicy.WithOnApply(func(r uint64) { applied.Store(r) }))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = e.Watch(ctx, routepolicy.StoreSource{Store: store}) }()
	require.Eventually(t, func() bool { return applied.Load() == 1 }, time.Second, 5*time.Millisecond)
	ok, _ = store.PutPolicy(context.Background(), 1, 2, []byte(`{"default":"b"}`))
	require.True(t, ok)
	require.Eventually(t, func() bool { return applied.Load() == 2 }, time.Second, 5*time.Millisecond)
	require.Equal(t, "b", e.Route(gateway.RouteInput{CPID: "x"}).Pool)
	cancel()
	<-done
	h, _ := store.PolicyHistory(context.Background())
	require.Len(t, h, 2)
	require.EqualValues(t, 2, h[0].Rev)
}

// A session that is already busy when it is first evaluated under a hold: busy
// rule (the previous revision had no hold, so no memory of it exists) must stay
// where the previous revision had it (review NIT: it moved mid-transaction).
func TestHoldBusyHoldsOnTheFirstEvaluation(t *testing.T) {
	e := routepolicy.NewEngine()
	require.NoError(t, e.Apply(canaryPolicy(1, func(r *routepolicy.Rule) { r.Match.Percent = pct(0) })))
	in := gateway.RouteInput{CPID: "cp", Action: "MeterValues", Attrs: map[string]string{routepolicy.BusyAttr: "1"}}
	require.Equal(t, "default", e.Route(in).Pool)
	// rev 2 sends everyone to the canary and holds busy sessions; this one has
	// never been evaluated under a hold rule, but it is on the default pool.
	require.NoError(t, e.Apply(canaryPolicy(2, func(r *routepolicy.Rule) { r.Hold = "busy" })))
	require.Equal(t, "default", e.Route(in).Pool, "a busy session moved on its first evaluation")
	delete(in.Attrs, routepolicy.BusyAttr)
	require.Equal(t, "canary", e.Route(in).Pool)
}
