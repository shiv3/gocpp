package cp

import (
	"context"
	"encoding/json"
	"fmt"

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
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, ocppj.WrapCallError(ocppj.ErrorCodeFormationViolation, err, nil)
		}
		resp, err := h(ctx, req)
		if err != nil {
			return nil, err
		}
		return json.Marshal(resp)
	})
	return nil
}

// Call sends a typed message from this charge point to the CSMS.
func Call[Req, Resp any](ctx context.Context, c *Client, m ocppj.Message[Req, Resp], req Req) (Resp, error) {
	var zero Resp
	if err := dispatcher.CheckDirection(dispatcher.RoleCP, dispatcher.OpCall, m.Direction); err != nil {
		return zero, err
	}
	conn := c.current()
	if conn == nil {
		return zero, ocppj.ErrNotConnected
	}
	reqPayload, err := json.Marshal(req)
	if err != nil {
		return zero, fmt.Errorf("marshal request: %w", err)
	}
	respPayload, err := dispatcher.DoCall(ctx, conn, m.Action, reqPayload)
	if err != nil {
		return zero, err
	}
	var resp Resp
	if err := json.Unmarshal(respPayload, &resp); err != nil {
		return zero, fmt.Errorf("unmarshal response: %w", err)
	}
	return resp, nil
}
