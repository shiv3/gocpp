package csms

import (
	"context"
	"encoding/json"
	"fmt"

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
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, ocppj.WrapCallError(ocppj.ErrorCodeFormationViolation, err, nil)
		}
		resp, err := h(ctx, &Conn{inner: dc}, req)
		if err != nil {
			return nil, err
		}
		return json.Marshal(resp)
	})
	return nil
}

// Call sends a typed message to a connected charge point and returns the response.
func Call[Req, Resp any](ctx context.Context, c *Conn, m ocppj.Message[Req, Resp], req Req) (Resp, error) {
	var zero Resp
	if err := dispatcher.CheckDirection(dispatcher.RoleCSMS, dispatcher.OpCall, m.Direction); err != nil {
		return zero, err
	}
	reqPayload, err := json.Marshal(req)
	if err != nil {
		return zero, fmt.Errorf("marshal request: %w", err)
	}
	respPayload, err := dispatcher.DoCall(ctx, c.inner, m.Action, reqPayload)
	if err != nil {
		return zero, err
	}
	var resp Resp
	if err := json.Unmarshal(respPayload, &resp); err != nil {
		return zero, fmt.Errorf("unmarshal response: %w", err)
	}
	return resp, nil
}
