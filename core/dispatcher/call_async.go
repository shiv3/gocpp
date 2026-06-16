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
// When SerializeOutboundCalls is set, calls are appended to a per-connection FIFO
// queue and sent one at a time by a single worker goroutine (at most one
// outstanding request); cb fires in submission order. Otherwise each call runs in
// its own goroutine and cb fires as responses arrive.
//
// It returns an error synchronously only if the call could not be accepted: a nil
// callback, a connection that is closed/not started, or (serialized mode) a full
// queue (ocppj.ErrQueueFull). Per-call failures are delivered to cb, not returned.
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
		cb(DoCall(ctx, c, action, payload))
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
	c.asyncMu.Lock()
	if c.asyncClosed {
		c.asyncMu.Unlock()
		return ocppj.ErrConnClosed
	}
	if c.asyncQ == nil {
		size := c.cfg.AsyncQueueSize
		if size <= 0 {
			size = 64
		}
		c.asyncQ = make(chan asyncJob, size)
		c.asyncWG.Add(1)
		go c.asyncWorker()
	}
	q := c.asyncQ
	c.asyncMu.Unlock()

	select {
	case q <- job:
		return nil
	case <-c.ctx.Done():
		return context.Cause(c.ctx)
	default:
		return ocppj.ErrQueueFull
	}
}

func (c *Conn) asyncWorker() {
	defer c.asyncWG.Done()
	for {
		select {
		case <-c.ctx.Done():
			c.drainAsync()
			return
		case job := <-c.asyncQ:
			job.cb(DoCall(job.ctx, c, job.action, job.payload))
		}
	}
}

// drainAsync fails any still-queued jobs once the connection is closing.
func (c *Conn) drainAsync() {
	cause := context.Cause(c.ctx)
	for {
		select {
		case job := <-c.asyncQ:
			job.cb(nil, cause)
		default:
			return
		}
	}
}
