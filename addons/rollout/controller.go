package rollout

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shiv3/gocpp/gateway/routepolicy"
)

// State is where a rollout is.
type State string

// States.
const (
	StateInit       State = "init"
	StateConverging State = "converging" // a stage was written; waiting for every gateway
	StateSoaking    State = "soaking"
	StateDone       State = "done"
	StateRolledBack State = "rolled_back"
	StateYielded    State = "yielded"
)

// Controller runs one Plan. Build it with New; it is not safe for concurrent
// use (Step and Run are called from one goroutine).
type Controller struct {
	Plan   Plan
	Policy routepolicy.Controller
	Source Source
	Notify Notifier
	// DryRun evaluates and reports but writes nothing: stages advance virtually, a
	// failing gate reports "rollback" and ends the dry run.
	DryRun bool
	// Now replaces time.Now (tests).
	Now func() time.Time

	state      State
	stage      int
	startRev   uint64 // the revision stored before the rollout began
	expectRev  uint64 // the revision this controller last wrote (or startRev in a dry run)
	stageStart time.Time
	pendingRev uint64 // written, waiting for convergence
	fails      int
}

// New returns a Controller.
func New(p Plan, pc routepolicy.Controller, src Source, n Notifier) *Controller {
	return &Controller{Plan: p, Policy: pc, Source: src, Notify: n, state: StateInit}
}

// State returns the current state.
func (c *Controller) State() State { return c.state }

// Stage returns the index of the current stage.
func (c *Controller) Stage() int { return c.stage }

func (c *Controller) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Controller) emit(d Decision) Decision {
	d.Time, d.DryRun = c.now(), c.DryRun
	d.Stage = c.stage
	if c.stage < len(c.Plan.Stages) {
		d.Percent = c.Plan.Stages[c.stage].Percent
	}
	if c.Notify != nil {
		c.Notify.Notify(d)
	}
	return d
}

// Finished reports whether the rollout has ended (done, rolled back, yielded).
func (c *Controller) Finished() bool {
	return c.state == StateDone || c.state == StateRolledBack || c.state == StateYielded
}

// Run steps the rollout every Plan.Interval until it finishes or ctx ends.
func (c *Controller) Run(ctx context.Context) error {
	t := time.NewTicker(c.Plan.interval())
	defer t.Stop()
	for {
		if _, err := c.Step(ctx); err != nil {
			return err
		}
		if c.Finished() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

// Step makes one decision. Transient problems (a metrics source that cannot be
// read, gateways that lag) are reported as a hold or an error decision and
// retried on the next step; only an invalid plan returns an error.
func (c *Controller) Step(ctx context.Context) (Decision, error) {
	if c.Finished() {
		return c.emit(Decision{Action: ActionHold, Reason: "rollout finished: " + string(c.state)}), nil
	}
	if err := c.Plan.Validate(); err != nil {
		return Decision{}, err
	}
	if s, ok := c.Source.(Sampler); ok {
		if err := s.Sample(ctx); err != nil {
			return c.emit(Decision{Action: ActionError, Reason: "metrics: " + err.Error()}), nil
		}
	}
	switch c.state {
	case StateInit:
		return c.start(ctx)
	case StateConverging:
		return c.converge(ctx)
	default:
		return c.soak(ctx)
	}
}

func (c *Controller) start(ctx context.Context) (Decision, error) {
	cur, _, err := c.Policy.Store.Policy(ctx)
	if err != nil {
		return c.emit(Decision{Action: ActionError, Reason: "policy store: " + err.Error()}), nil
	}
	c.startRev, c.expectRev, c.stage = cur, cur, 0
	return c.applyStage(ctx)
}

// applyStage writes the current stage (or, in a dry run, pretends to).
func (c *Controller) applyStage(ctx context.Context) (Decision, error) {
	st := c.Plan.Stages[c.stage]
	name := st.Name
	if name == "" {
		name = fmt.Sprintf("%.4g%%", st.Percent)
	}
	if c.DryRun {
		c.state, c.stageStart = StateSoaking, c.now()
		return c.emit(Decision{Action: ActionApply, Reason: "would apply stage " + name}), nil
	}
	rev, err := c.Policy.Put(ctx, c.Plan.policyFor(st), routepolicy.PutOptions{ExpectRev: c.expectRev})
	if err != nil {
		if errors.Is(err, routepolicy.ErrConflict) {
			c.state = StateYielded
			return c.emit(Decision{Action: ActionYield, Reason: "the policy changed under the controller: " + err.Error()}), nil
		}
		return c.emit(Decision{Action: ActionError, Reason: "apply: " + err.Error()}), nil
	}
	c.expectRev, c.pendingRev, c.state = rev, rev, StateConverging
	return c.emit(Decision{Action: ActionApply, Revision: rev, Reason: "applied stage " + name + ", waiting for the gateways"}), nil
}

func (c *Controller) converge(ctx context.Context) (Decision, error) {
	if c.DryRun || c.pendingRev == 0 {
		c.state = StateSoaking
		return c.soak(ctx)
	}
	st, err := c.Policy.Status(ctx)
	if err != nil {
		return c.emit(Decision{Action: ActionError, Reason: "status: " + err.Error()}), nil
	}
	if st.Revision != c.pendingRev {
		c.state = StateYielded
		return c.emit(Decision{Action: ActionYield, Revision: st.Revision, Reason: fmt.Sprintf("revision %d was written by someone else", st.Revision)}), nil
	}
	var lag []string
	for _, g := range st.Gateways {
		if g.PolicyRev < c.pendingRev {
			lag = append(lag, g.ID)
		}
	}
	if len(lag) > 0 {
		return c.emit(Decision{Action: ActionHold, Revision: c.pendingRev, Reason: "gateways not yet on the revision: " + strings.Join(lag, ", ")}), nil
	}
	c.state, c.stageStart, c.pendingRev = StateSoaking, c.now(), 0
	if s, ok := c.Source.(Sampler); ok {
		_ = s.Sample(ctx) // the baseline of the stage
	}
	return c.emit(Decision{Action: ActionHold, Revision: c.expectRev, Reason: "every gateway runs the stage; soaking"}), nil
}

func (c *Controller) soak(ctx context.Context) (Decision, error) {
	if !c.DryRun {
		st, err := c.Policy.Status(ctx)
		if err != nil {
			return c.emit(Decision{Action: ActionError, Reason: "status: " + err.Error()}), nil
		}
		if st.Revision != c.expectRev {
			c.state = StateYielded
			return c.emit(Decision{Action: ActionYield, Revision: st.Revision, Reason: fmt.Sprintf("revision %d was written by someone else", st.Revision)}), nil
		}
		for _, g := range st.Gateways {
			for _, r := range g.Suspended {
				if r == c.Plan.Rule {
					return c.rollback(ctx, "the gateway guard suspended rule "+r+" on "+g.ID, nil)
				}
			}
		}
	}
	elapsed := c.now().Sub(c.stageStart)
	window := min(c.Plan.window(), elapsed)
	if window < c.Plan.interval() {
		return c.emit(Decision{Action: ActionHold, Revision: c.expectRev, Reason: "warming up: not a full evaluation interval of data yet"}), nil
	}
	canary, err := c.Source.Stats(ctx, c.Plan.canary(), window)
	if err != nil {
		return c.emit(Decision{Action: ActionError, Reason: "canary metrics: " + err.Error()}), nil
	}
	stable, err := c.Source.Stats(ctx, c.Plan.stable(), window)
	if err != nil {
		return c.emit(Decision{Action: ActionError, Reason: "stable metrics: " + err.Error()}), nil
	}
	results := evaluate(c.Plan.Gates, canary, stable)
	results = append(results, c.checks(ctx, window)...)
	switch worst(results) {
	case Fail:
		c.fails++
		if c.fails >= max(c.Plan.FailEvaluations, 1) {
			return c.rollback(ctx, "gate failed: "+firstFail(results), results)
		}
		return c.emit(Decision{Action: ActionHold, Revision: c.expectRev, Gates: results,
			Reason: fmt.Sprintf("gate failing (%d of %d evaluations): %s", c.fails, max(c.Plan.FailEvaluations, 1), firstFail(results))}), nil
	case Insufficient:
		return c.emit(Decision{Action: ActionHold, Revision: c.expectRev, Gates: results, Reason: "no decision: " + results[0].Detail}), nil
	}
	c.fails = 0
	soak := time.Duration(c.Plan.Stages[c.stage].Soak)
	if elapsed < soak {
		return c.emit(Decision{Action: ActionHold, Revision: c.expectRev, Gates: results,
			Reason: fmt.Sprintf("gates pass; soaking (%s of %s)", elapsed.Round(time.Second), soak)}), nil
	}
	if c.stage == len(c.Plan.Stages)-1 {
		c.state = StateDone
		return c.emit(Decision{Action: ActionDone, Revision: c.expectRev, Gates: results,
			Reason: "every stage passed: ready to promote (roll the stable Deployment, then remove the rule)"}), nil
	}
	c.stage++
	d, err := c.applyStage(ctx)
	if d.Action == ActionApply {
		d.Gates, d.Reason = results, "gates passed, advancing: "+d.Reason
		return d, err
	}
	return d, err
}

func firstFail(rs []GateResult) string {
	for _, r := range rs {
		if r.Verdict == Fail {
			return r.Name + " (" + r.Detail + ")"
		}
	}
	return ""
}

func (c *Controller) checks(ctx context.Context, window time.Duration) []GateResult {
	var out []GateResult
	for _, ch := range c.Plan.Gates.Checks {
		q := strings.NewReplacer("{{.Window}}", promWindow(window), "{{.Canary}}", c.Plan.canary(), "{{.Stable}}", c.Plan.stable()).Replace(ch.Query)
		v, err := c.Source.Scalar(ctx, q)
		switch {
		case err != nil:
			out = append(out, GateResult{Name: ch.Name, Verdict: Insufficient, Detail: err.Error()})
		case v > ch.Max:
			out = append(out, GateResult{Name: ch.Name, Verdict: Fail, Detail: fmt.Sprintf("%.4g above %.4g", v, ch.Max)})
		default:
			out = append(out, GateResult{Name: ch.Name, Verdict: Pass, Detail: fmt.Sprintf("%.4g within %.4g", v, ch.Max)})
		}
	}
	return out
}

// rollback restores the policy that was in force before the rollout, as one new
// revision with holds ignored, and ends the rollout.
func (c *Controller) rollback(ctx context.Context, reason string, gates []GateResult) (Decision, error) {
	if c.DryRun {
		c.state = StateRolledBack
		return c.emit(Decision{Action: ActionRollback, Gates: gates, Reason: "would roll back: " + reason}), nil
	}
	var rev uint64
	var err error
	if c.startRev == 0 {
		rev, err = c.Policy.Put(ctx, routepolicy.Policy{}, routepolicy.PutOptions{IgnoreHold: true, Force: true})
	} else {
		rev, _, err = c.Policy.Rollback(ctx, c.startRev)
	}
	if err != nil {
		// Keep trying on the next step: a rollback that failed must not be forgotten.
		return c.emit(Decision{Action: ActionError, Gates: gates, Reason: "rollback failed (will retry): " + err.Error()}), nil
	}
	c.state, c.expectRev = StateRolledBack, rev
	return c.emit(Decision{Action: ActionRollback, Revision: rev, Gates: gates, Reason: "rolled back: " + reason}), nil
}
