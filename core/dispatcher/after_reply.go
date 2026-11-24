package dispatcher

import (
	"context"
	"sync"
)

type afterReplyKey struct{}

// afterReply collects the callbacks one inbound CALL's handler scheduled with
// AfterReply. It is created for every handled CALL and is only locked when a
// handler uses it.
type afterReply struct {
	mu     sync.Mutex
	fns    []func(ctx context.Context)
	closed bool // the reply has been attempted; later registrations are refused
}

// AfterReply schedules fn to run once the CALLRESULT of the inbound CALL that
// ctx belongs to has been written to the socket. It is how a handler orders
// follow-up work after its own reply, for example a GetConfiguration CALL that a
// charge point must only receive after the BootNotification CALLRESULT:
//
//	csms.On(srv, profiles.BootNotification, func(ctx context.Context, c *csms.Conn, req Req) (Resp, error) {
//		csms.AfterReply(ctx, func(ctx context.Context) {
//			_, _ = csms.Call(ctx, c, profiles.GetConfiguration, messages.GetConfigurationRequest{})
//		})
//		return Resp{Status: "Accepted"}, nil
//	})
//
// Callbacks run in registration order on one new goroutine per reply, so they
// never hold up the connection's handler slots and may block on a Call. ctx is
// the connection context: it carries the connection's values and is cancelled
// when the connection goes away. A callback panic is recovered and logged.
//
// fn does not run when the handler fails (a CALLERROR is sent instead), when
// the CALLRESULT cannot be written (the connection is closing), or when the
// connection ends first. AfterReply returns false, and fn never runs, when ctx
// is not the context of a handler running on a connection of this process, or
// when the reply was already sent (call it before the handler returns).
func AfterReply(ctx context.Context, fn func(ctx context.Context)) bool {
	ar, ok := ctx.Value(afterReplyKey{}).(*afterReply)
	if !ok || fn == nil {
		return false
	}
	ar.mu.Lock()
	defer ar.mu.Unlock()
	if ar.closed {
		return false
	}
	ar.fns = append(ar.fns, fn)
	return true
}

// finish closes ar and returns the callbacks to run (nil when there are none or
// ok is false).
func (ar *afterReply) finish(ok bool) []func(ctx context.Context) {
	ar.mu.Lock()
	defer ar.mu.Unlock()
	ar.closed = true
	if !ok {
		ar.fns = nil
	}
	return ar.fns
}

// runAfterReply runs fns in order on a new goroutine.
func (c *Conn) runAfterReply(fns []func(ctx context.Context)) {
	go func() {
		for _, fn := range fns {
			if c.ctx.Err() != nil {
				return
			}
			c.runAfterReplyFn(fn)
		}
	}()
}

func (c *Conn) runAfterReplyFn(fn func(ctx context.Context)) {
	defer func() {
		if r := recover(); r != nil {
			c.cfg.Logger.ErrorContext(c.ctx, "after-reply callback panic recovered", "cp_id", c.id, "panic", r)
		}
	}()
	fn(c.ctx)
}
