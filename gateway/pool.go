package gateway

import (
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultUpstreamWait is how long a gateway holds an inbound CALL for a backend
// to become available before it gives up (the charger then gets an
// InternalError CALLERROR, well inside its own timeout).
const DefaultUpstreamWait = 20 * time.Second

// Pool is an Upstream over a changing set of backends. It is the selection and
// hold logic every link shares: power-of-two-choices on in-flight count across
// attached, non-draining members; a hold of up to UpstreamWait while the set is
// empty; redispatch to another member on Nack or ErrNotDelivered, within the
// envelope's deadline, and never after a request may have been executed.
//
// Experimental: this API may change before v1.0.
type Pool struct {
	wait time.Duration

	mu      sync.Mutex
	members []*Member
	changed chan struct{} // closed and replaced whenever the set changes
}

// Member is one backend in a Pool.
type Member struct {
	info     BackendInfo
	id       string
	up       Upstream
	inflight atomic.Int64
	draining atomic.Bool
	idle     chan struct{} // closed when draining and inflight reaches 0
	idleOnce sync.Once
}

// ID returns the id the member was added under.
func (m *Member) ID() string { return m.id }

// InFlight returns the number of requests currently dispatched to the member.
func (m *Member) InFlight() int64 { return m.inflight.Load() }

// NewPool returns a Pool. wait <= 0 means DefaultUpstreamWait.
func NewPool(wait time.Duration) *Pool {
	if wait <= 0 {
		wait = DefaultUpstreamWait
	}
	return &Pool{wait: wait, changed: make(chan struct{})}
}

// Members returns the attached backends' announcements.
func (p *Pool) Members() []BackendInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]BackendInfo, 0, len(p.members))
	for _, m := range p.members {
		out = append(out, m.info)
	}
	return out
}

// Available reports whether a non-draining member is attached.
func (p *Pool) Available() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, m := range p.members {
		if !m.draining.Load() {
			return true
		}
	}
	return false
}

// Handles reports whether a non-draining member serves action (see
// BackendInfo.Handles).
func (p *Pool) Handles(action string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, m := range p.members {
		if !m.draining.Load() && m.info.Handles(action) {
			return true
		}
	}
	return false
}

// advertises reports whether a member explicitly lists action.
func (p *Pool) advertises(action string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, m := range p.members {
		if slices.Contains(m.info.Actions, action) {
			return true
		}
	}
	return false
}

// Add attaches a backend that announced itself with info. The returned Member
// can be drained with Drain.
func (p *Pool) Add(info BackendInfo, up Upstream) *Member {
	if info.Pool == "" {
		info.Pool = DefaultPool
	}
	id := info.ID
	m := &Member{id: id, up: up, idle: make(chan struct{}), info: info}
	p.mu.Lock()
	p.members = append(p.members, m)
	p.signalLocked()
	p.mu.Unlock()
	return m
}

func (p *Pool) signalLocked() {
	close(p.changed)
	p.changed = make(chan struct{})
}

// Len returns the number of attached members, draining ones included.
func (p *Pool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.members)
}

// Drain stops new work going to m and waits until its in-flight requests have
// finished or ctx ends, then removes it. It returns ctx.Err() if work was still
// in flight.
func (p *Pool) Drain(ctx context.Context, m *Member) error {
	p.markDraining(m)
	if m.inflight.Load() == 0 {
		m.idleOnce.Do(func() { close(m.idle) })
	}
	var err error
	select {
	case <-m.idle:
	case <-ctx.Done():
		err = ctx.Err()
	}
	p.Remove(m)
	return err
}

// markDraining flags m under the pool lock. pick checks the flag and reserves
// its in-flight slot under the same lock, so a request either sees m draining
// or is counted in m.inflight before Drain looks at it; without the lock a
// request could be handed to a member whose Drain had already returned.
func (p *Pool) markDraining(m *Member) {
	p.mu.Lock()
	m.draining.Store(true)
	p.mu.Unlock()
}

// Remove detaches m at once.
func (p *Pool) Remove(m *Member) {
	p.mu.Lock()
	m.draining.Store(true)
	for i, x := range p.members {
		if x == m {
			p.members = append(p.members[:i], p.members[i+1:]...)
			break
		}
	}
	p.signalLocked()
	p.mu.Unlock()
}

func (m *Member) done() {
	if m.inflight.Add(-1) == 0 && m.draining.Load() {
		m.idleOnce.Do(func() { close(m.idle) })
	}
}

// pick chooses a member not in tried by power of two choices, waiting up to
// until for one to appear.
//
// With failFast, a pick that finds no eligible member left to try (all of them
// refused, or vanished since the pool was chosen) returns ErrNoBackend at once
// instead of waiting for another member to attach: the caller has somewhere else
// to send the message.
func (p *Pool) pick(ctx context.Context, need, action string, tried map[*Member]bool, until time.Time, failFast bool) (*Member, error) {
	for {
		p.mu.Lock()
		var cand [2]*Member
		n := 0
		// Reservoir-sample two eligible members.
		seen := 0
		for _, m := range p.members {
			if m.draining.Load() || (need != "" && !m.info.Has(need)) || (action != "" && !m.info.Handles(action)) {
				continue
			}
			if tried[m] {
				continue
			}
			seen++
			switch {
			case n < 2:
				cand[n] = m
				n++
			default:
				if j := rand.IntN(seen); j < 2 {
					cand[j] = m
				}
			}
		}
		ch := p.changed
		var chosen *Member
		switch n {
		case 1:
			chosen = cand[0]
		case 2:
			chosen = cand[0]
			a, b := chosen.inflight.Load(), cand[1].inflight.Load()
			// Ties are broken at random: with an idle pool (one charger, requests
			// strictly one after another) a fixed order would always pick the
			// same member.
			if b < a || (b == a && rand.IntN(2) == 0) {
				chosen = cand[1]
			}
		}
		if chosen != nil {
			// Reserve under the lock so a concurrent Drain sees the request.
			chosen.inflight.Add(1)
		}
		p.mu.Unlock()
		if chosen != nil {
			return chosen, nil
		}
		if failFast {
			return nil, ErrNoBackend
		}
		wait := time.Until(until)
		if wait <= 0 {
			return nil, ErrNoBackend
		}
		t := time.NewTimer(wait)
		select {
		case <-ch:
			t.Stop()
		case <-t.C:
			return nil, ErrNoBackend
		case <-ctx.Done():
			t.Stop()
			return nil, ctx.Err()
		}
	}
}

// holdUntil is the latest moment to keep waiting for a backend.
func (p *Pool) holdUntil(deadline time.Time) time.Time {
	until := time.Now().Add(p.wait)
	if !deadline.IsZero() && deadline.Before(until) {
		until = deadline
	}
	return until
}

// needCap is the capability a member must advertise to receive env: a SEND
// needs send.21, so an old backend is never handed one it would answer
// ErrUnsupported to.
func needCap(env Envelope) string {
	if env.Kind == KindSend {
		return CapSend21
	}
	return ""
}

// Connect implements Upstream.
func (p *Pool) Connect(ctx context.Context, req ConnectRequest) (ConnectReply, error) {
	var out ConnectReply
	err := p.dispatch(ctx, "", "", time.Time{}, false, func(m *Member) (bool, error) {
		r, err := m.up.Connect(ctx, req)
		out = r
		return false, err
	})
	return out, err
}

// Handle implements Upstream.
func (p *Pool) Handle(ctx context.Context, env Envelope) (Reply, error) {
	rep, _, err := p.handle(ctx, env, "")
	return rep, err
}

// handle is Handle restricted to members that serve action ("" = any member).
func (p *Pool) handle(ctx context.Context, env Envelope, action string) (Reply, uint32, error) {
	return p.handleFast(ctx, env, action, false)
}

// handleFast is handle; with failFast it gives up with ErrNoBackend as soon as
// every eligible member has refused the message, rather than holding it for a
// member to attach (the caller redispatches on a fallback pool). next is the
// Attempt number a further dispatch of env (to another pool) must carry, so the
// number never goes back.
func (p *Pool) handleFast(ctx context.Context, env Envelope, action string, failFast bool) (rep Reply, next uint32, err error) {
	var out Reply
	attempt := env.Attempt
	if attempt == 0 {
		attempt = 1
	}
	err = p.dispatch(ctx, "", action, env.Deadline, failFast, func(m *Member) (bool, error) {
		e := env
		e.Attempt = attempt
		attempt++
		r, err := m.up.Handle(ctx, e)
		if err != nil {
			return false, err
		}
		if r.Nack {
			return true, nil
		}
		out = r
		return false, nil
	})
	return out, attempt, err
}

// Notify implements Upstream.
func (p *Pool) Notify(ctx context.Context, env Envelope) error {
	return p.notify(ctx, env, "", false)
}

func (p *Pool) notify(ctx context.Context, env Envelope, action string, failFast bool) error {
	return p.dispatch(ctx, needCap(env), action, env.Deadline, failFast, func(m *Member) (bool, error) {
		return false, m.up.Notify(ctx, env)
	})
}

// dispatch runs fn on a member, moving to another one when fn asks to retry
// (Nack) or fails with ErrNotDelivered.
func (p *Pool) dispatch(ctx context.Context, need, action string, deadline time.Time, failFast bool, fn func(m *Member) (retry bool, err error)) error {
	until := p.holdUntil(deadline)
	var tried map[*Member]bool
	for {
		m, err := p.pick(ctx, need, action, tried, until, failFast)
		if err != nil {
			return err
		}
		retry, err := func() (bool, error) {
			defer m.done()
			return fn(m)
		}()
		if err != nil && errors.Is(err, ErrNotDelivered) {
			retry, err = true, nil
		}
		if err != nil {
			return err
		}
		if !retry {
			return nil
		}
		if tried == nil {
			tried = make(map[*Member]bool)
		}
		tried[m] = true
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			return ErrNoBackend
		}
	}
}
