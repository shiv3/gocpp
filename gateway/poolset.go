package gateway

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// PoolSet holds one Pool per backend pool name and is the Upstream a link hands
// its gateways: it dispatches each message to the pool the gateway's Router
// chose (Envelope.Route; none means DefaultPool), within the pool by power of two
// choices, with the hold and Nack redispatch of Pool. A pool that has no
// available member falls back to Route.Fallback immediately; a pool that was
// never named by a router receives nothing.
type PoolSet struct {
	wait time.Duration

	mu    sync.Mutex
	pools map[string]*Pool
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

// Pool returns the pool called name, or nil when no member has ever attached.
func (s *PoolSet) Pool(name string) *Pool { return s.get(name, false) }

// Drain drains m out of its pool.
func (s *PoolSet) Drain(ctx context.Context, m *Member) error {
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

// choose applies the fallback rule: the routed pool, else its fallback.
func (s *PoolSet) choose(r *Route) *Pool {
	name := DefaultPool
	fb := ""
	if r != nil {
		if r.Pool != "" {
			name = r.Pool
		}
		fb = r.Fallback
	}
	if p := s.get(name, false); p != nil && p.Available() {
		return p
	}
	if fb != "" {
		if p := s.get(fb, false); p != nil && p.Available() {
			return p
		}
	}
	// Nothing available: the named pool holds the message (a backend may be
	// about to attach), creating it so the wait is observable.
	return s.get(name, true)
}

// Connect implements Upstream.
func (s *PoolSet) Connect(ctx context.Context, req ConnectRequest) (ConnectReply, error) {
	return s.choose(req.Route).Connect(ctx, req)
}

// Handle implements Upstream.
func (s *PoolSet) Handle(ctx context.Context, env Envelope) (Reply, error) {
	p := s.choose(env.Route)
	rep, err := p.Handle(ctx, env)
	if err != nil && env.Route != nil && env.Route.Fallback != "" && isNoBackend(err) {
		// Every member of the routed pool refused (Nack) or vanished: redispatch
		// on the fallback pool within the deadline. Never after a dispatch that
		// may have run (that error is not ErrNoBackend).
		if fp := s.get(env.Route.Fallback, false); fp != nil && fp != p && fp.Available() {
			env.Attempt++
			return fp.Handle(ctx, env)
		}
	}
	return rep, err
}

// Notify implements Upstream.
func (s *PoolSet) Notify(ctx context.Context, env Envelope) error {
	return s.choose(env.Route).Notify(ctx, env)
}

func isNoBackend(err error) bool { return errors.Is(err, ErrNoBackend) }
