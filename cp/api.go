package cp

import (
	"context"
	"fmt"

	"github.com/shiv3/gocpp/core/codec"
	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/ocppj"
)

// On registers a typed handler for a message the CSMS sends to this charge point.
func On[Req, Resp any](c *Client, m ocppj.Message[Req, Resp], h func(ctx context.Context, req Req) (Resp, error)) error {
	if err := dispatcher.CheckDirection(dispatcher.RoleCP, dispatcher.OpHandle, m.Direction); err != nil {
		return err
	}
	c.reg.Register(m.Action, func(ctx context.Context, dc *dispatcher.Conn, payload []byte) ([]byte, error) {
		var req Req
		if err := codec.Unmarshal(payload, &req); err != nil {
			return nil, ocppj.WrapCallError(ocppj.ErrorCodeFormationViolation, err, nil)
		}
		resp, err := h(ctx, req)
		if err != nil {
			return nil, err
		}
		return codec.Marshal(resp)
	})
	return nil
}

// CallRaw sends an action with a raw JSON payload and returns the raw response.
// Used by tools like the simulator that operate on untyped messages.
func CallRaw(ctx context.Context, c *Client, action string, payload []byte) ([]byte, error) {
	if c.queue != nil {
		return c.queue.call(ctx, action, payload)
	}
	conn := c.current()
	if conn == nil {
		return nil, ocppj.ErrNotConnected
	}
	return dispatcher.DoCall(ctx, conn, action, payload)
}

// Call sends a typed message from this charge point to the CSMS.
func Call[Req, Resp any](ctx context.Context, c *Client, m ocppj.Message[Req, Resp], req Req) (Resp, error) {
	var zero Resp
	if err := dispatcher.CheckDirection(dispatcher.RoleCP, dispatcher.OpCall, m.Direction); err != nil {
		return zero, err
	}
	reqPayload, err := codec.Marshal(req)
	if err != nil {
		return zero, fmt.Errorf("marshal request: %w", err)
	}
	respPayload, err := CallRaw(ctx, c, m.Action, reqPayload)
	if err != nil {
		return zero, err
	}
	var resp Resp
	if err := codec.Unmarshal(respPayload, &resp); err != nil {
		return zero, fmt.Errorf("unmarshal response: %w", err)
	}
	return resp, nil
}

// CallAsync sends a typed message to the CSMS without blocking and delivers the
// typed response (or error) to cb. With WithSerializedCalls the calls are queued
// FIFO and sent one outstanding at a time; otherwise they run concurrently. It
// returns synchronously only if the call could not be accepted (e.g. not
// connected, ocppj.ErrQueueFull). Unlike Call, this does not use the offline
// queue — it requires a live connection. See dispatcher.DoCallAsync.
func CallAsync[Req, Resp any](ctx context.Context, c *Client, m ocppj.Message[Req, Resp], req Req, cb func(Resp, error)) error {
	if err := dispatcher.CheckDirection(dispatcher.RoleCP, dispatcher.OpCall, m.Direction); err != nil {
		return err
	}
	conn := c.current()
	if conn == nil {
		return ocppj.ErrNotConnected
	}
	reqPayload, err := codec.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	return dispatcher.DoCallAsync(ctx, conn, m.Action, reqPayload, func(payload []byte, callErr error) {
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
func CallRawAsync(ctx context.Context, c *Client, action string, payload []byte, cb func([]byte, error)) error {
	conn := c.current()
	if conn == nil {
		return ocppj.ErrNotConnected
	}
	return dispatcher.DoCallAsync(ctx, conn, action, payload, cb)
}
