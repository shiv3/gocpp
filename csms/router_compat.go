package csms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/gateway"
)

// Compatibility between the legacy storage.MessageRouter and gateway.Downstream.
//
// Routers carry bytes between clustered CSMS instances (WithInstanceID); the
// bytes are a small JSON envelope with the call outcome (see handleRemote).
// These adapters speak that envelope, so an existing router addon and a link
// based Downstream can be mixed during a migration.

// RouterDownstream presents a message router as a gateway.Downstream, so a
// service written against Downstream (an admin API, a workflow worker) can call
// chargers held by clustered instances through the router addons it already
// runs (router-redis, router-nats, router-temporal) and later switch to a gateway
// link without changing its code. It is a call-only view: Send and Control
// return gateway.ErrUnsupported (routers have no equivalent), CallRequest.Target
// is used for its CPID only (routers know no session epoch, GatewayID or Boot),
// and a reply's Sent is NotSent when no instance holds the charger and
// SentUnknown otherwise (the router cannot tell whether the frame was written).
// The call's Deadline and ctx bound the router call.
func RouterDownstream(r storage.MessageRouter) gateway.Downstream { return routerDownstream{r} } //nolint:staticcheck // legacy interface, kept for compatibility (deprecated in favour of gateway.Downstream)

type routerDownstream struct{ r storage.MessageRouter } //nolint:staticcheck // legacy interface, kept for compatibility (deprecated in favour of gateway.Downstream)

func (d routerDownstream) Call(ctx context.Context, req gateway.CallRequest) (gateway.CallReply, error) {
	if !req.Deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, req.Deadline)
		defer cancel()
	}
	raw, err := d.r.CallRemote(ctx, req.Target.CPID, req.Action, req.Payload)
	if err != nil {
		if errors.Is(err, storage.ErrNotLocal) || errors.Is(err, storage.ErrRouterNotImplemented) {
			return gateway.CallReply{Error: gateway.EncodeError(ocppj.ErrNotConnected), Sent: gateway.NotSent}, nil
		}
		return gateway.CallReply{}, gateway.WithSent(fmt.Errorf("csms: router call %s to %q: %w", req.Action, req.Target.CPID, err), gateway.SentUnknown)
	}
	var resp remoteResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return gateway.CallReply{}, gateway.WithSent(fmt.Errorf("csms: decode router response for %q: %w", req.Target.CPID, err), gateway.SentUnknown)
	}
	if resp.Error != nil {
		we := gateway.EncodeError(decodeRemoteError(resp.Error))
		sent := gateway.SentUnknown
		if we.Kind == gateway.ErrKindSentinel && we.Sentinel == "not_connected" {
			sent = gateway.NotSent
		}
		return gateway.CallReply{Error: we, Sent: sent}, nil
	}
	return gateway.CallReply{Payload: []byte(resp.Payload), Sent: gateway.Answered}, nil
}

func (routerDownstream) Send(context.Context, gateway.CallRequest) error {
	return gateway.ErrUnsupported
}

func (routerDownstream) Control(context.Context, gateway.ControlRequest) error {
	return gateway.ErrUnsupported
}

// DownstreamRouter presents a gateway.Downstream as the message router of a
// clustered server (WithMessageRouter), so CSMS-initiated calls to a charger
// held elsewhere travel as Downstream.Call requests (the link routes them to the
// gateway that holds the session) while the rest of the clustered server keeps
// working as before. CallRemote maps to Call with a Target that names the charger
// only (epoch 0: whatever session is current); CallLocal reports ErrNotLocal; and
// ServeRemote only waits for ctx, because a Downstream is served by its link, not
// by the router. New deployments should use WithGatewayLink / NewGatewayServer
// instead of WithInstanceID plus a router.
func DownstreamRouter(d gateway.Downstream) storage.MessageRouter { return downstreamRouter{d} } //nolint:staticcheck // legacy interface, kept for compatibility (deprecated in favour of gateway.Downstream)

type downstreamRouter struct{ d gateway.Downstream }

func (r downstreamRouter) CallLocal(context.Context, string, string, []byte) ([]byte, error) {
	return nil, storage.ErrNotLocal
}

func (r downstreamRouter) CallRemote(ctx context.Context, cpID, action string, req []byte) ([]byte, error) {
	creq := gateway.CallRequest{Target: gateway.SessionRef{CPID: cpID}, Action: action, Payload: req}
	if dl, ok := ctx.Deadline(); ok {
		creq.Deadline = dl
	}
	reply, err := r.d.Call(ctx, creq)
	if err != nil {
		if errors.Is(err, ocppj.ErrNotConnected) {
			return nil, storage.ErrNotLocal
		}
		return nil, err
	}
	var resp remoteResponse
	if reply.Error != nil {
		resp.Error = &remoteError{
			Kind: string(reply.Error.Kind), Code: reply.Error.Code, Description: reply.Error.Description,
			Details: reply.Error.Details, ResultError: reply.Error.ResultError,
			Sentinel: reply.Error.Sentinel, Message: reply.Error.Message,
		}
		if resp.Error.Kind == string(gateway.ErrKindSentinel) {
			if _, known := remoteSentinels[resp.Error.Sentinel]; !known {
				// A sentinel only the link knows: keep the text, as an ordinary error.
				resp.Error.Kind, resp.Error.Message = string(gateway.ErrKindOther), reply.Error.Err().Error()
			}
		}
	} else {
		resp.Payload = reply.Payload
	}
	return json.Marshal(resp)
}

func (r downstreamRouter) ServeRemote(ctx context.Context, _ storage.RemoteHandler) error {
	<-ctx.Done()
	return ctx.Err()
}
