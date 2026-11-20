// Package dispatcher implements the version-agnostic OCPP connection lifecycle,
// pending-call tracking, and concurrency control. See spec §3.
package dispatcher

import (
	"log/slog"
	"time"

	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/ocppj/signing"
	"golang.org/x/sync/semaphore"
)

// FrameEvent and FrameObserver are defined in core/observability so the slog
// adapter can live there without an import cycle.
type (
	FrameEvent    = observability.FrameEvent
	FrameObserver = observability.FrameObserver
)

// SchemaMode controls how inbound JSON Schema validation failures are handled.
type SchemaMode int

const (
	SchemaModeOff SchemaMode = iota
	SchemaModeTolerant
	SchemaModeStrict
	SchemaModeLenient
)

// ErrorSource says which built-in condition produced a CALLERROR; see
// ErrorCodeMapper.
type ErrorSource int

const (
	// ErrorSourceUnknownAction: no handler is registered for the inbound action.
	ErrorSourceUnknownAction ErrorSource = iota + 1
	// ErrorSourceSchema: an inbound request was rejected by schema validation.
	// Err is a *schema.ViolationError when the keyword is known.
	ErrorSourceSchema
	// ErrorSourceDecode: the payload could not be decoded into the handler's
	// typed request. Err is the decoder's error.
	ErrorSourceDecode
	// ErrorSourceMalformedFrame: an inbound CALL with a readable message id had a
	// bad shape. Err is a *ocppj.ProtocolError.
	ErrorSourceMalformedFrame
	// ErrorSourceUnknownMessageType: a frame with an unsupported MessageTypeId.
	// Err is a *ocppj.UnknownMessageTypeError. By default only 2.x peers get a
	// reply (OCPP 1.6J has no MessageTypeNotSupported code).
	ErrorSourceUnknownMessageType
	// ErrorSourceMessageID: an inbound CALL whose message id breaks the 36
	// character limit, under a policy that answers it.
	ErrorSourceMessageID
)

// ErrorContext describes one built-in CALLERROR decision for an ErrorCodeMapper.
type ErrorContext struct {
	Source  ErrorSource
	Version ocppj.Version
	Action  string
	Err     error
	// WireCode is the code gocpp would send, spelled for Version. Reply is
	// whether it would send anything.
	WireCode string
	Reply    bool
}

// ErrorCodeMapper overrides the CALLERROR gocpp generates itself (never one a
// handler returns). It returns the exact wire code string to send and whether
// to reply at all; returning ctx.WireCode, ctx.Reply leaves the default.
type ErrorCodeMapper func(ctx ErrorContext) (wireCode string, reply bool)

// MessageIDPolicy says what happens to an inbound CALL whose message id is empty
// or longer than the 36 characters OCPP-J allows.
type MessageIDPolicy int

const (
	// MessageIDAuto follows the schema mode: MessageIDReject when strict,
	// MessageIDAccept (logging a soft violation) when lenient or tolerant, and
	// MessageIDAccept without logging when validation is off.
	MessageIDAuto MessageIDPolicy = iota
	// MessageIDAccept processes the call, whatever the id.
	MessageIDAccept
	// MessageIDReject answers an over-long id with a CALLERROR (FormationViolation
	// / FormatViolation, the id echoed) and drops an empty id, which cannot be
	// answered.
	MessageIDReject
	// MessageIDDrop drops both silently.
	MessageIDDrop
)

// Config controls a single connection's behavior.
type Config struct {
	CallTimeout  time.Duration
	WriteTimeout time.Duration
	PingInterval time.Duration
	PongWait     time.Duration
	// ReadTimeout cancels the connection if no inbound frame (data, ping, or
	// pong) arrives within this duration. 0 disables the read idle watchdog.
	ReadTimeout            time.Duration
	SerializeOutboundCalls bool
	OutboundQueueSize      int
	// AsyncQueueSize bounds the per-connection FIFO queue used by DoCallAsync when
	// SerializeOutboundCalls is set; enqueuing beyond it returns ocppj.ErrQueueFull.
	AsyncQueueSize        int
	MaxConcurrentHandlers int64
	// GlobalHandlerLimiter, when non-nil, bounds the total number of inbound
	// handlers running concurrently across all connections that share it. It is
	// acquired in addition to the per-connection MaxConcurrentHandlers budget.
	// nil disables the global cap. The same *semaphore.Weighted must be shared
	// by every connection for the limit to be server-wide.
	GlobalHandlerLimiter *semaphore.Weighted
	Logger               *slog.Logger
	Metrics              MetricsHook
	Tracer               observability.Tracer
	// FrameObserver, when non-nil, sees every raw inbound frame (after read,
	// including unparsable ones) and every outbound frame (after the write
	// attempt, with the write error if any). It runs inline on the reader or
	// writer goroutine and must be fast; panics are recovered.
	FrameObserver FrameObserver
	// SchemaValidate optionally validates a payload for the given version.
	// nil disables validation. SchemaMode controls how returned errors are handled.
	SchemaValidate func(version ocppj.Version, action, kind string, payload []byte) error
	// SchemaValidateLenient validates a payload in lenient mode. It returns the
	// possibly enum-normalized payload to dispatch, the soft-violation keywords
	// seen, and a non-nil error only for hard violations. Used only when
	// SchemaMode == SchemaModeLenient. Returns primitives so the dispatcher stays
	// decoupled from core/schema.
	SchemaValidateLenient func(version ocppj.Version, action, kind string, payload []byte) (out []byte, soft []string, err error)
	SchemaMode            SchemaMode

	// OutboundTransform, when non-nil, rewrites every outbound CALL, SEND and
	// CALLRESULT payload (kind is "request" or "response") after validation and
	// before signing; the date-time encoding option uses it.
	OutboundTransform func(version ocppj.Version, action, kind string, payload []byte) ([]byte, error)
	// ErrorCodeMapper, when non-nil, may change the CALLERROR code (or suppress
	// the reply) for errors generated by the dispatcher itself.
	ErrorCodeMapper ErrorCodeMapper
	// MessageIDPolicy governs inbound CALLs with an empty or over-long id.
	MessageIDPolicy MessageIDPolicy

	// Signer, when non-nil, signs outbound CALL/SEND messages as OCPP 2.1 Signed
	// Messages (Part 4 Ch 7).
	Signer *signing.Signer
	// Verifier, when non-nil, verifies inbound signed CALL/SEND messages.
	Verifier *signing.Verifier
	// RequireSignatureVerification rejects a signed inbound message whose
	// signature cannot be verified (instead of unwrapping and processing it).
	RequireSignatureVerification bool
}

// DefaultConfig returns production-sane defaults.
func DefaultConfig() Config {
	return Config{
		CallTimeout:           30 * time.Second,
		WriteTimeout:          10 * time.Second,
		PingInterval:          54 * time.Second,
		PongWait:              60 * time.Second,
		ReadTimeout:           60 * time.Second,
		OutboundQueueSize:     64,
		AsyncQueueSize:        64,
		MaxConcurrentHandlers: 16,
		Logger:                slog.Default(),
		Metrics:               NoopMetrics,
		Tracer:                observability.NewTracer(nil),
	}
}
