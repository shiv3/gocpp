package dispatcher

import (
	"context"
	"sync"

	"github.com/shiv3/gocpp/core/ocppj"
)

// callSlot enforces OCPP's synchronicity rule (1.6J §4.1.1, 2.0.1 Part 4 §4.1):
// at most one outstanding CALL per connection. It is per connection (no state is
// shared between connections) and is the single FIFO that both synchronous
// DoCall and DoCallAsync wait in, so their relative order is the order they
// asked. Waiting costs a list entry; an async call parks no goroutine until its
// turn comes, a synchronous caller parks only its own goroutine.
//
// Only CALLs take the slot. CALLRESULT/CALLERROR/CALLRESULTERROR and SEND go
// straight to the writer.
type callSlot struct {
	mu     sync.Mutex
	busy   bool
	closed bool
	q      []*slotWaiter
	// cbTail is the completion signal of the most recently enqueued async job;
	// the next one waits on it before running its callback so callbacks fire in
	// submission order even though the slot is released before the callback runs.
	cbTail chan struct{}
}

type slotWaiter struct {
	ready   chan struct{} // sync waiter: closed on grant
	start   func()        // async waiter: run (in a new goroutine) on grant
	fail    func(error)   // async waiter: run when removed without being granted
	stop    func() bool   // async waiter: unregisters the ctx watcher
	granted bool
}

func (s *callSlot) remove(w *slotWaiter) bool {
	for i, x := range s.q {
		if x == w {
			copy(s.q[i:], s.q[i+1:])
			s.q[len(s.q)-1] = nil
			s.q = s.q[:len(s.q)-1]
			return true
		}
	}
	return false
}

// release hands the slot to the next waiter, or frees it.
func (s *callSlot) release() {
	s.mu.Lock()
	if s.closed || len(s.q) == 0 {
		s.busy = false
		s.mu.Unlock()
		return
	}
	w := s.q[0]
	s.q[0] = nil
	s.q = s.q[1:]
	w.granted = true
	stop := w.stop
	s.mu.Unlock()
	if w.ready != nil {
		close(w.ready)
		return
	}
	go func() {
		if stop != nil {
			stop()
		}
		w.start()
	}()
}

func (c *Conn) queueLimit() int {
	if c.cfg.AsyncQueueSize > 0 {
		return c.cfg.AsyncQueueSize
	}
	return 64
}

// acquireSlot blocks until the caller owns the slot. It returns
// ocppj.ErrQueueFull at once when the wait queue is full, and gives up when ctx
// or the connection ends. The caller must call c.slot.release().
func (c *Conn) acquireSlot(ctx context.Context) error {
	s := &c.slot
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return context.Cause(c.ctx)
	}
	if !s.busy && len(s.q) == 0 {
		s.busy = true
		s.mu.Unlock()
		return nil
	}
	if len(s.q) >= c.queueLimit() {
		s.mu.Unlock()
		return ocppj.ErrQueueFull
	}
	w := &slotWaiter{ready: make(chan struct{})}
	s.q = append(s.q, w)
	s.mu.Unlock()

	var err error
	select {
	case <-w.ready:
		return nil
	case <-ctx.Done():
		err = ctx.Err()
	case <-c.ctx.Done():
		err = context.Cause(c.ctx)
	}
	s.mu.Lock()
	if w.granted {
		// Granted at the same moment we gave up: pass the slot on.
		s.mu.Unlock()
		s.release()
		return err
	}
	s.remove(w)
	s.mu.Unlock()
	return err
}

// failQueued fails every async job still waiting once the connection ends and
// stops handing out the slot. Synchronous waiters leave on their own via c.ctx.
func (c *Conn) failQueued() {
	s := &c.slot
	cause := context.Cause(c.ctx)
	s.mu.Lock()
	s.closed = true
	var failing []*slotWaiter
	kept := s.q[:0]
	for _, w := range s.q {
		if w.fail != nil {
			failing = append(failing, w)
		} else {
			kept = append(kept, w)
		}
	}
	for i := len(kept); i < len(s.q); i++ {
		s.q[i] = nil
	}
	s.q = kept
	s.mu.Unlock()
	for _, w := range failing {
		if w.stop != nil {
			w.stop()
		}
		w.fail(cause)
	}
}
