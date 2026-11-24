package csms

import (
	"context"

	"github.com/shiv3/gocpp/core/codec"
	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/ocppj"
)

// OnSend registers a typed handler for an OCPP 2.1 SEND a CSMS receives.
func OnSend[Req any](s *Server, m ocppj.SendMessage[Req], h func(ctx context.Context, c *Conn, req Req) error) error {
	return onSend(s, "", m, h)
}

// OnSendVersion is OnSend for connections that negotiated protocol version v
// only; see OnVersion.
func OnSendVersion[Req any](s *Server, v ocppj.Version, m ocppj.SendMessage[Req], h func(ctx context.Context, c *Conn, req Req) error) error {
	return onSend(s, v, m, h)
}

func onSend[Req any](s *Server, v ocppj.Version, m ocppj.SendMessage[Req], h func(ctx context.Context, c *Conn, req Req) error) error {
	if err := dispatcher.CheckDirection(dispatcher.RoleCSMS, dispatcher.OpHandle, m.Direction); err != nil {
		return err
	}
	typed := func(ctx context.Context, c *Conn, payload []byte) error {
		var req Req
		if err := codec.Unmarshal(payload, &req); err != nil {
			return dispatcher.NewDecodeError(err)
		}
		return h(ctx, c, req)
	}
	wrapped := func(ctx context.Context, dc *dispatcher.Conn, payload []byte) error {
		return typed(ctx, &Conn{inner: dc}, payload)
	}
	if v == "" {
		s.handlers.setSend(m.Action, typed)
		s.reg.RegisterSend(m.Action, wrapped)
		return nil
	}
	s.handlers.setSendVersion(v, m.Action, typed)
	s.reg.RegisterSendVersion(v, m.Action, wrapped)
	return nil
}

// Send sends a typed OCPP 2.1 SEND from the CSMS to a connected charge point.
func Send[Req any](ctx context.Context, c *Conn, m ocppj.SendMessage[Req], req Req) error {
	if err := dispatcher.CheckDirection(dispatcher.RoleCSMS, dispatcher.OpCall, m.Direction); err != nil {
		return err
	}
	if c.inner == nil && c.gw == nil {
		return ErrRemoteConn
	}
	payload, err := codec.Marshal(req)
	if err != nil {
		return err
	}
	if c.inner == nil {
		if c.srv == nil || c.srv.backend == nil {
			return ErrRemoteConn
		}
		return c.srv.sendGateway(ctx, c, m.Action, payload)
	}
	return dispatcher.DoSend(ctx, c.inner, m.Action, payload)
}
