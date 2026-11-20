package dispatcher

import (
	"context"
	"errors"

	"github.com/shiv3/gocpp/core/ocppj"
)

// AsyncCallback receives the raw response payload, or an error if the call
// failed (timeout, CallError, connection closed, ...). Exactly one of the two
// is meaningful: on error, payload is nil.
type AsyncCallback func(payload []byte, err error)

type asyncJob struct {
	ctx     context.Context
	action  string
	payload []byte
	cb      AsyncCallback
}

// DoCallAsync sends an OCPP Call without blocking and delivers the result to cb.
//
// When SerializeOutboundCalls is set (the default), calls wait in the same
// per-connection FIFO as synchronous DoCall (at most one outstanding request);
// cb fires in submission order, after the call's slot has been released, so a
// callback may itself issue calls. Otherwise each call runs in its own goroutine
// and cb fires as responses arrive.
//
// It returns an error synchronously only if the call could not be accepted: a nil
// callback, a connection that is closed/not started, or (serialized mode) a full
// queue (ocppj.ErrQueueFull). Per-call failures, including ctx being cancelled
// while the call still waits in the queue (it is then never sent), are
// delivered to cb, not returned.
func DoCallAsync(ctx context.Context, c *Conn, action string, payload []byte, cb AsyncCallback) error {
	if cb == nil {
		return errors.New("dispatcher: nil async callback")
	}
	if c.ctx == nil {
		return ocppj.ErrNotConnected
	}
	if c.cfg.SerializeOutboundCalls {
		return c.enqueueAsync(asyncJob{ctx: ctx, action: action, payload: payload, cb: cb})
	}
	if !c.asyncTrackStart() {
		return ocppj.ErrConnClosed
	}
	go func() {
		defer c.asyncWG.Done()
		resp, err := DoCall(ctx, c, action, payload)
		c.runAsyncCallback(action, cb, resp, err)
	}()
	return nil
}

// asyncTrackStart reserves an asyncWG slot unless the connection is closing.
// Gating the Add behind asyncClosed (set under the same mutex in Close) ensures
// asyncWG.Add can never run concurrently with asyncWG.Wait at a zero counter.
func (c *Conn) asyncTrackStart() bool {
	c.asyncMu.Lock()
	defer c.asyncMu.Unlock()
	if c.asyncClosed {
		return false
	}
	c.asyncWG.Add(1)
	return true
}

func (c *Conn) enqueueAsync(job asyncJob) error {
	if !c.asyncTrackStart() {
		return ocppj.ErrConnClosed
	}
	s := &c.slot
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		c.asyncWG.Done()
		return context.Cause(c.ctx)
	}
	immediate := !s.busy && len(s.q) == 0
	if !immediate && len(s.q) >= c.queueLimit() {
		s.mu.Unlock()
		c.asyncWG.Done()
		return ocppj.ErrQueueFull
	}
	prev, own := s.cbTail, make(chan struct{})
	s.cbTail = own
	if immediate {
		s.busy = true
		s.mu.Unlock()
		go c.runAsyncJob(job, prev, own)
		return nil
	}
	w := &slotWaiter{
		start: func() { c.runAsyncJob(job, prev, own) },
		fail: func(err error) {
			c.finishAsync(job, prev, own, nil, err)
		},
	}
	s.q = append(s.q, w)
	s.mu.Unlock()

	if job.ctx.Done() != nil {
		stop := context.AfterFunc(job.ctx, func() {
			s.mu.Lock()
			removed := !w.granted && s.remove(w)
			s.mu.Unlock()
			if removed {
				w.fail(job.ctx.Err())
			}
		})
		s.mu.Lock()
		if w.granted {
			s.mu.Unlock()
			stop()
		} else {
			w.stop = stop
			s.mu.Unlock()
		}
	}
	return nil
}

// runAsyncJob runs a job that owns the call slot.
func (c *Conn) runAsyncJob(job asyncJob, prev, own chan struct{}) {
	resp, err := c.call(job.ctx, job.action, job.payload, false)
	c.slot.release()
	c.finishAsync(job, prev, own, resp, err)
}

// finishAsync delivers the callback after the previous job's callback.
func (c *Conn) finishAsync(job asyncJob, prev, own chan struct{}, resp []byte, err error) {
	defer c.asyncWG.Done()
	defer close(own)
	if prev != nil {
		<-prev
	}
	c.runAsyncCallback(job.action, job.cb, resp, err)
}

// runAsyncCallback invokes an application callback and recovers from a panic.
// The callbacks run on goroutines owned by the connection (the serialized
// worker, or a per-call goroutine), where an unrecovered panic would take down
// the whole process and with it every other connection; the worker must also
// keep serving the rest of its queue.
func (c *Conn) runAsyncCallback(action string, cb AsyncCallback, payload []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			c.cfg.Logger.ErrorContext(c.ctx, "async callback panic recovered",
				"cp_id", c.id, "action", action, "panic", r)
		}
	}()
	cb(payload, err)
}
