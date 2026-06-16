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
	s.reg.Register(m.Action, func(ctx context.Context, dc *dispatcher.Conn, payload []byte) ([]byte, error) {
		var req Req
		if err := codec.Unmarshal(payload, &req); err != nil {
			return nil, ocppj.WrapCallError(ocppj.ErrorCodeFormationViolation, err, nil)
		}
		resp, err := h(ctx, &Conn{inner: dc}, req)
		if err != nil {
			return nil, err
		}
		return codec.Marshal(resp)
	})
	return nil
}

// CallRaw sends an action with a raw JSON payload to a connected charge point and
// returns the raw response. Used by tools/tests that operate on untyped messages
// (the typed Call is preferred for application code).
func CallRaw(ctx context.Context, c *Conn, action string, payload []byte) ([]byte, error) {
	return dispatcher.DoCall(ctx, c.inner, action, payload)
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
	respPayload, err := dispatcher.DoCall(ctx, c.inner, m.Action, reqPayload)
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
// delivers the typed response (or error) to cb. With WithSerializedCalls the calls
// are queued FIFO and sent one outstanding at a time; otherwise they run
// concurrently. It returns synchronously only if the call could not be accepted
// (e.g. ocppj.ErrQueueFull, ocppj.ErrConnClosed). See dispatcher.DoCallAsync.
func CallAsync[Req, Resp any](ctx context.Context, c *Conn, m ocppj.Message[Req, Resp], req Req, cb func(Resp, error)) error {
	if err := dispatcher.CheckDirection(dispatcher.RoleCSMS, dispatcher.OpCall, m.Direction); err != nil {
		return err
	}
	reqPayload, err := codec.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	return dispatcher.DoCallAsync(ctx, c.inner, m.Action, reqPayload, func(payload []byte, callErr error) {
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
	return dispatcher.DoCallAsync(ctx, c.inner, action, payload, cb)
}
