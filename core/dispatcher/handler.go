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

type HandlerRegistry struct {
	mu sync.RWMutex
	hs map[string]HandlerFunc
}

func NewHandlerRegistry() *HandlerRegistry {
	return &HandlerRegistry{hs: make(map[string]HandlerFunc)}
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

func (c *Conn) runHandler(frame ocppj.Frame) {
	defer c.sem.Release(1)

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
	if c.cfg.SchemaValidate != nil {
		if err := c.cfg.SchemaValidate(c.version, frame.Action, "request", frame.Payload); err != nil {
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
	resp, err := h(hctx, c, frame.Payload)
	span.End()
	if err != nil {
		status = "error"
		c.sendCallError(frame.MsgID, mapHandlerError(err))
		return
	}
	c.sendCallResult(frame.MsgID, resp)
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
