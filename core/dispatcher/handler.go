package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
)

type HandlerFunc func(ctx context.Context, c *Conn, payload []byte) ([]byte, error)

// SendHandlerFunc handles an inbound SEND frame (OCPP 2.1 message type 6).
// A SEND is unconfirmed: the handler must not return a reply; the dispatcher
// never sends one regardless.
type SendHandlerFunc func(ctx context.Context, c *Conn, payload []byte) error

type HandlerRegistry struct {
	mu    sync.RWMutex
	hs    map[string]HandlerFunc
	fb    HandlerFunc
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

// SetFallback sets the handler Resolve returns for an action with no
// registered handler (nil clears it). A gateway uses it as a catch-all that
// forwards every action upstream; Lookup is unaffected.
func (r *HandlerRegistry) SetFallback(h HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fb = h
}

// Resolve is Lookup that falls back to the handler set by SetFallback when the
// action has none. Inbound dispatch uses it.
func (r *HandlerRegistry) Resolve(action string) (HandlerFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if h, ok := r.hs[action]; ok {
		return h, true
	}
	return r.fb, r.fb != nil
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

// connKey is the context key under which handler contexts carry their *Conn
// (the connection context is c.ctx).
type connKey struct{}

// ConnContext returns the connection lifecycle context of the connection that
// is serving the handler ctx: it is cancelled when the connection goes away,
// unlike ctx itself. ok is false outside a handler context.
func ConnContext(ctx context.Context) (context.Context, bool) {
	c, ok := ctx.Value(connKey{}).(*Conn)
	if !ok {
		return nil, false
	}
	return c.ctx, true
}

// ConnFromContext returns the connection serving the handler ctx.
func ConnFromContext(ctx context.Context) (*Conn, bool) {
	c, ok := ctx.Value(connKey{}).(*Conn)
	return c, ok
}

// handlerContext builds the context handed to inbound handlers. It keeps the
// connection context's values (cp id, tracing, ...) but not its cancellation: a
// dropped connection must not abort work such as a database write the handler is
// halfway through (the CALLRESULT is then dropped, the work is not lost).
// Handlers that want to stop on disconnect watch ConnContext(ctx).Done(). The
// per-handler deadline (HandlerTimeout) is what bounds a stuck handler.
//
// The detached base context (c.handlerBase) is built once per connection; each
// message adds only its message id and, with HandlerTimeout, the deadline.
func (c *Conn) handlerContext(msgID string) (context.Context, context.CancelFunc) {
	ctx := ocppj.ContextWithMessageID(c.handlerBase, msgID)
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

	// Every inbound message runs on a fresh goroutine whose 2 KiB stack is too
	// small for the handler chain, so it has to grow. Growing at entry, while
	// only a few frames exist, is cheaper than growing later from inside the
	// pipeline (about 7% of a Heartbeat round trip in BenchmarkInboundHeartbeat),
	// so reserve frame space here, which makes the runtime grow once, now.
	var reserve [stackReserve]byte
	reserveStack(&reserve)
	start := time.Now()
	c.cfg.Metrics.CallStarted(frame.Action, "inbound")
	status := "ok"
	defer func() {
		c.cfg.Metrics.CallCompleted(frame.Action, "inbound", time.Since(start), status)
	}()

	var run func(ctx context.Context, payload []byte) ([]byte, error)
	if h, ok := c.reg.Resolve(frame.Action); ok {
		run = func(ctx context.Context, payload []byte) ([]byte, error) { return h(ctx, c, payload) }
	}
	hctx, cancelHandler := c.handlerContext(frame.MsgID)
	defer cancelHandler()
	out := c.pipe.RunCall(hctx, &frame, run)
	status = out.Status
	switch {
	case out.Err == nil:
		c.sendCallResult(frame.MsgID, out.Payload)
	case !out.NoReply:
		c.sendCallErrorWire(frame.MsgID, out.Wire, out.Err)
	}
}

// runSendHandler dispatches an inbound SEND (OCPP 2.1). A SEND is unconfirmed:
// the receiver MUST NOT reply with a CallResult or CallError, so every outcome
// (missing handler, schema-invalid, handler error) is logged and dropped.
func (c *Conn) runSendHandler(frame ocppj.Frame) {
	var run func(ctx context.Context, payload []byte) error
	if h, ok := c.reg.LookupSend(frame.Action); ok {
		run = func(ctx context.Context, payload []byte) error { return h(ctx, c, payload) }
	}
	hctx, cancelHandler := c.handlerContext(frame.MsgID)
	defer cancelHandler()
	c.pipe.RunSend(hctx, &frame, run)
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

// stackReserve is the frame space runHandler reserves up front; see there.
const stackReserve = 600

//go:noinline
func reserveStack(*[stackReserve]byte) {}
