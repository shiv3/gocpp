package gateway

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// PoolSet holds one Pool per backend pool name and is the Upstream a link hands
// its gateways: it dispatches each message to the pool the gateway's Router
// chose (Envelope.Route; none means DefaultPool), within the pool by power of two
// choices, with the hold and Nack redispatch of Pool. A pool that has no
// available member falls back to Route.Fallback immediately; a pool that was
// never named by a router receives nothing.
//
// Experimental: this API may change before v1.0.
type PoolSet struct {
	wait time.Duration
	obs  atomic.Pointer[RouteObserver]

	mu        sync.Mutex
	pools     map[string]*Pool
	unhandled map[[2]string]uint64 // {pool, action} -> routings to a pool without a handler
	onUnhand  func(pool, action string)
}

// SetObserver sets the RouteObserver (nil removes it).
func (s *PoolSet) SetObserver(o RouteObserver) {
	if o == nil {
		s.obs.Store(nil)
		return
	}
	s.obs.Store(&o)
}

func (s *PoolSet) observer() RouteObserver {
	if p := s.obs.Load(); p != nil {
		return *p
	}
	return nil
}

// NewPoolSet returns a PoolSet whose pools hold for wait (<= 0: DefaultUpstreamWait).
func NewPoolSet(wait time.Duration) *PoolSet {
	return &PoolSet{wait: wait, pools: map[string]*Pool{DefaultPool: NewPool(wait)}}
}

func (s *PoolSet) get(name string, create bool) *Pool {
	if name == "" {
		name = DefaultPool
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.pools[name]
	if p == nil && create {
		p = NewPool(s.wait)
		s.pools[name] = p
	}
	return p
}

// Add attaches a backend to the pool named in info. RoleCaller backends take no
// inbound work and are not added (nil is returned).
func (s *PoolSet) Add(info BackendInfo, up Upstream) *Member {
	if info.Role == RoleCaller {
		return nil
	}
	return s.get(info.Pool, true).Add(info, up)
}

// SetUnhandledObserver registers fn to be called (synchronously, keep it cheap)
// whenever a message is routed to a pool in which no available member serves
// its action (BackendInfo.Actions), once per such message.
func (s *PoolSet) SetUnhandledObserver(fn func(pool, action string)) {
	s.mu.Lock()
	s.onUnhand = fn
	s.mu.Unlock()
}

// Unhandled reports, per pool and action, how many messages were routed to a
// pool without a member that serves the action since the PoolSet was created.
func (s *PoolSet) Unhandled() map[[2]string]uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[[2]string]uint64, len(s.unhandled))
	for k, v := range s.unhandled {
		out[k] = v
	}
	return out
}

func (s *PoolSet) noteUnhandled(pool, action string) {
	s.mu.Lock()
	if s.unhandled == nil {
		s.unhandled = make(map[[2]string]uint64)
	}
	s.unhandled[[2]string{pool, action}]++
	fn := s.onUnhand
	s.mu.Unlock()
	if fn != nil {
		fn(pool, action)
	}
}

// Pool returns the pool called name, or nil when no member has ever attached.
func (s *PoolSet) Pool(name string) *Pool { return s.get(name, false) }

// Drain drains m out of its pool.
//
// A nil member (a RoleCaller backend has none) is a no-op: it has no pool to
// leave and takes no dispatched work.
func (s *PoolSet) Drain(ctx context.Context, m *Member) error {
	if m == nil {
		return nil
	}
	return s.get(m.info.Pool, true).Drain(ctx, m)
}

// Available reports whether pool name has an attached, non-draining member.
func (s *PoolSet) Available(name string) bool {
	p := s.get(name, false)
	return p != nil && p.Available()
}

// Len returns the number of attached members over all pools.
func (s *PoolSet) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, p := range s.pools {
		n += p.Len()
	}
	return n
}

// PoolStatus describes one pool.
type PoolStatus struct {
	Name    string
	Members []BackendInfo
}

// Status lists the pools and their members, by name.
func (s *PoolSet) Status() []PoolStatus {
	s.mu.Lock()
	names := make([]string, 0, len(s.pools))
	for n := range s.pools {
		names = append(names, n)
	}
	s.mu.Unlock()
	sort.Strings(names)
	out := make([]PoolStatus, 0, len(names))
	for _, n := range names {
		out = append(out, PoolStatus{Name: n, Members: s.get(n, false).Members()})
	}
	return out
}

// choose applies the fallback rule: the routed pool, else its fallback. reason
// is "unavailable" when the fallback was taken.
func (s *PoolSet) choose(r *Route) (p *Pool, reason string) {
	name := DefaultPool
	fb := ""
	if r != nil {
		if r.Pool != "" {
			name = r.Pool
		}
		fb = r.Fallback
	}
	if p := s.get(name, false); p != nil && p.Available() {
		return p, ""
	}
	if fb != "" {
		if p := s.get(fb, false); p != nil && p.Available() {
			return p, "unavailable"
		}
	}
	// Nothing available: the named pool holds the message (a backend may be
	// about to attach), creating it so the wait is observable.
	return s.get(name, true), ""
}

func (s *PoolSet) fellBack(r *Route, reason string) {
	if o := s.observer(); o != nil && r != nil && reason != "" {
		o.RouteFallback(*r, reason)
	}
}

// chooseFor is choose for a message with an OCPP action: when the chosen pool
// has no available member that serves it (every member lists its actions and
// none lists this one), the fallback pool takes it if that one does serve it;
// without a usable fallback the routed pool keeps it (it will answer
// NotImplemented, as a backend without a handler does) and the miss is counted.
// restrict is the action members must serve, "" when the pool is kept anyway;
// reason is choose's ("unavailable" when the pool was replaced because it has no
// member; a per-action fallback is not a pool failure and reports none).
func (s *PoolSet) chooseFor(r *Route, kind Kind, action string) (p *Pool, restrict, reason string) {
	p, reason = s.choose(r)
	if action == "" || (kind != KindCall && kind != KindSend) || !p.Available() {
		return p, "", reason
	}
	if p.Handles(action) {
		return p, action, reason
	}
	s.noteUnhandled(poolName(r), action)
	if r != nil && r.Fallback != "" {
		if fp := s.get(r.Fallback, false); fp != nil && fp != p && fp.Available() && fp.Handles(action) {
			return fp, action, reason
		}
	}
	return p, "", reason
}

func poolName(r *Route) string {
	if r == nil || r.Pool == "" {
		return DefaultPool
	}
	return r.Pool
}

// Connect implements Upstream.
func (s *PoolSet) Connect(ctx context.Context, req ConnectRequest) (ConnectReply, error) {
	if req.Route != nil && req.Route.Pool == PoolReject {
		return ConnectReply{}, ErrNoBackend
	}
	p, reason := s.choose(req.Route)
	s.fellBack(req.Route, reason)
	return p.Connect(ctx, req)
}

// Handle implements Upstream.
func (s *PoolSet) Handle(ctx context.Context, env Envelope) (Reply, error) {
	if env.Route != nil && env.Route.Pool == PoolReject {
		return Reply{}, ErrNoBackend
	}
	p, restrict, reason := s.chooseFor(env.Route, env.Kind, env.Action)
	s.fellBack(env.Route, reason)
	rep, err := p.handle(ctx, env, restrict)
	if err != nil && env.Route != nil && env.Route.Fallback != "" && isNoBackend(err) {
		// Every member of the routed pool refused (Nack) or vanished: redispatch
		// on the fallback pool within the deadline. Never after a dispatch that
		// may have run (that error is not ErrNoBackend).
		if fp := s.get(env.Route.Fallback, false); fp != nil && fp != p && fp.Available() {
			s.fellBack(env.Route, "nack")
			env.Attempt++
			return fp.Handle(ctx, env)
		}
	}
	if o := s.observer(); o != nil && env.Route != nil && env.Route.Rule != "" && reason == "" && p != nil && s.isRoutedPool(env.Route, p) {
		o.RouteOutcome(*env.Route, Outcome(rep, err))
	}
	return rep, err
}

// isRoutedPool reports whether p is the pool r names (not its fallback).
func (s *PoolSet) isRoutedPool(r *Route, p *Pool) bool {
	return s.get(r.Pool, false) == p
}

// Outcome classifies the end of an inbound CALL for RouteObserver.RouteOutcome.
func Outcome(rep Reply, err error) string {
	switch {
	case err != nil && errors.Is(err, ErrNoBackend):
		return "no_backend"
	case err != nil && errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case err != nil:
		return "stream_broken"
	case rep.Error != nil && rep.Error.Kind == ErrKindCallError:
		return "callerror_" + rep.Error.Code
	case rep.Error != nil:
		return "callerror_InternalError"
	}
	return "result"
}

// Notify implements Upstream.
func (s *PoolSet) Notify(ctx context.Context, env Envelope) error {
	if env.Route != nil && env.Route.Pool == PoolReject {
		return ErrNoBackend
	}
	p, restrict, reason := s.chooseFor(env.Route, env.Kind, env.Action)
	s.fellBack(env.Route, reason)
	return p.notify(ctx, env, restrict)
}

func isNoBackend(err error) bool { return errors.Is(err, ErrNoBackend) }

// Remove detaches m at once (a broken stream), without waiting for its work.
func (s *PoolSet) Remove(m *Member) {
	if m != nil {
		s.get(m.info.Pool, true).Remove(m)
	}
}
