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

// DefaultMaxMessageSize is the default inbound message size limit (1 MiB).
const DefaultMaxMessageSize int64 = 1 << 20

// Config controls a single connection's behavior.
type Config struct {
	CallTimeout  time.Duration
	WriteTimeout time.Duration
	PingInterval time.Duration
	PongWait     time.Duration
	// ReadTimeout cancels the connection if no inbound frame (data, ping, or
	// pong) arrives within this duration. 0 disables the read idle watchdog.
	ReadTimeout time.Duration
	// SerializeOutboundCalls allows at most one outstanding outbound CALL per
	// connection, as OCPP 1.6J §4.1.1 and 2.0.1 Part 4 §4.1 require. It is on in
	// DefaultConfig; turning it off is non-compliant and only meant for peers or
	// tests that need concurrent calls.
	SerializeOutboundCalls bool
	// HandlerTimeout bounds each inbound handler: its context carries this
	// deadline. 0 disables the deadline. Handler contexts are NOT cancelled when
	// the connection drops (see Conn.HandlerContext).
	HandlerTimeout time.Duration
	// MaxMessageSize is the largest inbound WebSocket message accepted, in bytes.
	MaxMessageSize int64
	// OutboundQueueSize is ignored. Connections no longer have a writer goroutine
	// and queue: each sender writes under a per-connection FIFO write lock. The
	// field is kept so existing configuration still compiles.
	OutboundQueueSize int
	// InboundQueueSize bounds the per-connection queue of inbound CALL/SEND
	// frames that wait for a free handler slot (or for ReleaseInbound). It matters
	// only while all MaxConcurrentHandlers slots are busy: further inbound CALLs
	// wait here, and once it is full the reader stops reading, so CALLRESULTs on
	// that connection are not resolved either. Raise it if handlers on one
	// connection routinely block for long (for example while waiting for a reply
	// to a Call they sent back to the charge point). The queue is allocated only
	// while frames wait. Values below 1 mean nothing is queued: the reader waits
	// for a free slot.
	InboundQueueSize int
	// AsyncQueueSize bounds the per-connection queue of calls waiting for the
	// outbound slot (sync and async) when SerializeOutboundCalls is set; waiting
	// beyond it returns ocppj.ErrQueueFull.
	AsyncQueueSize        int
	MaxConcurrentHandlers int64
	// GlobalHandlerLimiter, when non-nil, bounds the total number of inbound
	// handlers running concurrently across all connections that share it. It is
	// acquired in addition to the per-connection MaxConcurrentHandlers budget.
	// nil disables the global cap. The same *semaphore.Weighted must be shared
	// by every connection for the limit to be server-wide.
	GlobalHandlerLimiter *semaphore.Weighted
	// HandlerPool, when non-nil, runs inbound handlers on resident goroutines
	// with warm stacks when one is idle (see HandlerPool); nil, the default,
	// starts a goroutine per message. Share one pool across connections.
	HandlerPool *HandlerPool
	// DeepHandlerStack grows the stack of every goroutine started for an
	// inbound message to 16 KB before the handler runs. Set it when handlers
	// are known to go deep (a gateway forwarding each CALL through a link
	// transport and its encoder): a 2 KB stack that grows as needed is copied
	// three or four times on the way down, each time with more frames to
	// adjust, and one copy of an almost empty stack costs a fraction of that.
	// Leave it off for ordinary handlers, which fit the default.
	DeepHandlerStack bool
	Logger           *slog.Logger
	Metrics          MetricsHook
	Tracer           observability.Tracer
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
	// SkipOutboundValidation turns schema validation (and lenient repair) off for
	// payloads this side produces: replies of local handlers, outbound CALLs and
	// SENDs. Inbound messages, including the CALLRESULT answering an outbound
	// CALL, are still validated according to SchemaMode. A throughput knob for
	// deployments whose typed handlers are trusted to build valid payloads.
	SkipOutboundValidation bool

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
		CallTimeout:            30 * time.Second,
		WriteTimeout:           10 * time.Second,
		PingInterval:           54 * time.Second,
		PongWait:               60 * time.Second,
		ReadTimeout:            60 * time.Second,
		SerializeOutboundCalls: true,
		HandlerTimeout:         60 * time.Second,
		MaxMessageSize:         DefaultMaxMessageSize,
		OutboundQueueSize:      4,
		InboundQueueSize:       4,
		AsyncQueueSize:         64,
		MaxConcurrentHandlers:  16,
		Logger:                 slog.Default(),
		Metrics:                NoopMetrics,
		Tracer:                 observability.NewTracer(nil),
	}
}
