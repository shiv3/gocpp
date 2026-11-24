package msglog

import (
	"sync"
	"time"
)

// Filter selects entries.
type Filter struct {
	// CPID matches the charge point id exactly ("" = any).
	CPID string
	// Action matches the action exactly ("" = any); a response matches the
	// action of the CALL it answers.
	Action string
	// Direction is In, Out or "" (both).
	Direction string
	// SinceSeq keeps entries with a larger seq when HasSinceSeq is set
	// (0 = from the start).
	SinceSeq    int64
	HasSinceSeq bool
	// SinceTime keeps entries at or after it (zero = no bound).
	SinceTime time.Time
}

func (f Filter) bounded() bool { return f.HasSinceSeq || !f.SinceTime.IsZero() }

// Match reports whether e passes f.
func (f Filter) Match(e Entry) bool {
	if f.CPID != "" && e.CPID != f.CPID {
		return false
	}
	if f.Action != "" && e.Action != f.Action {
		return false
	}
	if f.Direction != "" && e.Direction != f.Direction {
		return false
	}
	if f.HasSinceSeq && e.Seq <= f.SinceSeq {
		return false
	}
	if !f.SinceTime.IsZero() && e.Time.Before(f.SinceTime) {
		return false
	}
	return true
}

// Ring keeps the last entries in memory and feeds live subscribers.
type Ring struct {
	mu   sync.Mutex
	buf  []Entry
	next int // index of the next write
	full bool
	subs map[*Subscription]struct{}
}

// NewRing returns a ring holding up to size entries (size >= 1).
func NewRing(size int) *Ring {
	if size < 1 {
		size = 1
	}
	return &Ring{buf: make([]Entry, size), subs: map[*Subscription]struct{}{}}
}

// Add is the Sink of the ring.
func (r *Ring) Add(e Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.next] = e
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
	for s := range r.subs {
		if !s.filter.Match(e) {
			continue
		}
		select {
		case s.ch <- e:
		default:
			// A reader that cannot keep up is dropped; it reconnects with
			// Last-Event-ID and replays from the ring.
			delete(r.subs, s)
			close(s.ch)
		}
	}
}

// Clear drops every stored entry (sequence numbers keep counting).
func (r *Ring) Clear() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.lenLocked()
	clear(r.buf)
	r.next, r.full = 0, false
	return n
}

// Len returns the number of stored entries.
func (r *Ring) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lenLocked()
}

func (r *Ring) lenLocked() int {
	if r.full {
		return len(r.buf)
	}
	return r.next
}

// each calls fn for the stored entries, oldest first, until fn returns false.
func (r *Ring) eachLocked(fn func(Entry) bool) {
	n := r.lenLocked()
	start := 0
	if r.full {
		start = r.next
	}
	for i := 0; i < n; i++ {
		if !fn(r.buf[(start+i)%len(r.buf)]) {
			return
		}
	}
}

// Query returns up to limit matching entries, oldest first: with a since
// bound the first ones after it (paging forward), otherwise the most recent.
// limit <= 0 means no limit.
func (r *Ring) Query(f Filter, limit int) []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.queryLocked(f, limit)
}

func (r *Ring) queryLocked(f Filter, limit int) []Entry {
	out := []Entry{}
	forward := f.bounded()
	r.eachLocked(func(e Entry) bool {
		if !f.Match(e) {
			return true
		}
		out = append(out, e)
		return !forward || limit <= 0 || len(out) < limit
	})
	if !forward && limit > 0 && len(out) > limit {
		out = append([]Entry(nil), out[len(out)-limit:]...)
	}
	return out
}

// Subscription is a live feed of matching entries.
type Subscription struct {
	filter Filter
	ch     chan Entry
}

// C delivers the entries; it is closed when the subscriber fell behind or
// was cancelled.
func (s *Subscription) C() <-chan Entry { return s.ch }

// Subscribe returns the stored entries matching f (the backlog, only when f
// has a since bound) and a subscription for the following ones, atomically:
// nothing is lost or delivered twice between the two.
func (r *Ring) Subscribe(f Filter, buffer int) ([]Entry, *Subscription) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var backlog []Entry
	if f.bounded() {
		backlog = r.queryLocked(f, 0)
	}
	s := &Subscription{filter: f, ch: make(chan Entry, buffer)}
	r.subs[s] = struct{}{}
	return backlog, s
}

// Unsubscribe ends s (idempotent).
func (r *Ring) Unsubscribe(s *Subscription) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.subs[s]; ok {
		delete(r.subs, s)
		close(s.ch)
	}
}
