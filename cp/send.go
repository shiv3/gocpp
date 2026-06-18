package cp

import (
	"context"

	"github.com/shiv3/gocpp/core/codec"
	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/ocppj"
)

// OnSend registers a typed handler for an OCPP 2.1 SEND this charge point
// receives. SEND messages are unconfirmed: the handler returns only an error
// (logged, never sent back).
func OnSend[Req any](c *Client, m ocppj.SendMessage[Req], h func(ctx context.Context, req Req) error) error {
	if err := dispatcher.CheckDirection(dispatcher.RoleCP, dispatcher.OpHandle, m.Direction); err != nil {
		return err
	}
	c.reg.RegisterSend(m.Action, func(ctx context.Context, _ *dispatcher.Conn, payload []byte) error {
		var req Req
		if err := codec.Unmarshal(payload, &req); err != nil {
			return ocppj.WrapCallError(ocppj.ErrorCodeFormationViolation, err, nil)
		}
		return h(ctx, req)
	})
	return nil
}

// Send sends a typed OCPP 2.1 SEND from this charge point to the CSMS. It returns
// once the frame is written; no response is awaited.
func Send[Req any](ctx context.Context, c *Client, m ocppj.SendMessage[Req], req Req) error {
	if err := dispatcher.CheckDirection(dispatcher.RoleCP, dispatcher.OpCall, m.Direction); err != nil {
		return err
	}
	conn := c.current()
	if conn == nil {
		return ocppj.ErrNotConnected
	}
	payload, err := codec.Marshal(req)
	if err != nil {
		return err
	}
	return dispatcher.DoSend(ctx, conn, m.Action, payload)
}
