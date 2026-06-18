package csms

import (
	"context"

	"github.com/shiv3/gocpp/core/codec"
	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/ocppj"
)

// OnSend registers a typed handler for an OCPP 2.1 SEND a CSMS receives.
func OnSend[Req any](s *Server, m ocppj.SendMessage[Req], h func(ctx context.Context, c *Conn, req Req) error) error {
	if err := dispatcher.CheckDirection(dispatcher.RoleCSMS, dispatcher.OpHandle, m.Direction); err != nil {
		return err
	}
	s.reg.RegisterSend(m.Action, func(ctx context.Context, dc *dispatcher.Conn, payload []byte) error {
		var req Req
		if err := codec.Unmarshal(payload, &req); err != nil {
			return ocppj.WrapCallError(ocppj.ErrorCodeFormationViolation, err, nil)
		}
		return h(ctx, &Conn{inner: dc}, req)
	})
	return nil
}

// Send sends a typed OCPP 2.1 SEND from the CSMS to a connected charge point.
func Send[Req any](ctx context.Context, c *Conn, m ocppj.SendMessage[Req], req Req) error {
	if err := dispatcher.CheckDirection(dispatcher.RoleCSMS, dispatcher.OpCall, m.Direction); err != nil {
		return err
	}
	payload, err := codec.Marshal(req)
	if err != nil {
		return err
	}
	return dispatcher.DoSend(ctx, c.inner, m.Action, payload)
}
