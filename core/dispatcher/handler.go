package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"go.opentelemetry.io/otel/attribute"
)

type HandlerFunc func(ctx context.Context, c *Conn, payload []byte) ([]byte, error)

// SendHandlerFunc handles an inbound SEND frame (OCPP 2.1 message type 6).
// A SEND is unconfirmed: the handler must not return a reply; the dispatcher
// never sends one regardless.
type SendHandlerFunc func(ctx context.Context, c *Conn, payload []byte) error

type HandlerRegistry struct {
	mu    sync.RWMutex
	hs    map[string]HandlerFunc
	sends map[string]SendHandlerFunc
}

func NewHandlerRegistry() *HandlerRegistry {
	return &HandlerRegistry{
		hs:    make(map[string]HandlerFunc),
		sends: make(map[string]SendHandlerFunc),
	}
}

func (r *HandlerRegistry) Lookup(action string) (HandlerFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.hs[action]
	return h, ok
}

func (r *HandlerRegistry) Register(action string, h HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hs[action] = h
}

func (r *HandlerRegistry) RegisterSend(action string, h SendHandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sends[action] = h
}

func (r *HandlerRegistry) LookupSend(action string) (SendHandlerFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.sends[action]
	return h, ok
}

type connCtxKey struct{}

// ConnContext returns the connection lifecycle context of the connection that
// is serving the handler ctx: it is cancelled when the connection goes away,
// unlike ctx itself. ok is false outside a handler context.
func ConnContext(ctx context.Context) (context.Context, bool) {
	cc, ok := ctx.Value(connCtxKey{}).(context.Context)
	return cc, ok
}

// ConnFromContext returns the connection serving the handler ctx.
func ConnFromContext(ctx context.Context) (*Conn, bool) {
	c, ok := ctx.Value(connKey{}).(*Conn)
	return c, ok
}

type connKey struct{}

// handlerContext builds the context handed to inbound handlers. It keeps the
// connection context's values (cp id, tracing, ...) but not its cancellation: a
// dropped connection must not abort work such as a database write the handler is
// halfway through (the CALLRESULT is then dropped, the work is not lost).
// Handlers that want to stop on disconnect watch ConnContext(ctx).Done(). The
// per-handler deadline (HandlerTimeout) is what bounds a stuck handler.
func (c *Conn) handlerContext(msgID string) (context.Context, context.CancelFunc) {
	ctx := context.WithoutCancel(c.ctx)
	ctx = context.WithValue(ctx, connCtxKey{}, c.ctx)
	ctx = context.WithValue(ctx, connKey{}, c)
	ctx = ocppj.ContextWithMessageID(ctx, msgID)
	if c.cfg.HandlerTimeout > 0 {
		return context.WithTimeout(ctx, c.cfg.HandlerTimeout)
	}
	return ctx, func() {}
}

func (c *Conn) runHandler(frame ocppj.Frame) {
	if lim := c.cfg.GlobalHandlerLimiter; lim != nil {
		// Wait for a server-wide slot on this (handler) goroutine, never on the
		// reader: it parks at most MaxConcurrentHandlers goroutines per
		// connection and backs up through the inbound queue.
		if err := lim.Acquire(c.ctx, 1); err != nil {
			return // connection ended while waiting; the frame is dropped
		}
		defer lim.Release(1)
	}

	// SEND (type 6) is unconfirmed: dispatch and never reply.
	if frame.Type == ocppj.Send {
		c.runSendHandler(frame)
		return
	}

	start := time.Now()
	c.cfg.Metrics.CallStarted(frame.Action, "inbound")
	status := "ok"
	defer func() {
		c.cfg.Metrics.CallCompleted(frame.Action, "inbound", time.Since(start), status)
	}()
	// A panic in a user handler must not crash the whole peer/process: recover,
	// log, and reply with an InternalError CallError so the connection survives.
	defer func() {
		if r := recover(); r != nil {
			status = "panic"
			c.cfg.Logger.ErrorContext(c.ctx, "handler panic recovered",
				"cp_id", c.id, "action", frame.Action, "panic", r)
			c.sendCallError(frame.MsgID, ocppj.NewCallError(
				ocppj.ErrorCodeInternalError, "internal error", nil))
		}
	}()

	decoded, reject := c.decodeSignedPayload(frame)
	if reject != nil {
		status = "signature_invalid"
		c.sendCallError(frame.MsgID, reject)
		return
	}
	frame.Payload = decoded

	h, ok := c.reg.Lookup(frame.Action)
	if !ok {
		status = "not_implemented"
		c.replyBuiltinError(frame.MsgID, ErrorSourceUnknownAction, frame.Action, ocppj.ErrUnknownAction, ocppj.NewCallError(
			ocppj.ErrorCodeNotImplemented, "action "+frame.Action+" not implemented", nil))
		return
	}
	reqPayload := frame.Payload
	if c.cfg.SchemaMode == SchemaModeLenient {
		out, herr := c.lenientValidate(frame.Action, "request", frame.Payload)
		if herr != nil {
			status = "schema_invalid"
			c.replyBuiltinError(frame.MsgID, ErrorSourceSchema, frame.Action, herr, ocppj.NewCallError(SchemaErrorCode(herr), herr.Error(), nil))
			return
		}
		reqPayload = out
	} else if err := c.schemaValidationError(frame.Action, "request", frame.Payload); err != nil {
		if c.cfg.SchemaMode == SchemaModeStrict {
			status = "schema_invalid"
			c.replyBuiltinError(frame.MsgID, ErrorSourceSchema, frame.Action, err, ocppj.NewCallError(SchemaErrorCode(err), err.Error(), nil))
			return
		}
	}
	baseCtx, cancelHandler := c.handlerContext(frame.MsgID)
	defer cancelHandler()
	hctx, span := c.cfg.Tracer.Start(baseCtx, "ocpp.handler")
	span.SetAttributes(
		attribute.String("ocpp.action", frame.Action),
		attribute.String("ocpp.cp_id", c.id),
		attribute.String("ocpp.direction", "inbound"),
	)
	resp, err := h(hctx, c, reqPayload)
	span.End()
	if err != nil {
		status = "error"
		if hctx.Err() != nil && errors.Is(err, context.DeadlineExceeded) {
			status = "timeout"
			c.cfg.Logger.WarnContext(c.ctx, "handler exceeded its timeout",
				"cp_id", c.id, "action", frame.Action, "timeout", c.cfg.HandlerTimeout)
			c.sendCallError(frame.MsgID, ocppj.NewCallError(
				ocppj.ErrorCodeInternalError, "handler timed out", nil))
			return
		}
		ce := mapHandlerError(err)
		var de *decodeError
		if errors.As(err, &de) {
			c.replyBuiltinError(frame.MsgID, ErrorSourceDecode, frame.Action, de.err, ce)
			return
		}
		c.sendCallError(frame.MsgID, ce)
		return
	}
	respPayload := resp
	if c.cfg.SchemaMode == SchemaModeLenient {
		out, herr := c.lenientValidate(frame.Action, "response", resp)
		if herr != nil {
			status = "schema_invalid"
			// The local handler produced an invalid response, so no valid
			// CallResult can be sent. Return InternalError to the peer.
			c.sendCallError(frame.MsgID, ocppj.NewCallError(ocppj.ErrorCodeInternalError, herr.Error(), nil))
			return
		}
		respPayload = out
	} else if err := c.schemaValidationError(frame.Action, "response", resp); err != nil {
		if c.cfg.SchemaMode == SchemaModeStrict {
			status = "schema_invalid"
			// The local handler produced an invalid response, so no valid
			// CallResult can be sent. Return InternalError to the peer.
			c.sendCallError(frame.MsgID, ocppj.NewCallError(ocppj.ErrorCodeInternalError, err.Error(), nil))
			return
		}
	}
	respPayload, terr := c.transformOutbound(frame.Action, "response", respPayload)
	if terr != nil {
		status = "error"
		c.sendCallError(frame.MsgID, ocppj.NewCallError(ocppj.ErrorCodeInternalError, terr.Error(), nil))
		return
	}
	c.sendCallResult(frame.MsgID, respPayload)
}

// runSendHandler dispatches an inbound SEND (OCPP 2.1). A SEND is unconfirmed:
// the receiver MUST NOT reply with a CallResult or CallError, so every outcome
// (missing handler, schema-invalid, handler error) is logged and dropped.
func (c *Conn) runSendHandler(frame ocppj.Frame) {
	// A panic in a user SEND handler must not crash the peer/process. Unlike a
	// Call, a SEND has no reply, so recover, log, and drop.
	defer func() {
		if r := recover(); r != nil {
			c.cfg.Logger.ErrorContext(c.ctx, "SEND handler panic recovered",
				"cp_id", c.id, "action", frame.Action, "panic", r)
		}
	}()

	h, ok := c.reg.LookupSend(frame.Action)
	if !ok {
		c.cfg.Logger.DebugContext(c.ctx, "no SEND handler registered, dropping",
			"cp_id", c.id, "action", frame.Action)
		return
	}
	decoded, reject := c.decodeSignedPayload(frame)
	if reject != nil {
		c.cfg.Logger.WarnContext(c.ctx, "dropping unverifiable signed SEND",
			"cp_id", c.id, "action", frame.Action, "code", reject.Code)
		return
	}
	frame.Payload = decoded
	reqPayload := frame.Payload
	if c.cfg.SchemaMode == SchemaModeLenient {
		out, herr := c.lenientValidate(frame.Action, "request", frame.Payload)
		if herr != nil {
			c.cfg.Logger.WarnContext(c.ctx, "dropping schema-invalid SEND",
				"cp_id", c.id, "action", frame.Action, "err", herr)
			return
		}
		reqPayload = out
	} else if err := c.schemaValidationError(frame.Action, "request", frame.Payload); err != nil {
		if c.cfg.SchemaMode == SchemaModeStrict {
			c.cfg.Logger.WarnContext(c.ctx, "dropping schema-invalid SEND",
				"cp_id", c.id, "action", frame.Action, "err", err)
			return
		}
	}
	hctx, cancelHandler := c.handlerContext(frame.MsgID)
	defer cancelHandler()
	if err := h(hctx, c, reqPayload); err != nil {
		c.cfg.Logger.WarnContext(c.ctx, "SEND handler error",
			"cp_id", c.id, "action", frame.Action, "err", err)
	}
}

// SchemaErrorCode picks the CALLERROR code for a schema rejection of an inbound
// request from the keyword of its first hard violation (OCPP 1.6J §4.2.3 and
// OCPP 2.0.1 Part 4 §4.2.3 error code tables):
//
//   - required, min/maxItems, min/maxProperties: OccurrenceConstraintViolation
//     (WireCode spells it OccurenceConstraintViolation on 1.6)
//   - enum, const, min/maxLength, minimum/maximum (and exclusive variants),
//     multipleOf, pattern, format: PropertyConstraintViolation
//   - type below the root: TypeConstraintViolation
//   - anything else (payload not an object, additionalProperties, composition
//     keywords, undecodable JSON, a custom validator's plain error): the
//     version's FormationViolation / FormatViolation.
func SchemaErrorCode(err error) ocppj.ErrorCode {
	var ve *schema.ViolationError
	if !errors.As(err, &ve) {
		return ocppj.ErrorCodeFormationViolation
	}
	switch ve.Keyword {
	case "required", "minItems", "maxItems", "minProperties", "maxProperties":
		return ocppj.ErrorCodeOccurrenceConstraintViolation
	case "enum", "const", "minLength", "maxLength", "minimum", "maximum",
		"exclusiveMinimum", "exclusiveMaximum", "multipleOf", "pattern", "format":
		return ocppj.ErrorCodePropertyConstraintViolation
	case "type":
		if !ve.AtRoot {
			return ocppj.ErrorCodeTypeConstraintViolation
		}
	}
	return ocppj.ErrorCodeFormationViolation
}

// decodeError marks a CallError that came from decoding a request payload into
// the handler's typed struct, so ErrorCodeMapper can tell it from an error the
// application returned.
type decodeError struct{ err error }

func (e *decodeError) Error() string { return e.err.Error() }
func (e *decodeError) Unwrap() error { return e.err }

// NewDecodeError is the CALLERROR a typed handler wrapper returns when the
// request payload cannot be decoded (code per DecodeErrorCode).
func NewDecodeError(err error) *ocppj.CallError {
	return ocppj.WrapCallError(DecodeErrorCode(err), &decodeError{err: err}, nil)
}

// DecodeErrorCode picks the CALLERROR code for a failure to decode a request
// payload into a typed struct: a JSON value of the wrong type inside the
// payload is a TypeConstraintViolation, anything else a FormationViolation.
func DecodeErrorCode(err error) ocppj.ErrorCode {
	var ute *json.UnmarshalTypeError
	// An empty Field means the payload itself is the wrong type (not an object),
	// a structural failure rather than a property's type.
	if errors.As(err, &ute) && ute.Field != "" {
		return ocppj.ErrorCodeTypeConstraintViolation
	}
	return ocppj.ErrorCodeFormationViolation
}

// mapHandlerError converts a handler error into a CallError per spec §6.5.
func mapHandlerError(err error) *ocppj.CallError {
	var ce *ocppj.CallError
	if errors.As(err, &ce) {
		return ce
	}
	switch {
	case errors.Is(err, ocppj.ErrUnknownAction):
		return ocppj.NewCallError(ocppj.ErrorCodeNotImplemented, err.Error(), nil)
	case errors.Is(err, ocppj.ErrInvalidDirection):
		return ocppj.NewCallError(ocppj.ErrorCodeMessageTypeNotSupported, err.Error(), nil)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return ocppj.NewCallError(ocppj.ErrorCodeInternalError, "request cancelled", nil)
	default:
		return ocppj.WrapCallError(ocppj.ErrorCodeInternalError, err, nil)
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func (c *Conn) recordSchemaValidationFailure(action, kind string) {
	if m, ok := c.cfg.Metrics.(schemaValidationMetricsHook); ok {
		m.SchemaValidationFailure(string(c.version), action, kind)
	}
}

// lenientValidate applies lenient validation. A non-nil hardErr means the
// caller must reject the message. Otherwise out is the payload to continue with,
// possibly enum-normalized.
func (c *Conn) lenientValidate(action, kind string, payload []byte) (out []byte, hardErr error) {
	if c.cfg.SchemaValidateLenient == nil {
		return payload, nil
	}
	out, soft, err := c.cfg.SchemaValidateLenient(c.version, action, kind, payload)
	if err != nil {
		c.recordSchemaValidationFailure(action, kind)
		return nil, err
	}
	for _, kw := range soft {
		if m, ok := c.cfg.Metrics.(schemaSoftViolationMetricsHook); ok {
			m.SchemaSoftViolation(string(c.version), action, kind, kw)
		}
	}
	if len(soft) > 0 {
		c.cfg.Logger.WarnContext(c.ctx, "schema soft violations passed (lenient)",
			"version", string(c.version), "action", action, "kind", kind, "keywords", soft)
	}
	return out, nil
}

func (c *Conn) schemaValidationError(action, kind string, payload []byte) error {
	if c.cfg.SchemaValidate == nil || c.cfg.SchemaMode == SchemaModeOff {
		return nil
	}
	err := c.cfg.SchemaValidate(c.version, action, kind, payload)
	if err == nil {
		return nil
	}
	c.recordSchemaValidationFailure(action, kind)
	if c.cfg.SchemaMode == SchemaModeTolerant {
		c.cfg.Logger.WarnContext(c.ctx, "schema validation failed",
			"version", string(c.version), "action", action, "kind", kind, "err", err)
	}
	return err
}
