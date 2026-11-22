package ocppj

import "context"

type msgIDKey struct{}
type msgIDCallbackKey struct{}

// ContextWithMessageID returns ctx carrying the OCPP message id of the inbound
// frame being handled. The dispatcher sets it for every handler invocation.
func ContextWithMessageID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, msgIDKey{}, id)
}

// MessageIDFromContext returns the OCPP message id of the inbound CALL/SEND a
// handler is serving. ok is false outside a handler context.
func MessageIDFromContext(ctx context.Context) (id string, ok bool) {
	id, ok = ctx.Value(msgIDKey{}).(string)
	return id, ok
}

// WithMessageIDCallback returns a context that makes an outbound Call/Send
// report the OCPP message id it is sent with. fn is invoked once, synchronously
// on the calling goroutine, after the id is generated and before the frame is
// queued, so the id is known even if the call later times out or fails. Pass
// the returned context to csms.Call, cp.Call, CallAsync, Send or their raw
// variants; no signature changes are needed. A panic in fn is recovered.
func WithMessageIDCallback(ctx context.Context, fn func(msgID string)) context.Context {
	return context.WithValue(ctx, msgIDCallbackKey{}, fn)
}

// NotifyMessageID invokes the callback installed by WithMessageIDCallback, if
// any. Used by the dispatcher.
func NotifyMessageID(ctx context.Context, id string) {
	fn, _ := ctx.Value(msgIDCallbackKey{}).(func(string))
	if fn == nil {
		return
	}
	defer func() { _ = recover() }()
	fn(id)
}

type outboundMsgIDKey struct{}

// ContextWithOutboundMessageID returns a context that makes the next outbound
// Call (or Send) sent with it use id as its OCPP message id instead of a
// generated one. The caller is responsible for the id being unique on the
// connection. It exists for embedders that own the id policy, such as a
// compatibility layer that must honour an application supplied id generator.
func ContextWithOutboundMessageID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, outboundMsgIDKey{}, id)
}

// OutboundMessageID returns the id set by ContextWithOutboundMessageID, or a
// freshly generated one. Used by the dispatcher.
func OutboundMessageID(ctx context.Context) string {
	if id, ok := ctx.Value(outboundMsgIDKey{}).(string); ok && id != "" {
		return id
	}
	return NewMsgID()
}
