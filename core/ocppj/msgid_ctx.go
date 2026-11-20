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
