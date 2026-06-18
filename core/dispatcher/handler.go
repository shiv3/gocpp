package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
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

func (c *Conn) runHandler(frame ocppj.Frame) {
	defer c.sem.Release(1)
	if c.cfg.GlobalHandlerLimiter != nil {
		defer c.cfg.GlobalHandlerLimiter.Release(1)
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

	h, ok := c.reg.Lookup(frame.Action)
	if !ok {
		status = "not_implemented"
		c.sendCallError(frame.MsgID, ocppj.NewCallError(
			ocppj.ErrorCodeNotImplemented, "action "+frame.Action+" not implemented", nil))
		return
	}
	reqPayload := frame.Payload
	if c.cfg.SchemaMode == SchemaModeLenient {
		out, herr := c.lenientValidate(frame.Action, "request", frame.Payload)
		if herr != nil {
			status = "schema_invalid"
			c.sendCallError(frame.MsgID, ocppj.NewCallError(ocppj.ErrorCodeFormationViolation, herr.Error(), nil))
			return
		}
		reqPayload = out
	} else if err := c.schemaValidationError(frame.Action, "request", frame.Payload); err != nil {
		if c.cfg.SchemaMode == SchemaModeStrict {
			status = "schema_invalid"
			c.sendCallError(frame.MsgID, ocppj.NewCallError(ocppj.ErrorCodeFormationViolation, err.Error(), nil))
			return
		}
	}
	hctx, span := c.cfg.Tracer.Start(c.ctx, "ocpp.handler")
	span.SetAttributes(
		attribute.String("ocpp.action", frame.Action),
		attribute.String("ocpp.cp_id", c.id),
		attribute.String("ocpp.direction", "inbound"),
	)
	resp, err := h(hctx, c, reqPayload)
	span.End()
	if err != nil {
		status = "error"
		c.sendCallError(frame.MsgID, mapHandlerError(err))
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
	c.sendCallResult(frame.MsgID, respPayload)
}

// runSendHandler dispatches an inbound SEND (OCPP 2.1). A SEND is unconfirmed:
// the receiver MUST NOT reply with a CallResult or CallError, so every outcome
// (missing handler, schema-invalid, handler error) is logged and dropped.
func (c *Conn) runSendHandler(frame ocppj.Frame) {
	h, ok := c.reg.LookupSend(frame.Action)
	if !ok {
		c.cfg.Logger.DebugContext(c.ctx, "no SEND handler registered, dropping",
			"cp_id", c.id, "action", frame.Action)
		return
	}
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
	if err := h(c.ctx, c, reqPayload); err != nil {
		c.cfg.Logger.WarnContext(c.ctx, "SEND handler error",
			"cp_id", c.id, "action", frame.Action, "err", err)
	}
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
