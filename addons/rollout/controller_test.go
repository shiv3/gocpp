package rollout_test

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/shiv3/gocpp/addons/rollout"
	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/storage/memory"
	"github.com/shiv3/gocpp/gateway/routepolicy"
	"github.com/stretchr/testify/require"
)

const planYAML = `
rule: step
base:
  salt: r1
  rules:
    - name: step
      pool: canary
      hold: busy
stages:
  - {name: one, percent: 1, soak: 10m}
  - {name: ten, percent: 10, soak: 10m}
  - {name: all, percent: 100, soak: 10m}
interval: 1m
window: 5m
gates: {min_requests: 500}
`

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

// series is a recorded metric source: stats per pool as a function of time and of
// the percent currently applied (so the canary only sees traffic when it has any).
type series struct {
	mu     sync.Mutex
	canary func() rollout.PoolStats
	stable func() rollout.PoolStats
	asked  int
}

func (s *series) Stats(_ context.Context, pool string, _ time.Duration) (rollout.PoolStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked++
	if pool == "canary" {
		return s.canary(), nil
	}
	return s.stable(), nil
}
func (s *series) Scalar(context.Context, string) (float64, error) { return 0, rollout.ErrNoScalar }

func healthy(n float64) rollout.PoolStats {
	return rollout.PoolStats{Requests: n, Failed: n * 0.001, ByOutcome: map[string]float64{"callerror_InternalError": n * 0.001}, P99: 0.040, HasLatency: true}
}

// rig: a store, two simulated gateways that follow it, a controller on a fake clock.
type rig struct {
	t      *testing.T
	store  *memory.PolicyStore
	reg    storage.SessionRegistry
	ctl    *rollout.Controller
	clk    *clock
	events []rollout.Decision
	gws    []*gw
}

type gw struct {
	id     string
	labels map[string]string
	reg    storage.SessionRegistry
	mu     sync.Mutex
}

func (g *gw) beat() {
	g.mu.Lock()
	defer g.mu.Unlock()
	cp := map[string]string{}
	for k, v := range g.labels {
		cp[k] = v
	}
	_ = g.reg.Heartbeat(context.Background(), storage.GatewayInfo{ID: g.id, Boot: "b", State: storage.GatewayServing, Labels: cp}, time.Hour)
}

func (g *gw) set(k, v string) { g.mu.Lock(); g.labels[k] = v; g.mu.Unlock(); g.beat() }

func newRig(t *testing.T, src rollout.Source, mut func(*rollout.Plan)) *rig {
	t.Helper()
	r := &rig{t: t, store: memory.NewPolicyStore(), reg: memory.NewSessionRegistry(), clk: &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}
	for i := 0; i < 2; i++ {
		g := &gw{id: "gw-" + strconv.Itoa(i), reg: r.reg, labels: map[string]string{"caps": "route.v1", "policy_rev": "0"}}
		g.beat()
		r.gws = append(r.gws, g)
	}
	plan, err := rollout.Parse([]byte(planYAML))
	require.NoError(t, err)
	if mut != nil {
		mut(&plan)
	}
	r.ctl = rollout.New(plan, routepolicy.Controller{Store: r.store, Registry: r.reg}, src, rollout.NotifyFunc(func(d rollout.Decision) { r.events = append(r.events, d) }))
	r.ctl.Now = r.clk.now
	return r
}

// follow makes the gateways report the stored revision (what the gateway binary does).
func (r *rig) follow() {
	rev, _, _ := r.store.Policy(context.Background())
	for _, g := range r.gws {
		g.set("policy_rev", strconv.FormatUint(rev, 10))
	}
}

func (r *rig) step() rollout.Decision {
	r.t.Helper()
	d, err := r.ctl.Step(context.Background())
	require.NoError(r.t, err)
	return d
}

func (r *rig) stored() routepolicy.Policy {
	p, err := routepolicy.Controller{Store: r.store}.Current(context.Background())
	require.NoError(r.t, err)
	return p
}

func percentOf(p routepolicy.Policy) float64 {
	if len(p.Rules) == 0 || p.Rules[0].Match.Percent == nil {
		return -1
	}
	return *p.Rules[0].Match.Percent
}

func TestHealthyRolloutAdvancesThroughAllStages(t *testing.T) {
	src := &series{canary: func() rollout.PoolStats { return healthy(2000) }, stable: func() rollout.PoolStats { return healthy(50000) }}
	r := newRig(t, src, nil)

	d := r.step() // init: writes stage 1
	require.Equal(t, rollout.ActionApply, d.Action)
	require.Equal(t, 1.0, percentOf(r.stored()))
	d = r.step()
	require.Equal(t, rollout.ActionHold, d.Action)
	require.Contains(t, d.Reason, "not yet on the revision")

	r.follow()
	d = r.step()
	require.Contains(t, d.Reason, "soaking")
	r.clk.advance(time.Minute)
	for _, want := range []string{"soaking", "soaking"} {
		d = r.step()
		require.Equal(t, rollout.ActionHold, d.Action)
		require.Contains(t, d.Reason, want)
		r.clk.advance(time.Minute)
	}
	// Soak is 10m: advance at the first evaluation past it.
	r.clk.advance(8 * time.Minute)
	d = r.step()
	require.Equal(t, rollout.ActionApply, d.Action, d.Reason)
	require.Contains(t, d.Reason, "gates passed, advancing")
	require.Equal(t, 10.0, percentOf(r.stored()))
	require.EqualValues(t, 2, r.stored().Revision)

	r.follow()
	r.step()
	r.clk.advance(11 * time.Minute)
	d = r.step()
	require.Equal(t, 100.0, percentOf(r.stored()), d.Reason)
	r.follow()
	r.step()
	r.clk.advance(11 * time.Minute)
	d = r.step()
	require.Equal(t, rollout.ActionDone, d.Action)
	require.Equal(t, rollout.StateDone, r.ctl.State())
	require.Contains(t, d.Reason, "ready to promote")
	require.True(t, r.ctl.Finished())
	require.Equal(t, 100.0, percentOf(r.stored()), "the rule stays at its last stage; promoting is a deliberate step")
}

// run drives a rollout whose gateways follow the store immediately, until it
// finishes or max steps pass.
func (r *rig) run(limit int, every time.Duration) []rollout.Decision {
	var out []rollout.Decision
	for i := 0; i < limit && !r.ctl.Finished(); i++ {
		out = append(out, r.step())
		r.follow()
		r.clk.advance(every)
	}
	return out
}

func TestErrorSpikeRollsBackWithinOneInterval(t *testing.T) {
	spike := false
	src := &series{
		canary: func() rollout.PoolStats {
			if spike {
				return rollout.PoolStats{Requests: 1000, Failed: 300, ByOutcome: map[string]float64{"callerror_InternalError": 300}, P99: 0.05, HasLatency: true}
			}
			return healthy(1000)
		},
		stable: func() rollout.PoolStats { return healthy(50000) },
	}
	r := newRig(t, src, nil)
	// Start from an existing policy so the rollback has something to restore.
	prior := routepolicy.Policy{Default: "default", Salt: "old"}
	_, err := routepolicy.Controller{Store: r.store}.Put(context.Background(), prior, routepolicy.PutOptions{})
	require.NoError(t, err)

	r.run(14, time.Minute) // healthy: into stage "ten"
	require.Equal(t, 10.0, percentOf(r.stored()))
	spike = true
	d := r.step()
	require.Equal(t, rollout.ActionRollback, d.Action, d.Reason)
	require.Contains(t, d.Reason, "failed_ratio")
	require.Equal(t, rollout.StateRolledBack, r.ctl.State())
	p := r.stored()
	require.True(t, p.IgnoreHold, "rollback ignores holds")
	require.Equal(t, "old", p.Salt, "the policy from before the rollout is back")
	require.Empty(t, p.Rules)
	r.step() // finished: only holds
	require.Equal(t, rollout.ActionHold, r.events[len(r.events)-1].Action)
}

func TestLatencyRegressionAndCodeShareRollBack(t *testing.T) {
	for name, canary := range map[string]rollout.PoolStats{
		"latency": {Requests: 1000, Failed: 1, P99: 0.200, HasLatency: true},
		"codes": {Requests: 1000, Failed: 1, P99: 0.04, HasLatency: true,
			ByOutcome: map[string]float64{"callerror_FormationViolation": 40}},
	} {
		t.Run(name, func(t *testing.T) {
			src := &series{canary: func() rollout.PoolStats { return canary }, stable: func() rollout.PoolStats { return healthy(50000) }}
			r := newRig(t, src, nil)
			ds := r.run(6, time.Minute)
			last := ds[len(ds)-1]
			require.Equal(t, rollout.ActionRollback, last.Action, last.Reason)
			require.Equal(t, rollout.StateRolledBack, r.ctl.State())
			require.Empty(t, r.stored().Rules, "no policy before the rollout: an empty policy is restored")
		})
	}
}

func TestTooFewSamplesMakesNoDecision(t *testing.T) {
	src := &series{canary: func() rollout.PoolStats { return healthy(120) }, stable: func() rollout.PoolStats { return healthy(50000) }}
	r := newRig(t, src, nil)
	ds := r.run(40, time.Minute)
	for _, d := range ds[2:] {
		require.Equal(t, rollout.ActionHold, d.Action)
	}
	require.Contains(t, ds[len(ds)-1].Reason, "no decision")
	require.Equal(t, 1.0, percentOf(r.stored()), "no advance, no rollback")
	require.EqualValues(t, 1, r.stored().Revision)
	require.False(t, r.ctl.Finished())

	// even a terrible error ratio on 120 requests is not acted on
	src.canary = func() rollout.PoolStats {
		return rollout.PoolStats{Requests: 120, Failed: 100, ByOutcome: map[string]float64{}}
	}
	d := r.step()
	require.Equal(t, rollout.ActionHold, d.Action)
}

func TestGuardSuspensionRollsBack(t *testing.T) {
	src := &series{canary: func() rollout.PoolStats { return healthy(2000) }, stable: func() rollout.PoolStats { return healthy(50000) }}
	r := newRig(t, src, nil)
	r.step()
	r.follow()
	r.step()
	r.gws[1].set("suspended", "step")
	d := r.step()
	require.Equal(t, rollout.ActionRollback, d.Action)
	require.Contains(t, d.Reason, "guard suspended")
	require.Contains(t, d.Reason, "gw-1")
}

func TestYieldsWhenSomeoneElseChangesThePolicy(t *testing.T) {
	src := &series{canary: func() rollout.PoolStats { return healthy(2000) }, stable: func() rollout.PoolStats { return healthy(50000) }}
	r := newRig(t, src, nil)
	r.step()
	r.follow()
	r.step()
	// an operator rolls back by hand
	_, _, err := routepolicy.Controller{Store: r.store, Registry: r.reg}.Rollback(context.Background(), 0)
	require.Error(t, err, "only one revision: nothing to roll back to")
	_, err = routepolicy.Controller{Store: r.store}.Put(context.Background(), routepolicy.Policy{Salt: "manual"}, routepolicy.PutOptions{})
	require.NoError(t, err)
	d := r.step()
	require.Equal(t, rollout.ActionYield, d.Action)
	require.Equal(t, rollout.StateYielded, r.ctl.State())
	require.Equal(t, "manual", r.stored().Salt, "the controller did not touch it")

	// and a CAS conflict at apply time yields too
	r2 := newRig(t, src, nil)
	r2.ctl.Policy.Store = &racyStore{PolicyStore: r2.store}
	d = r2.step()
	require.Equal(t, rollout.ActionYield, d.Action)
}

// racyStore makes every first write lose the race.
type racyStore struct{ *memory.PolicyStore }

func (s *racyStore) PutPolicy(ctx context.Context, prev, rev uint64, data []byte) (bool, error) {
	_, _ = s.PolicyStore.PutPolicy(ctx, prev, prev+1, []byte(`{}`))
	return s.PolicyStore.PutPolicy(ctx, prev, rev, data)
}

func TestDryRunWritesNothing(t *testing.T) {
	src := &series{canary: func() rollout.PoolStats { return healthy(2000) }, stable: func() rollout.PoolStats { return healthy(50000) }}
	r := newRig(t, src, nil)
	r.ctl.DryRun = true
	ds := r.run(60, time.Minute)
	require.Equal(t, rollout.StateDone, r.ctl.State(), "virtual stages advance on the same clock")
	rev, _, _ := r.store.Policy(context.Background())
	require.Zero(t, rev, "a dry run never writes")
	var applies []string
	for _, d := range ds {
		require.True(t, d.DryRun)
		if d.Action == rollout.ActionApply {
			applies = append(applies, d.Reason)
		}
	}
	require.Len(t, applies, 3)
	require.Contains(t, applies[0], "would apply stage one")

	// a failing canary reports the rollback it would do
	bad := &series{canary: func() rollout.PoolStats { return rollout.PoolStats{Requests: 1000, Failed: 500} }, stable: func() rollout.PoolStats { return healthy(50000) }}
	r2 := newRig(t, bad, nil)
	r2.ctl.DryRun = true
	ds = r2.run(10, time.Minute)
	require.Equal(t, rollout.ActionRollback, ds[len(ds)-1].Action)
	require.Contains(t, ds[len(ds)-1].Reason, "would roll back")
	rev, _, _ = r2.store.Policy(context.Background())
	require.Zero(t, rev)
}

func TestFailEvaluationsAndMetricsErrors(t *testing.T) {
	bad := true
	src := &series{canary: func() rollout.PoolStats {
		if bad {
			return rollout.PoolStats{Requests: 1000, Failed: 500}
		}
		return healthy(1000)
	}, stable: func() rollout.PoolStats { return healthy(50000) }}
	r := newRig(t, src, func(p *rollout.Plan) { p.FailEvaluations = 2 })
	ds := r.run(3, time.Minute)
	require.Contains(t, ds[2].Reason, "gate failing (1 of 2")
	bad = false
	d := r.step()
	require.NotEqual(t, rollout.ActionRollback, d.Action, "one good evaluation resets the count")
	bad = true
	r.clk.advance(time.Minute)
	require.Contains(t, r.step().Reason, "1 of 2")
	r.clk.advance(time.Minute)
	require.Equal(t, rollout.ActionRollback, r.step().Action)
}

func TestPlanValidation(t *testing.T) {
	for name, doc := range map[string]string{
		"unknown rule":   "rule: x\nbase: {rules: [{name: step, pool: canary}]}\nstages: [{percent: 5, soak: 1m}]\n",
		"no stages":      "rule: step\nbase: {rules: [{name: step, pool: canary}]}\n",
		"shrinking":      "rule: step\nbase: {rules: [{name: step, pool: canary}]}\nstages: [{percent: 50, soak: 1m}, {percent: 5, soak: 1m}]\n",
		"zero percent":   "rule: step\nbase: {rules: [{name: step, pool: canary}]}\nstages: [{percent: 0, soak: 1m}]\n",
		"unknown field":  "rule: step\nbogus: 1\nbase: {rules: [{name: step, pool: canary}]}\nstages: [{percent: 5, soak: 1m}]\n",
		"invalid policy": "rule: step\nbase: {rules: [{name: step}]}\nstages: [{percent: 5, soak: 1m}]\n",
	} {
		_, err := rollout.Parse([]byte(doc))
		require.Error(t, err, name)
	}
}

func TestWaitsWhileGatewaysLagAndRetriesOnMetricErrors(t *testing.T) {
	fail := true
	src := &flaky{series: series{canary: func() rollout.PoolStats { return healthy(2000) }, stable: func() rollout.PoolStats { return healthy(50000) }}, fail: &fail}
	r := newRig(t, src, nil)
	r.step()
	r.follow()
	r.step() // converged
	r.clk.advance(2 * time.Minute)
	d := r.step()
	require.Equal(t, rollout.ActionError, d.Action)
	require.Contains(t, d.Reason, "canary metrics")
	fail = false
	d = r.step()
	require.Equal(t, rollout.ActionHold, d.Action)
}

type flaky struct {
	series
	fail *bool
}

func (f *flaky) Stats(ctx context.Context, pool string, w time.Duration) (rollout.PoolStats, error) {
	if *f.fail {
		return rollout.PoolStats{}, context.DeadlineExceeded
	}
	return f.series.Stats(ctx, pool, w)
}
