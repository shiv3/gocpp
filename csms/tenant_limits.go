package csms

import (
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/ocppj"
)

// TenantResolver names the tenant of an upgrade request. cpID is the charge
// point id (so a mapping table keyed by id works); ok false means the request
// carries no tenant.
type TenantResolver func(r *http.Request, cpID string) (tenant string, ok bool)

// TenantFromRequest adapts a resolver that only looks at the request, such as
// the ones of addons/tenant (FromPathPrefix, FromHeader, ...).
func TenantFromRequest(f func(r *http.Request) (string, bool)) TenantResolver {
	return func(r *http.Request, _ string) (string, bool) { return f(r) }
}

// TenantLimit is the budget of one tenant. Zero values mean unlimited.
type TenantLimit struct {
	// MaxSessions caps the tenant's simultaneous sessions. An upgrade over the cap
	// is refused with 503 and a jittered Retry-After before the WebSocket is
	// accepted, so nothing is established and then torn down. A charger that
	// reconnects over its own stale socket does not count twice.
	MaxSessions int
	// MessageRate is the tenant's budget of inbound CALLs and SENDs per second,
	// summed over its sessions. A message over budget is answered with a CALLERROR
	// (TenantLimits.RejectCode) without reaching the handlers or, on a gateway, the
	// backend; an over-budget SEND is dropped. The session stays up.
	MessageRate float64
	// MessageBurst is the bucket depth (default: one second of MessageRate, at
	// least 1).
	MessageBurst int
	// MaxInFlight caps the tenant's inbound CALLs and SENDs in flight at once (from
	// the moment the frame is admitted until its handler returned and the reply is
	// queued, so on a gateway it covers the backend hop), summed over its sessions.
	// A message over the cap is refused like one over MessageRate. It is a hard
	// ceiling on top of the weighted pool (TenantLimits.InFlightPool).
	MaxInFlight int
	// Weight is the tenant's share of TenantLimits.InFlightPool relative to the
	// other tenants that have sessions (default 1).
	Weight float64
}

// TenantLimits configures per-tenant limits (WithTenantLimits).
type TenantLimits struct {
	// Resolver names the tenant of an upgrade request. When it is nil or finds
	// none, the tenant is the session attribute AttrKey as set by the processing
	// tier at connect time (ConnectReply.Attrs on a gateway,
	// WithSessionAttributes on an embedded server). A connection without a tenant
	// belongs to the tenant "" and gets Default.
	Resolver TenantResolver
	// AttrKey is the session attribute holding the tenant (default "tenant").
	AttrKey string

	// InFlightPool is the number of inbound CALLs and SENDs the server lets run at
	// once across all tenants (0: no pool, only the per-tenant MaxInFlight).
	// Capacity is split by TenantLimit.Weight among the tenants that currently have
	// sessions: a tenant is always admitted up to its weighted share, so a noisy
	// tenant cannot crowd out a quiet one; above its share it may borrow capacity
	// that no other tenant could claim right now (the sum of the other tenants'
	// unused shares stays reserved), so idle capacity is not wasted. A refused
	// message is answered with RejectCode, counted as kind "inflight", and the
	// session stays up.
	InFlightPool int

	// Default applies to tenants not listed in Tenants.
	Default TenantLimit
	// Tenants overrides Default per tenant id.
	Tenants map[string]TenantLimit

	// RejectCode is the CALLERROR code of a refused message (default
	// GenericError: OCPP defines no code for throttling and chargers treat it as a
	// retryable failure).
	RejectCode ocppj.ErrorCode
	// RetryAfter is the base of the jittered Retry-After of a refused upgrade
	// (default 30 s).
	RetryAfter time.Duration
	// MaxMetricTenants bounds the tenant label of the metrics: tenants beyond it
	// are reported as "other" (default 100).
	MaxMetricTenants int
}

// WithTenantLimits limits sessions and inbound message rate per tenant. The
// tenant is resolved once, at connect. Sessions are checked at admission, before
// the upgrade; the message budget is a token bucket per tenant that is shared
// fairly: while it is more than half empty, each session may only use its equal
// share of the rate, so one noisy charger cannot starve the others of its tenant,
// and one tenant never consumes another's budget. Without this option nothing
// changes; a gateway and an embedded server use it the same way (on a gateway it
// is applied at the edge, before the backend hop).
func WithTenantLimits(l TenantLimits) Option {
	return optionFunc(func(c *serverConfig) { c.tenantLimits = &l })
}

// tenants is the per-server tenant table.
type tenants struct {
	cfg     TenantLimits
	metrics observability.TenantMetrics

	mu      sync.Mutex
	byID    map[string]*tenantState
	labeled int

	// active is the set of tenants with at least one session and the sum of their
	// weights, replaced (never mutated) when it changes; the in-flight pool reads it
	// without locking. totalInflight is the pool's current use.
	active        atomic.Pointer[activeSet]
	totalInflight atomic.Int64
}

type activeSet struct {
	tenants     []*tenantState
	totalWeight float64
}

// tenantState is the live state of one tenant.
type tenantState struct {
	id       string
	label    string
	limit    TenantLimit
	sessions atomic.Int64
	inflight atomic.Int64
	weight   float64

	mu     sync.Mutex
	tokens float64
	last   time.Time
}

func newTenants(cfg TenantLimits, m observability.Metrics) *tenants {
	if cfg.AttrKey == "" {
		cfg.AttrKey = "tenant"
	}
	if cfg.RejectCode == "" {
		cfg.RejectCode = ocppj.ErrorCodeGenericError
	}
	if cfg.RetryAfter <= 0 {
		cfg.RetryAfter = 30 * time.Second
	}
	if cfg.MaxMetricTenants <= 0 {
		cfg.MaxMetricTenants = 100
	}
	t := &tenants{cfg: cfg, byID: make(map[string]*tenantState)}
	t.metrics, _ = m.(observability.TenantMetrics)
	return t
}

// resolve names the tenant of a connection.
func (t *tenants) resolve(r *http.Request, cpID string, attrs map[string]string) string {
	if t.cfg.Resolver != nil {
		if id, ok := t.cfg.Resolver(r, cpID); ok {
			return id
		}
	}
	return attrs[t.cfg.AttrKey]
}

func (t *tenants) state(id string) *tenantState {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s := t.byID[id]; s != nil {
		return s
	}
	lim, ok := t.cfg.Tenants[id]
	if !ok {
		lim = t.cfg.Default
	}
	if lim.MessageRate > 0 && lim.MessageBurst <= 0 {
		lim.MessageBurst = max(1, int(math.Ceil(lim.MessageRate)))
	}
	w := lim.Weight
	if w <= 0 {
		w = 1
	}
	s := &tenantState{id: id, label: "other", limit: lim, weight: w, tokens: float64(lim.MessageBurst)}
	if t.labeled < t.cfg.MaxMetricTenants {
		t.labeled++
		s.label = id
	}
	t.byID[id] = s
	return s
}

// admit reserves a session slot of tenant id. It fails when the tenant is at
// its MaxSessions (replacing says the upgrade replaces a live socket of the same
// charger, which is let through: the old one is about to go). release gives the
// slot back and is idempotent.
func (t *tenants) admit(id string, replacing bool) (s *tenantState, release func(), ok bool) {
	s = t.state(id)
	for {
		cur := s.sessions.Load()
		if lim := int64(s.limit.MaxSessions); lim > 0 && cur >= lim && !replacing {
			if t.metrics != nil {
				t.metrics.TenantRejected(s.label, "sessions")
			}
			return s, nil, false
		}
		if s.sessions.CompareAndSwap(cur, cur+1) {
			if cur == 0 {
				t.setActive()
			}
			break
		}
	}
	if t.metrics != nil {
		t.metrics.TenantSessions(s.label, 1)
	}
	var once sync.Once
	return s, func() {
		once.Do(func() {
			if s.sessions.Add(-1) == 0 {
				t.setActive()
			}
			if t.metrics != nil {
				t.metrics.TenantSessions(s.label, -1)
			}
		})
	}, true
}

// setActive rebuilds the set of tenants with sessions after one gained its first
// or lost its last. A
// concurrent 0->1 and 1->0 on the same tenant can interleave; the set is then
// corrected by the session count read under the lock.
func (t *tenants) setActive() {
	if t.cfg.InFlightPool <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var set activeSet
	for _, o := range t.byID {
		if o.sessions.Load() > 0 {
			set.tenants = append(set.tenants, o)
			set.totalWeight += o.weight
		}
	}
	t.active.Store(&set)
}

// connShare is one session's fair-share credit inside its tenant's bucket.
type connShare struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

// tenantConn is the admission state of one session: its fair-share credit and
// the in-flight slots it currently holds.
type tenantConn struct {
	t      *tenants
	s      *tenantState
	share  *connShare
	reject *ocppj.CallError
	limitR *ocppj.CallError

	mu     sync.Mutex
	held   int64
	closed bool
}

// newConn returns the gate state of one session of s, or nil when the tenant has
// neither a message budget nor an in-flight limit.
func (t *tenants) newConn(s *tenantState) *tenantConn {
	if s.limit.MessageRate <= 0 && s.limit.MaxInFlight <= 0 && t.cfg.InFlightPool <= 0 {
		return nil
	}
	return &tenantConn{
		t: t, s: s, share: &connShare{tokens: math.MaxFloat64},
		reject: &ocppj.CallError{Code: t.cfg.RejectCode, Description: "tenant message rate exceeded"},
		limitR: &ocppj.CallError{Code: t.cfg.RejectCode, Description: "tenant in-flight limit exceeded"},
	}
}

// gate is the dispatcher.InboundGate of the session.
func (c *tenantConn) gate(ocppj.Frame) *ocppj.CallError {
	t, s := c.t, c.s
	if s.limit.MessageRate > 0 && !s.allow(c.share, time.Now()) {
		if t.metrics != nil {
			t.metrics.TenantRejected(s.label, "messages")
		}
		return c.reject
	}
	if (s.limit.MaxInFlight > 0 || t.cfg.InFlightPool > 0) && !c.acquire() {
		if t.metrics != nil {
			t.metrics.TenantRejected(s.label, "inflight")
		}
		return c.limitR
	}
	return nil
}

func (c *tenantConn) acquire() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return true // the session is going away; nothing to count
	}
	if !c.t.acquireInflight(c.s) {
		return false
	}
	c.held++
	return true
}

// done releases the slot of a frame whose handler returned.
func (c *tenantConn) done(ocppj.Frame) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.held == 0 {
		return
	}
	c.held--
	c.t.releaseInflight(c.s, 1)
}

// close returns the slots of frames that were admitted but never finished
// because the session ended.
func (c *tenantConn) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	if c.held > 0 {
		c.t.releaseInflight(c.s, c.held)
		c.held = 0
	}
}

// acquireInflight takes one in-flight slot for s, or reports that the tenant is
// refused. The slot is taken first and given back on refusal, so concurrent
// sessions cannot overshoot a limit by racing between check and increment.
func (t *tenants) acquireInflight(s *tenantState) bool {
	mine := s.inflight.Add(1)
	total := t.totalInflight.Add(1)
	if lim := int64(s.limit.MaxInFlight); lim > 0 && mine > lim {
		t.undoInflight(s, 1)
		return false
	}
	if pool := int64(t.cfg.InFlightPool); pool > 0 && !t.poolAdmits(s, mine, total, pool) {
		t.undoInflight(s, 1)
		return false
	}
	if t.metrics != nil {
		if m, ok := t.metrics.(observability.TenantInflightMetrics); ok {
			m.TenantInflight(s.label, 1)
		}
	}
	return true
}

func (t *tenants) undoInflight(s *tenantState, n int64) {
	s.inflight.Add(-n)
	t.totalInflight.Add(-n)
}

func (t *tenants) releaseInflight(s *tenantState, n int64) {
	t.undoInflight(s, n)
	if t.metrics != nil {
		if m, ok := t.metrics.(observability.TenantInflightMetrics); ok {
			m.TenantInflight(s.label, -int(n))
		}
	}
}

// poolAdmits decides whether s, which now holds mine slots with total in use in
// the pool, may keep the slot it just took. Up to its weighted share a tenant is
// always admitted; above it only while the slots left after reserving the
// unused shares of the other tenants with sessions cover it.
func (t *tenants) poolAdmits(s *tenantState, mine, total, pool int64) bool {
	set := t.active.Load()
	if set == nil || set.totalWeight <= 0 {
		return total <= pool
	}
	if mine <= shareOf(s, set, pool) {
		return true
	}
	var reserved int64
	for _, o := range set.tenants {
		if o == s {
			continue
		}
		if unused := shareOf(o, set, pool) - o.inflight.Load(); unused > 0 {
			reserved += unused
		}
	}
	return total <= pool-reserved
}

// shareOf is the weighted guaranteed share of s (at least one slot).
func shareOf(s *tenantState, set *activeSet, pool int64) int64 {
	return max(1, int64(float64(pool)*s.weight/set.totalWeight))
}

// allow takes one message from the tenant's bucket for a session.
//
// Uncontended (the bucket is at least half full) any session may take from it,
// bursting up to the bucket depth. Contended, a session also needs credit from
// its own bucket, which refills at rate/sessions: a session within its equal
// share is never turned away while the tenant has any budget left, a session
// above it is.
func (s *tenantState) allow(cs *connShare, now time.Time) bool {
	rate, burst := s.limit.MessageRate, float64(s.limit.MessageBurst)
	n := float64(max(s.sessions.Load(), 1))
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.last.IsZero() {
		s.tokens = min(burst, s.tokens+now.Sub(s.last).Seconds()*rate)
	}
	s.last = now

	fairBurst := max(1, burst/n)
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.tokens == math.MaxFloat64 { // first message of the session: a full credit
		cs.tokens = fairBurst
	} else {
		cs.tokens = min(fairBurst, cs.tokens+now.Sub(cs.last).Seconds()*rate/n)
	}
	cs.last = now

	if s.tokens < 1 {
		return false
	}
	if s.tokens < burst/2 && cs.tokens < 1 { // contended and over its share
		return false
	}
	s.tokens--
	cs.tokens = max(0, cs.tokens-1)
	return true
}
