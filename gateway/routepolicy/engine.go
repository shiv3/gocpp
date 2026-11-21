package routepolicy

import (
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shiv3/gocpp/gateway"
)

// ErrStaleRevision is returned by Engine.Apply for a revision that is not
// greater than the one in force. It is not a failure: the policy source may
// legitimately redeliver.
var ErrStaleRevision = errors.New("routepolicy: revision is not newer than the one in force")

// Metrics receives the engine's accounting (docs/design/gateway.md 5.4). All
// methods are called on the dispatch path and must not block. A nil Metrics is
// valid.
type Metrics interface {
	// Decision counts one routing decision.
	Decision(rule, pool string, rev uint64)
	// Fallback counts a message sent to the fallback pool.
	Fallback(rule, reason string)
	// Suspended reports a rule entering (true) or leaving (false) suspension.
	Suspended(rule string, on bool)
	// PolicyApplied reports the revision now in force.
	PolicyApplied(rev uint64)
	// PolicyRejected counts an invalid policy that was refused.
	PolicyRejected()
	// PolicyMissing reports whether the engine runs without any policy (it then
	// routes everything to gateway.DefaultPool).
	PolicyMissing(missing bool)
}

// Option configures an Engine.
type Option func(*Engine)

// WithMetrics sets the metrics sink.
func WithMetrics(m Metrics) Option { return func(e *Engine) { e.metrics = m } }

// WithLogger sets the logger (default: discard).
func WithLogger(l *slog.Logger) Option { return func(e *Engine) { e.log = l } }

// WithOnApply sets a callback run after a revision is applied; the gateway
// binary uses it to publish the revision in its lease label "policy_rev".
func WithOnApply(fn func(rev uint64)) Option { return func(e *Engine) { e.onApply = fn } }

// WithOnSuspend sets a callback run when a rule is suspended or resumes; the
// gateway binary uses it to publish the lease label "suspended".
func WithOnSuspend(fn func(suspended []string)) Option { return func(e *Engine) { e.onSuspend = fn } }

// WithClock replaces time.Now (tests).
func WithClock(now func() time.Time) Option { return func(e *Engine) { e.now = now } }

// Engine is the built-in gateway.Router: it applies the policy in force to every
// message. It also implements gateway.RouteObserver (see Engine.RouteFallback).
// An Engine without a policy routes everything to gateway.DefaultPool.
type Engine struct {
	cur atomic.Pointer[compiled]

	metrics   Metrics
	log       *slog.Logger
	onApply   func(uint64)
	onSuspend func([]string)
	now       func() time.Time

	mu       sync.Mutex // guards sessions and applyMu-protected rule state transitions
	sessions map[string]*sessState
	applyMu  sync.Mutex
}

var (
	_ gateway.Router        = (*Engine)(nil)
	_ gateway.RouteObserver = (*Engine)(nil)
)

// NewEngine returns an Engine with no policy.
func NewEngine(opts ...Option) *Engine {
	e := &Engine{sessions: make(map[string]*sessState), now: time.Now, log: slog.New(slog.DiscardHandler)}
	for _, o := range opts {
		o(e)
	}
	if e.metrics != nil {
		e.metrics.PolicyMissing(true)
	}
	return e
}

// Revision returns the revision in force (0 = none).
func (e *Engine) Revision() uint64 {
	if c := e.cur.Load(); c != nil {
		return c.p.Revision
	}
	return 0
}

// Policy returns the policy in force.
func (e *Engine) Policy() (Policy, bool) {
	if c := e.cur.Load(); c != nil {
		return c.p, true
	}
	return Policy{}, false
}

// Suspended lists the currently suspended rules.
func (e *Engine) Suspended() []string {
	c := e.cur.Load()
	if c == nil {
		return nil
	}
	now := e.now()
	var out []string
	for _, r := range c.rules {
		if r.suspendedAt(now) {
			out = append(out, r.name)
		}
	}
	return out
}

// Apply validates p and makes it the policy in force. An invalid policy is
// rejected and the previous one stays; a revision not newer than the current
// one returns ErrStaleRevision.
func (e *Engine) Apply(p Policy) error {
	e.applyMu.Lock()
	defer e.applyMu.Unlock()
	if cur := e.cur.Load(); cur != nil && p.Revision <= cur.p.Revision {
		return ErrStaleRevision
	}
	if err := p.Validate(); err != nil {
		if e.metrics != nil {
			e.metrics.PolicyRejected()
		}
		return err
	}
	// Suspension does not survive a revision.
	if e.metrics != nil {
		for _, name := range e.Suspended() {
			e.metrics.Suspended(name, false)
		}
	}
	e.cur.Store(compile(p))
	if e.metrics != nil {
		e.metrics.PolicyApplied(p.Revision)
		e.metrics.PolicyMissing(false)
	}
	if e.onApply != nil {
		e.onApply(p.Revision)
	}
	e.notifySuspended()
	return nil
}

// ApplyData parses and applies a policy document; rev, when non-zero, overrides
// the document's revision (the store's revision is authoritative).
func (e *Engine) ApplyData(rev uint64, data []byte) error {
	p, err := Parse(data)
	if err != nil {
		if e.metrics != nil {
			e.metrics.PolicyRejected()
		}
		return err
	}
	if rev != 0 {
		p.Revision = rev
	}
	return e.Apply(p)
}

func (e *Engine) notifySuspended() {
	if e.onSuspend != nil {
		e.onSuspend(e.Suspended())
	}
}

// sessState is the per-session memory of hold: busy sessions keep the
// membership they had before they became busy.
type sessState struct {
	in        map[string]bool // rule name -> was a member when last not busy
	busySince time.Time
}

// Route implements gateway.Router.
func (e *Engine) Route(in gateway.RouteInput) gateway.RouteDecision {
	c := e.cur.Load()
	if c == nil {
		return gateway.RouteDecision{Pool: gateway.DefaultPool}
	}
	d := e.decide(c, in)
	if e.metrics != nil {
		e.metrics.Decision(d.Rule, d.Pool, d.Rev)
	}
	if in.Action == "$closed" {
		e.mu.Lock()
		delete(e.sessions, in.CPID)
		e.mu.Unlock()
	}
	return d
}

func (e *Engine) decide(c *compiled, in gateway.RouteInput) gateway.RouteDecision {
	now := e.now()
	var busy bool
	var st *sessState
	if c.hasHold && !c.p.IgnoreHold {
		busy = in.Attrs[BusyAttr] != ""
	}
	for _, r := range c.rules {
		if r.suspendedAt(now) {
			if r.errMode && r.actionOK(in) && r.member(in) {
				// Suspended rule whose actions only the canary handles: refuse, as
				// the design says ("affected actions get CALLERROR").
				return gateway.RouteDecision{Pool: gateway.PoolReject, Rule: r.name, Rev: c.p.Revision}
			}
			continue
		}
		if r.hold && !c.p.IgnoreHold {
			if st == nil {
				st = e.state(in.CPID)
			}
		}
		if r.matches(in, st, busy, now, e) {
			d := gateway.RouteDecision{Pool: r.pool, Rule: r.name, Rev: c.p.Revision}
			if !r.errMode && r.pool != c.def {
				d.Fallback = c.def
			}
			return d
		}
	}
	return gateway.RouteDecision{Pool: c.def, Rev: c.p.Revision}
}

func (e *Engine) state(cpID string) *sessState {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.sessions[cpID]
	if st == nil {
		st = &sessState{in: make(map[string]bool)}
		e.sessions[cpID] = st
	}
	return st
}

// HeldSessions returns the number of sessions currently held, per rule.
func (e *Engine) HeldSessions() map[string]int {
	out := map[string]int{}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, st := range e.sessions {
		if st.busySince.IsZero() {
			continue
		}
		for r, in := range st.in {
			if in {
				out[r]++
			}
		}
	}
	return out
}

// RouteFallback implements gateway.RouteObserver: a fallback suspends the rule
// for its suspend_for (default 60 s), so a flapping pool changes a charger's
// pool at most once per window.
func (e *Engine) RouteFallback(r gateway.Route, reason string) {
	if e.metrics != nil {
		e.metrics.Fallback(r.Rule, reason)
	}
	c := e.cur.Load()
	if c == nil || c.p.Revision != r.Rev {
		return
	}
	if rl := c.byName[r.Rule]; rl != nil && rl.suspend(e.now(), rl.suspendFor) {
		e.log.Warn("routepolicy: rule suspended after fallback", "rule", r.Rule, "reason", reason, "for", rl.suspendFor)
		if e.metrics != nil {
			e.metrics.Suspended(r.Rule, true)
		}
		e.notifySuspended()
	}
}

// RouteOutcome implements gateway.RouteObserver: it feeds the rule's guard.
func (e *Engine) RouteOutcome(r gateway.Route, outcome string) {
	c := e.cur.Load()
	if c == nil || c.p.Revision != r.Rev {
		return
	}
	rl := c.byName[r.Rule]
	if rl == nil || rl.guard == nil {
		return
	}
	failed := outcome == "timeout" || outcome == "stream_broken" || outcome == "callerror_InternalError"
	now := e.now()
	if rl.guard.record(now, failed) && rl.suspend(now, 0) {
		e.log.Warn("routepolicy: guard tripped, rule suspended until the next revision", "rule", r.Rule)
		if e.metrics != nil {
			e.metrics.Suspended(r.Rule, true)
		}
		e.notifySuspended()
	}
}

// guardState counts outcomes over a sliding window of one-second buckets.
type guardState struct {
	mu      sync.Mutex
	min     int
	maxRate float64
	window  int // seconds
	total   []int
	failed  []int
	last    int64 // unix second of the newest bucket
}

func newGuard(g Guard) *guardState {
	w := max(int(time.Duration(g.Window)/time.Second), 1)
	return &guardState{min: g.MinRequests, maxRate: g.MaxErrorRatio, window: w, total: make([]int, w), failed: make([]int, w)}
}

// record adds one outcome and reports whether the window now exceeds the limit.
func (g *guardState) record(now time.Time, failed bool) bool {
	sec := now.Unix()
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.last == 0 {
		g.last = sec
	}
	// Clear the buckets that have aged out since the last outcome.
	for s := g.last + 1; s <= sec && s <= g.last+int64(g.window); s++ {
		i := int(s % int64(g.window))
		g.total[i], g.failed[i] = 0, 0
	}
	if sec > g.last {
		g.last = sec
	}
	i := int(sec % int64(g.window))
	g.total[i]++
	if failed {
		g.failed[i]++
	}
	var t, f int
	for k := range g.total {
		t += g.total[k]
		f += g.failed[k]
	}
	return t >= g.min && float64(f)/float64(t) > g.maxRate
}

// compiled is a Policy prepared for matching, with its rule state.
type compiled struct {
	p       Policy
	def     string
	rules   []*rule
	byName  map[string]*rule
	hasHold bool
}

type rule struct {
	name       string
	pool       string
	attrs      []attrMatch
	ocpp       map[string]bool
	cpids      map[string]bool
	threshold  float64 // bucket < threshold; >= 10000 means everyone
	salt       string
	actions    map[string]bool
	errMode    bool
	hold       bool
	maxHold    time.Duration
	suspendFor time.Duration
	guard      *guardState

	susp atomic.Int64 // unix nanos until which the rule is suspended; max = until the next revision
}

type attrMatch struct {
	key      string
	exact    map[string]bool
	prefixes []string
}

const forever = int64(1<<62 - 1)

func (r *rule) suspendedAt(now time.Time) bool {
	u := r.susp.Load()
	return u != 0 && now.UnixNano() < u
}

// suspend suspends the rule for d (0 = until the next revision) and reports
// whether it was not already suspended.
func (r *rule) suspend(now time.Time, d time.Duration) bool {
	until := forever
	if d > 0 {
		until = now.Add(d).UnixNano()
	}
	for {
		old := r.susp.Load()
		if old != 0 && now.UnixNano() < old {
			return false // already suspended; a flapping pool must not extend its own window
		}
		if r.susp.CompareAndSwap(old, until) {
			return true
		}
	}
}

func compile(p Policy) *compiled {
	c := &compiled{p: p, def: p.DefaultPool(), byName: make(map[string]*rule, len(p.Rules))}
	for _, rl := range p.Rules {
		r := &rule{name: rl.Name, pool: rl.Pool, salt: p.Salt, errMode: rl.OnUnavailable == "error",
			hold: rl.Hold == "busy", maxHold: time.Duration(rl.MaxHold), suspendFor: time.Duration(rl.SuspendFor)}
		if r.maxHold == 0 {
			r.maxHold = DefaultMaxHold
		}
		if r.suspendFor == 0 {
			r.suspendFor = DefaultSuspendFor
		}
		if r.hold {
			c.hasHold = true
		}
		r.threshold = 10000
		if pc := rl.Match.Percent; pc != nil {
			r.threshold = *pc * 100
		}
		for k, vs := range rl.Match.Attrs {
			am := attrMatch{key: k, exact: map[string]bool{}}
			for _, v := range vs {
				if p, ok := strings.CutSuffix(v, "*"); ok {
					am.prefixes = append(am.prefixes, p)
				} else {
					am.exact[v] = true
				}
			}
			r.attrs = append(r.attrs, am)
		}
		if len(rl.Match.OCPP) > 0 {
			r.ocpp = map[string]bool{}
			for _, v := range rl.Match.OCPP {
				r.ocpp[normVersion(v)] = true
			}
		}
		if len(rl.Match.CPIDs) > 0 {
			r.cpids = map[string]bool{}
			for _, id := range rl.Match.CPIDs {
				r.cpids[id] = true
			}
		}
		if len(rl.Actions) > 0 {
			r.actions = map[string]bool{}
			for _, a := range rl.Actions {
				r.actions[a] = true
			}
		}
		if rl.Guard != nil {
			r.guard = newGuard(*rl.Guard)
		}
		c.rules = append(c.rules, r)
		c.byName[r.name] = r
	}
	return c
}

// member evaluates the session-level conditions of the rule: everything except
// the action set.
func (r *rule) member(in gateway.RouteInput) bool {
	if r.cpids != nil && !r.cpids[in.CPID] {
		return false
	}
	if r.ocpp != nil && !r.ocpp[string(in.OCPP)] {
		return false
	}
	for _, a := range r.attrs {
		v, ok := in.Attrs[a.key]
		if !ok {
			return false
		}
		if a.exact[v] {
			continue
		}
		hit := false
		for _, p := range a.prefixes {
			if strings.HasPrefix(v, p) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if r.threshold < 10000 {
		return float64(Bucket(r.salt, in.CPID)) < r.threshold
	}
	return true
}

func (r *rule) actionOK(in gateway.RouteInput) bool {
	return r.actions == nil || r.actions[in.Action]
}

func (r *rule) matches(in gateway.RouteInput, st *sessState, busy bool, now time.Time, e *Engine) bool {
	if !r.actionOK(in) {
		return false
	}
	if st == nil || !r.hold {
		return r.member(in)
	}
	// hold: busy. A busy session keeps the membership it had before it became
	// busy; the memory is refreshed on every message of a session that is not busy.
	e.mu.Lock()
	defer e.mu.Unlock()
	if !busy {
		m := r.member(in)
		st.in[r.name] = m
		st.busySince = time.Time{}
		return m
	}
	if st.busySince.IsZero() {
		st.busySince = now
	}
	prev, known := st.in[r.name]
	if now.Sub(st.busySince) > r.maxHold || !known {
		m := r.member(in)
		st.in[r.name] = m
		return m
	}
	return prev
}
