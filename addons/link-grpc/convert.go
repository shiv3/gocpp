package linkgrpc

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/shiv3/gocpp/addons/link-grpc/linkpb"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/gateway"
)

func refToPB(r gateway.SessionRef) *linkpb.SessionRef {
	return &linkpb.SessionRef{Cpid: r.CPID, GatewayId: r.GatewayID, Boot: r.Boot, Epoch: r.Epoch}
}

func refFromPB(r *linkpb.SessionRef) gateway.SessionRef {
	if r == nil {
		return gateway.SessionRef{}
	}
	return gateway.SessionRef{CPID: r.Cpid, GatewayID: r.GatewayId, Boot: r.Boot, Epoch: r.Epoch}
}

func routeToPB(r *gateway.Route) *linkpb.Route {
	if r == nil {
		return nil
	}
	return &linkpb.Route{Pool: r.Pool, Fallback: r.Fallback, Rule: r.Rule, Rev: r.Rev}
}

func routeFromPB(r *linkpb.Route) *gateway.Route {
	if r == nil {
		return nil
	}
	return &gateway.Route{Pool: r.Pool, Fallback: r.Fallback, Rule: r.Rule, Rev: r.Rev}
}

func errToPB(e *gateway.WireError) *linkpb.WireError {
	if e == nil {
		return nil
	}
	var details []byte
	if e.Details != nil {
		details, _ = json.Marshal(e.Details)
	}
	return &linkpb.WireError{
		Kind: string(e.Kind), Code: e.Code, Description: e.Description, DetailsJson: details,
		ResultError: e.ResultError, Sentinel: e.Sentinel, Message: e.Message,
	}
}

func errFromPB(e *linkpb.WireError) *gateway.WireError {
	if e == nil {
		return nil
	}
	w := &gateway.WireError{
		Kind: gateway.ErrKind(e.Kind), Code: e.Code, Description: e.Description,
		ResultError: e.ResultError, Sentinel: e.Sentinel, Message: e.Message,
	}
	if len(e.DetailsJson) > 0 {
		_ = json.Unmarshal(e.DetailsJson, &w.Details)
	}
	return w
}

func headerToPB(h http.Header) map[string]*linkpb.HeaderValues {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]*linkpb.HeaderValues, len(h))
	for k, v := range h {
		out[k] = &linkpb.HeaderValues{V: v}
	}
	return out
}

func headerFromPB(m map[string]*linkpb.HeaderValues) http.Header {
	if m == nil {
		return nil
	}
	out := make(http.Header, len(m))
	for k, v := range m {
		out[k] = v.GetV()
	}
	return out
}

func infoToPB(i *gateway.SessionInfo) *linkpb.SessionInfo {
	if i == nil {
		return nil
	}
	return &linkpb.SessionInfo{
		RemoteAddr: i.RemoteAddr, Subprotocol: i.Subprotocol, Header: headerToPB(i.RequestHeader),
		PeerCerts: i.PeerCerts, Identity: i.Identity, Meta: i.Meta,
	}
}

func infoFromPB(i *linkpb.SessionInfo) *gateway.SessionInfo {
	if i == nil {
		return nil
	}
	return &gateway.SessionInfo{
		RemoteAddr: i.RemoteAddr, Subprotocol: i.Subprotocol, RequestHeader: headerFromPB(i.Header),
		PeerCerts: i.PeerCerts, Identity: i.Identity, Meta: i.Meta,
	}
}

func nanos(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

func fromNanos(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}

func envToPB(e gateway.Envelope) *linkpb.Envelope {
	pb := &linkpb.Envelope{
		Proto: e.Proto, Session: refToPB(e.Session), Version: string(e.Version), Kind: uint32(e.Kind),
		DeliveryId: e.DeliveryID, Attempt: e.Attempt, MsgId: e.MsgID, Action: e.Action, Payload: e.Payload,
		ReceivedAtUnixNano: nanos(e.ReceivedAt), DeadlineUnixNano: nanos(e.Deadline), Trace: e.Trace,
		Attrs: e.Attrs, Open: infoToPB(e.Open), Route: routeToPB(e.Route), CallId: e.CallID, Error: errToPB(e.Error),
	}
	if e.Close != nil {
		pb.Close = &linkpb.CloseInfo{Cause: e.Close.Cause, Message: e.Close.Message}
	}
	return pb
}

func envFromPB(pb *linkpb.Envelope) gateway.Envelope {
	e := gateway.Envelope{
		Proto: pb.Proto, Session: refFromPB(pb.Session), Version: ocppj.Version(pb.Version), Kind: gateway.Kind(pb.Kind),
		DeliveryID: pb.DeliveryId, Attempt: pb.Attempt, MsgID: pb.MsgId, Action: pb.Action, Payload: pb.Payload,
		ReceivedAt: fromNanos(pb.ReceivedAtUnixNano), Deadline: fromNanos(pb.DeadlineUnixNano), Trace: pb.Trace,
		Attrs: pb.Attrs, Open: infoFromPB(pb.Open), Route: routeFromPB(pb.Route), CallID: pb.CallId, Error: errFromPB(pb.Error),
	}
	if pb.Close != nil {
		e.Close = &gateway.CloseInfo{Cause: pb.Close.Cause, Message: pb.Close.Message}
	}
	return e
}

func replyToPB(r gateway.Reply) *linkpb.Reply {
	return &linkpb.Reply{Payload: r.Payload, Error: errToPB(r.Error), Nack: r.Nack, NoReply: r.NoReply}
}

func replyFromPB(r *linkpb.Reply) gateway.Reply {
	return gateway.Reply{Payload: r.Payload, Error: errFromPB(r.Error), Nack: r.Nack, NoReply: r.NoReply}
}

func connectReqToPB(r gateway.ConnectRequest) *linkpb.ConnectRequest {
	return &linkpb.ConnectRequest{
		Cpid: r.CPID, Path: r.Path, RemoteAddr: r.RemoteAddr, Header: headerToPB(r.Header), PeerCerts: r.PeerCerts,
		Subprotocol: r.Subprotocol, Gateway: refToPB(r.Gateway), Route: routeToPB(r.Route),
	}
}

func connectReqFromPB(r *linkpb.ConnectRequest) gateway.ConnectRequest {
	return gateway.ConnectRequest{
		CPID: r.Cpid, Path: r.Path, RemoteAddr: r.RemoteAddr, Header: headerFromPB(r.Header), PeerCerts: r.PeerCerts,
		Subprotocol: r.Subprotocol, Gateway: refFromPB(r.Gateway), Route: routeFromPB(r.Route),
	}
}

func connectRepToPB(r gateway.ConnectReply) *linkpb.ConnectReply {
	pb := &linkpb.ConnectReply{Cpid: r.CPID, Identity: r.Identity, Meta: r.Meta, Attrs: r.Attrs}
	if r.Reject != nil {
		pb.RejectStatus = int32(r.Reject.Status)
		if pb.RejectStatus == 0 {
			pb.RejectStatus = http.StatusUnauthorized
		}
		pb.RejectMessage = r.Reject.Message
	}
	if t := r.Timeouts; t != nil {
		pb.Timeouts = &linkpb.Timeouts{
			CallTimeoutNs: int64(t.CallTimeout), WriteTimeoutNs: int64(t.WriteTimeout),
			PingIntervalNs: int64(t.PingInterval), PongWaitNs: int64(t.PongWait), ReadTimeoutNs: int64(t.ReadTimeout),
		}
	}
	return pb
}

func connectRepFromPB(pb *linkpb.ConnectReply) gateway.ConnectReply {
	r := gateway.ConnectReply{CPID: pb.Cpid, Identity: pb.Identity, Meta: pb.Meta, Attrs: pb.Attrs}
	if pb.RejectStatus != 0 {
		r.Reject = &gateway.Rejection{Status: int(pb.RejectStatus), Message: pb.RejectMessage}
	}
	if t := pb.Timeouts; t != nil {
		r.Timeouts = &gateway.Timeouts{
			CallTimeout: time.Duration(t.CallTimeoutNs), WriteTimeout: time.Duration(t.WriteTimeoutNs),
			PingInterval: time.Duration(t.PingIntervalNs), PongWait: time.Duration(t.PongWaitNs),
			ReadTimeout: time.Duration(t.ReadTimeoutNs),
		}
	}
	return r
}

func callReqToPB(r gateway.CallRequest) *linkpb.CallRequest {
	return &linkpb.CallRequest{
		Target: refToPB(r.Target), CallId: r.CallID, Action: r.Action, Payload: r.Payload,
		DeadlineUnixNano: nanos(r.Deadline), Trace: r.Trace,
	}
}

func callReqFromPB(r *linkpb.CallRequest) gateway.CallRequest {
	return gateway.CallRequest{
		Target: refFromPB(r.Target), CallID: r.CallId, Action: r.Action, Payload: r.Payload,
		Deadline: fromNanos(r.DeadlineUnixNano), Trace: r.Trace,
	}
}

func callRepToPB(r gateway.CallReply) *linkpb.CallReply {
	return &linkpb.CallReply{Payload: r.Payload, Error: errToPB(r.Error), Sent: uint32(r.Sent), Session: refToPB(r.Session)}
}

func callRepFromPB(r *linkpb.CallReply) gateway.CallReply {
	return gateway.CallReply{Payload: r.Payload, Error: errFromPB(r.Error), Sent: gateway.Sent(r.Sent), Session: refFromPB(r.Session)}
}

func infoBackendToPB(b gateway.BackendInfo) *linkpb.BackendInfo {
	return &linkpb.BackendInfo{
		Id: b.ID, Pool: b.Pool, Version: b.Version, Role: uint32(b.Role),
		Proto: &linkpb.ProtoRange{Min: b.Proto.Min, Max: b.Proto.Max}, Caps: b.Caps, Actions: b.Actions, Labels: b.Labels,
	}
}

func infoBackendFromPB(b *linkpb.BackendInfo) gateway.BackendInfo {
	i := gateway.BackendInfo{
		ID: b.Id, Pool: b.Pool, Version: b.Version, Role: gateway.Role(b.Role), Caps: b.Caps, Actions: b.Actions, Labels: b.Labels,
	}
	if b.Proto != nil {
		i.Proto = gateway.ProtoRange{Min: b.Proto.Min, Max: b.Proto.Max}
	}
	return i
}
