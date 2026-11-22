package csms

import (
	"context"
	"fmt"
	"github.com/shiv3/gocpp/core/codec"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/ocppj"
)

// On registers a typed handler for an inbound message (sent by the charge point).
func On[Req, Resp any](s *Server, m ocppj.Message[Req, Resp], h func(ctx context.Context, c *Conn, req Req) (Resp, error)) error {
	if err := dispatcher.CheckDirection(dispatcher.RoleCSMS, dispatcher.OpHandle, m.Direction); err != nil {
		return err
	}
	typed := func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) {
		var req Req
		if err := codec.Unmarshal(payload, &req); err != nil {
			return nil, dispatcher.NewDecodeError(err)
		}
		resp, err := h(ctx, c, req)
		if err != nil {
			return nil, err
		}
		return codec.Marshal(resp)
	}
	// The same closure is kept in a table typed on *Conn so that a processing
	// tier behind a gateway can run it with a Conn that is not backed by a
	// dispatcher.Conn (see Server.handler).
	s.handlers.setCall(m.Action, typed)
	s.reg.Register(m.Action, func(ctx context.Context, dc *dispatcher.Conn, payload []byte) ([]byte, error) {
		return typed(ctx, &Conn{inner: dc}, payload)
	})
	return nil
}

// OnAny registers a catch-all handler for inbound CALLs: it serves every action
// that has no handler registered with On, and receives the action name and the
// raw JSON payload. Pass nil to remove it. It is for embedders that route
// untyped messages themselves (a compatibility layer, a proxy); typed handlers
// registered with On take precedence (a gateway server forwards every action
// itself and must not use OnAny), and a handler error is mapped like any
// other (return a *dispatcher.RawCallError to choose the CALLERROR sent).
//
// Experimental: this API may change before v1.0.
func OnAny(s *Server, h func(ctx context.Context, c *Conn, action string, payload []byte) ([]byte, error)) {
	if h == nil {
		s.reg.SetFallback(nil)
		return
	}
	s.reg.SetFallback(func(ctx context.Context, dc *dispatcher.Conn, action string, payload []byte) ([]byte, error) {
		return h(ctx, &Conn{inner: dc}, action, payload)
	})
}

// CallRaw sends an action with a raw JSON payload to a connected charge point and
// returns the raw response. Used by tools/tests that operate on untyped messages
// (the typed Call is preferred for application code).
func CallRaw(ctx context.Context, c *Conn, action string, payload []byte) ([]byte, error) {
	return doCall(ctx, c, action, payload)
}

// doCall sends on the local connection, or forwards through the router when c is
// a handle for a charge point held by another instance.
func doCall(ctx context.Context, c *Conn, action string, payload []byte) ([]byte, error) {
	if c.inner == nil && c.gw != nil && c.srv != nil && c.srv.backend != nil {
		return c.srv.callGateway(ctx, c, action, payload)
	}
	if c.inner == nil {
		return c.srv.callRemote(ctx, c.remoteID, action, payload)
	}
	return dispatcher.DoCall(ctx, c.inner, action, payload)
}

// doCallAsync is the asynchronous form of doCall. A remote call has no outbound
// queue on this instance, so it simply runs in its own goroutine.
func doCallAsync(ctx context.Context, c *Conn, action string, payload []byte, cb func([]byte, error)) error {
	if c.inner == nil && c.gw != nil && c.srv != nil && c.srv.backend != nil {
		go func() { cb(c.srv.callGateway(ctx, c, action, payload)) }()
		return nil
	}
	if c.inner == nil {
		go func() { cb(c.srv.callRemote(ctx, c.remoteID, action, payload)) }()
		return nil
	}
	return dispatcher.DoCallAsync(ctx, c.inner, action, payload, cb)
}

// Call sends a typed message to a connected charge point and returns the response.
func Call[Req, Resp any](ctx context.Context, c *Conn, m ocppj.Message[Req, Resp], req Req) (Resp, error) {
	var zero Resp
	if err := dispatcher.CheckDirection(dispatcher.RoleCSMS, dispatcher.OpCall, m.Direction); err != nil {
		return zero, err
	}
	reqPayload, err := codec.Marshal(req)
	if err != nil {
		return zero, fmt.Errorf("marshal request: %w", err)
	}
	respPayload, err := doCall(ctx, c, m.Action, reqPayload)
	if err != nil {
		return zero, err
	}
	var resp Resp
	if err := codec.Unmarshal(respPayload, &resp); err != nil {
		return zero, fmt.Errorf("unmarshal response: %w", err)
	}
	return resp, nil
}

// CallAsync sends a typed message to a connected charge point without blocking and
// delivers the typed response (or error) to cb. Calls are queued FIFO and sent one
// outstanding at a time (the default; see WithConcurrentCalls). It returns synchronously only if the call could not be accepted
// (e.g. ocppj.ErrQueueFull, ocppj.ErrConnClosed). See dispatcher.DoCallAsync.
func CallAsync[Req, Resp any](ctx context.Context, c *Conn, m ocppj.Message[Req, Resp], req Req, cb func(Resp, error)) error {
	if err := dispatcher.CheckDirection(dispatcher.RoleCSMS, dispatcher.OpCall, m.Direction); err != nil {
		return err
	}
	reqPayload, err := codec.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	return doCallAsync(ctx, c, m.Action, reqPayload, func(payload []byte, callErr error) {
		var resp Resp
		if callErr != nil {
			cb(resp, callErr)
			return
		}
		if err := codec.Unmarshal(payload, &resp); err != nil {
			cb(resp, fmt.Errorf("unmarshal response: %w", err))
			return
		}
		cb(resp, nil)
	})
}

// CallRawAsync is the untyped form of CallAsync.
func CallRawAsync(ctx context.Context, c *Conn, action string, payload []byte, cb func([]byte, error)) error {
	return doCallAsync(ctx, c, action, payload, cb)
}
