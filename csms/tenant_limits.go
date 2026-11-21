package csms

import (
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shiv3/gocpp/core/dispatcher"
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
}

// tenantState is the live state of one tenant.
type tenantState struct {
	id       string
	label    string
	limit    TenantLimit
	sessions atomic.Int64

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
	s := &tenantState{id: id, label: "other", limit: lim, tokens: float64(lim.MessageBurst)}
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
			break
		}
	}
	if t.metrics != nil {
		t.metrics.TenantSessions(s.label, 1)
	}
	var once sync.Once
	return s, func() {
		once.Do(func() {
			s.sessions.Add(-1)
			if t.metrics != nil {
				t.metrics.TenantSessions(s.label, -1)
			}
		})
	}, true
}

// connShare is one session's fair-share credit inside its tenant's bucket.
type connShare struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

// gate returns the inbound gate of one session of s, or nil when the tenant has
// no message budget.
func (t *tenants) gate(s *tenantState) dispatcher.InboundGate {
	if s.limit.MessageRate <= 0 {
		return nil
	}
	cs := &connShare{tokens: math.MaxFloat64}
	reject := &ocppj.CallError{Code: t.cfg.RejectCode, Description: "tenant message rate exceeded"}
	return func(ocppj.Frame) *ocppj.CallError {
		if s.allow(cs, time.Now()) {
			return nil
		}
		if t.metrics != nil {
			t.metrics.TenantRejected(s.label, "messages")
		}
		return reject
	}
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
