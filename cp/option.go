package cp

import (
	"crypto/tls"
	"log/slog"
	"net/http"
	"time"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/ocppj/signing"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/core/transport"
)

type clientConfig struct {
	// lenientTimeLocation interprets zone-less date-times in lenient mode (nil = UTC).
	lenientTimeLocation *time.Location
	dispatcher          dispatcher.Config
	// timeEncoding rewrites outbound date-times (zero = untouched).
	timeEncoding         schema.TimeEncoding
	subProtocols         []string
	compressionMode      transport.CompressionMode
	heartbeatInterval    time.Duration
	basicAuthUser        string
	basicAuthPass        string
	httpHeader           http.Header
	tlsConfig            *tls.Config
	offlineQueueCapacity int
	retryInFlight        bool
	reconnectInitial     time.Duration
	reconnectMax         time.Duration
	reconnectMaxElapsed  time.Duration
	onConnect            func()
	onDisconnect         func(error)
	onReconnect          func()
	registry             *schema.Registry
}

func defaultClientConfig() clientConfig {
	return clientConfig{
		dispatcher:      dispatcher.DefaultConfig(),
		subProtocols:    []string{"ocpp1.6"},
		compressionMode: transport.CompressionNoContextTakeover,
	}
}

// Option configures a Client.
type Option interface{ apply(*clientConfig) }

type optionFunc func(*clientConfig)

func (f optionFunc) apply(c *clientConfig) { f(c) }

// WithSubProtocols sets offered subprotocols.
func WithSubProtocols(p ...string) Option {
	return optionFunc(func(c *clientConfig) { c.subProtocols = p })
}

// WithCompression sets the RFC 7692 permessage-deflate mode for the dial.
// Defaults to transport.CompressionNoContextTakeover; pass
// transport.CompressionDisabled to opt out.
func WithCompression(m transport.CompressionMode) Option {
	return optionFunc(func(c *clientConfig) { c.compressionMode = m })
}

// WithSigner signs outbound CALL/SEND messages (OCPP 2.1 Signed Messages).
func WithSigner(s *signing.Signer) Option {
	return optionFunc(func(c *clientConfig) { c.dispatcher.Signer = s })
}

// WithVerifier verifies inbound signed CALL/SEND messages.
func WithVerifier(v *signing.Verifier) Option {
	return optionFunc(func(c *clientConfig) { c.dispatcher.Verifier = v })
}

// WithRequireSignature rejects inbound signed messages that fail verification
// instead of unwrapping and processing them.
func WithRequireSignature(require bool) Option {
	return optionFunc(func(c *clientConfig) { c.dispatcher.RequireSignatureVerification = require })
}

// WithLogger sets the structured logger.
func WithLogger(l *slog.Logger) Option {
	return optionFunc(func(c *clientConfig) { c.dispatcher.Logger = l })
}

// WithCallTimeout sets the per-call timeout.
func WithCallTimeout(d time.Duration) Option {
	return optionFunc(func(c *clientConfig) { c.dispatcher.CallTimeout = d })
}

// WithHeartbeatInterval sets the OCPP Heartbeat interval.
func WithHeartbeatInterval(d time.Duration) Option {
	return optionFunc(func(c *clientConfig) { c.heartbeatInterval = d })
}

// WithWebSocketPingInterval sets the transport ping interval.
func WithWebSocketPingInterval(d time.Duration) Option {
	return optionFunc(func(c *clientConfig) { c.dispatcher.PingInterval = d })
}

// WithWebSocketPongWait sets the transport pong timeout.
func WithWebSocketPongWait(d time.Duration) Option {
	return optionFunc(func(c *clientConfig) { c.dispatcher.PongWait = d })
}

// WithWebSocketReadTimeout sets the read idle timeout: the connection is closed
// if no inbound frame (data, ping, or pong) arrives within d. 0 disables it.
func WithWebSocketReadTimeout(d time.Duration) Option {
	return optionFunc(func(c *clientConfig) { c.dispatcher.ReadTimeout = d })
}

// WithSerializedCalls limits outbound OCPP Calls to one outstanding request per
// connection (OCPP 1.6J §4.1.1, 2.0.1 Part 4 §4.1). This is the default; the
// option only re-enables it after WithConcurrentCalls.
func WithSerializedCalls() Option {
	return optionFunc(func(c *clientConfig) { c.dispatcher.SerializeOutboundCalls = true })
}

// WithConcurrentCalls lifts the one-outstanding-CALL rule: every Call is written
// at once. This violates OCPP's synchronicity requirement and some charge points
// misbehave or drop requests; use it only for peers known to cope, or in tests.
func WithConcurrentCalls() Option {
	return optionFunc(func(c *clientConfig) { c.dispatcher.SerializeOutboundCalls = false })
}

// WithHandlerTimeout bounds every inbound handler: its context gets this
// deadline (default 60s, 0 = none). A handler that returns the context's
// deadline error is answered with an InternalError CALLERROR. The deadline is
// cooperative; the handler must honor ctx. Handler contexts are not cancelled
// when the connection drops, so work in flight (e.g. a database write) can
// finish; a handler that wants to stop on disconnect watches ConnDone(ctx).
func WithHandlerTimeout(d time.Duration) Option {
	return optionFunc(func(c *clientConfig) { c.dispatcher.HandlerTimeout = d })
}

// WithMaxMessageSize sets the largest inbound WebSocket message accepted, in
// bytes (default 1 MiB); a larger message closes the connection with status
// 1009. n <= 0 keeps the default.
func WithMaxMessageSize(n int64) Option {
	return optionFunc(func(c *clientConfig) {
		if n > 0 {
			c.dispatcher.MaxMessageSize = n
		}
	})
}

// WithInboundQueueSize sets the depth of the per-connection queue between the
// socket reader and the handler dispatcher (default 4). The queue only fills
// while all handler slots are busy; once it is full the reader stops, so replies
// on that connection wait too. Raise it for handlers that block for long, for
// example while awaiting a reply to a Call they sent to the peer. n <= 0 keeps
// the default.
func WithInboundQueueSize(n int) Option {
	return optionFunc(func(c *clientConfig) {
		if n > 0 {
			c.dispatcher.InboundQueueSize = n
		}
	})
}

// WithOutboundQueueSize sets the depth of the channel feeding the connection's
// writer (default 4). Every send waits for its own write result, so a larger
// buffer only helps many goroutines sending at once. n <= 0 keeps the default.
func WithOutboundQueueSize(n int) Option {
	return optionFunc(func(c *clientConfig) {
		if n > 0 {
			c.dispatcher.OutboundQueueSize = n
		}
	})
}

// WithAsyncQueueSize bounds the per-connection queue of Calls (sync and async)
// waiting for the outstanding-call slot (default 64). Waiting beyond it fails at
// once with ocppj.ErrQueueFull. Unused with WithConcurrentCalls.
func WithAsyncQueueSize(n int) Option {
	return optionFunc(func(c *clientConfig) { c.dispatcher.AsyncQueueSize = n })
}

// WithOfflineQueue enables a bounded FIFO queue for CP-originated calls while disconnected.
func WithOfflineQueue(capacity int) Option {
	return optionFunc(func(c *clientConfig) {
		if capacity <= 0 {
			c.offlineQueueCapacity = 0
			return
		}
		c.offlineQueueCapacity = capacity
		c.dispatcher.SerializeOutboundCalls = true
	})
}

// WithRetryInFlightCalls makes the offline queue re-send a CALL that was already
// in-flight when the connection dropped, instead of failing it. Off by default:
// OCPP gives no idempotency guarantee, so an in-flight CALL the peer may already
// have received fails with ocppj.ErrConnClosed rather than risk double execution.
// Only affects connections using WithOfflineQueue.
func WithRetryInFlightCalls() Option {
	return optionFunc(func(c *clientConfig) { c.retryInFlight = true })
}

// WithReconnectBackoff tunes the exponential backoff Run uses between connection
// attempts: the first delay (initial), the cap on any delay (maxInterval), and how long
// Run keeps failing to connect before it gives up and returns (maxElapsed).
// A zero initial or maxInterval keeps the default (500ms / 60s); a zero maxElapsed, the
// default, means Run retries until its context is cancelled. The schedule
// restarts after every successful connection.
func WithReconnectBackoff(initial, maxInterval, maxElapsed time.Duration) Option {
	return optionFunc(func(c *clientConfig) {
		c.reconnectInitial = initial
		c.reconnectMax = maxInterval
		c.reconnectMaxElapsed = maxElapsed
	})
}

// WithOnConnect registers a callback fired after a successful connection.
func WithOnConnect(fn func()) Option {
	return optionFunc(func(c *clientConfig) { c.onConnect = fn })
}

// WithOnDisconnect registers a callback fired after the active connection drops.
func WithOnDisconnect(fn func(error)) Option {
	return optionFunc(func(c *clientConfig) { c.onDisconnect = fn })
}

// WithOnReconnect registers a callback fired after Run re-establishes a dropped connection.
func WithOnReconnect(fn func()) Option {
	return optionFunc(func(c *clientConfig) { c.onReconnect = fn })
}

// WithBasicAuth sets HTTP Basic authentication for the WebSocket dial.
func WithBasicAuth(username, password string) Option {
	return optionFunc(func(c *clientConfig) {
		c.basicAuthUser = username
		c.basicAuthPass = password
	})
}

// WithHTTPHeader appends an HTTP header to the WebSocket dial request.
func WithHTTPHeader(key, value string) Option {
	return optionFunc(func(c *clientConfig) {
		if c.httpHeader == nil {
			c.httpHeader = make(http.Header)
		}
		c.httpHeader.Add(key, value)
	})
}

// WithTLSConfig sets the TLS configuration used by the WebSocket dial.
func WithTLSConfig(cfg *tls.Config) Option {
	return optionFunc(func(c *clientConfig) { c.tlsConfig = cfg })
}

// WithSchemaRegistry sets the schema registry used for first-layer validation.
func WithSchemaRegistry(r *schema.Registry) Option {
	return optionFunc(func(c *clientConfig) { c.registry = r })
}

// WithStrictSchema controls whether schema validation failures reject the message.
// Passing false turns schema validation off. If WithStrictSchema and
// WithTolerantSchema are both used, the last option in the list wins.
func WithStrictSchema(strict bool) Option {
	return optionFunc(func(c *clientConfig) {
		if strict {
			c.dispatcher.SchemaMode = dispatcher.SchemaModeStrict
		} else {
			c.dispatcher.SchemaMode = dispatcher.SchemaModeOff
		}
	})
}

// WithTolerantSchema enables schema validation that logs validation failures
// but continues processing messages. If WithStrictSchema and WithTolerantSchema
// are both used, the last option in the list wins.
func WithTolerantSchema() Option {
	return optionFunc(func(c *clientConfig) {
		c.dispatcher.SchemaMode = dispatcher.SchemaModeTolerant
	})
}

// WithOutboundValidation controls whether payloads this side produces (replies of
// registered handlers, typed and raw outbound Calls and Sends) go through schema
// validation. The default is true. Passing false skips validation, and the lenient
// repairs (enum case, timestamp and number coercion), for those payloads only:
// inbound messages, including the CALLRESULT answering your own call, are validated
// as configured. Typed message structs generated from the schemas rarely produce an
// invalid payload, so turning this off removes a validation pass per message at
// high request rates; keep it on while developing handlers that build raw payloads.
func WithOutboundValidation(on bool) Option {
	return optionFunc(func(c *clientConfig) { c.dispatcher.SkipOutboundValidation = !on })
}

// WithLenientSchema enables lenient JSON-Schema validation: structurally broken
// messages are rejected, while benign violations are logged and passed, and
// enum case mismatches are normalized to canonical values. Last-wins with
// WithStrictSchema / WithTolerantSchema.
func WithLenientSchema() Option {
	return optionFunc(func(c *clientConfig) {
		c.dispatcher.SchemaMode = dispatcher.SchemaModeLenient
	})
}

// WithFrameObserver installs a callback that sees every raw OCPP-J frame,
// inbound (after read, including unparsable frames) and outbound (after the
// write attempt, with the error if it failed). It runs inline on the
// connection's reader/writer goroutine, so it must be fast and must not block;
// panics are recovered. See observability.SlogFrameObserver.
func WithFrameObserver(fn observability.FrameObserver) Option {
	return optionFunc(func(c *clientConfig) { c.dispatcher.FrameObserver = fn })
}

// WithLenientTimeLocation sets the location in which lenient mode interprets
// date-times that carry no zone designator ("2023-10-19T15:28:07"). Default UTC.
// It has no effect outside lenient mode.
func WithLenientTimeLocation(loc *time.Location) Option {
	return optionFunc(func(c *clientConfig) { c.lenientTimeLocation = loc })
}

// WithTimeEncoding controls how date-time values are written on the wire in
// every outbound message (CALL, CALLRESULT, SEND): convert to UTC and/or fix the
// number of fractional-second digits, for chargers that reject fractional
// seconds or a non-UTC offset. The zero value (the default) leaves values as the
// message types encode them. Fields are found through the schema registry, so it
// needs WithSchemaRegistry; messages without a registered schema are sent
// unchanged.
func WithTimeEncoding(e schema.TimeEncoding) Option {
	return optionFunc(func(c *clientConfig) { c.timeEncoding = e })
}

// WithErrorCodeMapper overrides the CALLERROR code (or suppresses the reply) for
// errors gocpp generates itself: unknown action, schema rejection, undecodable
// payload, malformed frame, unsupported message type. It never sees errors a
// handler returns. dispatcher.OCPPGoErrorCodeMapper is a ready-made mapper.
func WithErrorCodeMapper(m dispatcher.ErrorCodeMapper) Option {
	return optionFunc(func(c *clientConfig) { c.dispatcher.ErrorCodeMapper = m })
}

// WithMessageIDPolicy sets how an inbound CALL whose message id is empty or
// longer than 36 characters (the OCPP-J limit) is handled. The default,
// dispatcher.MessageIDAuto, rejects under WithStrictSchema(true) and accepts
// (logging a warning) otherwise.
func WithMessageIDPolicy(p dispatcher.MessageIDPolicy) Option {
	return optionFunc(func(c *clientConfig) { c.dispatcher.MessageIDPolicy = p })
}

// WithOCPPGoCompat makes the client reproduce the charger-visible behaviour of
// the ocpp-go library where gocpp differs, for simulators and test rigs that
// were written against it. It is shorthand for, in order:
//
//   - WithLenientSchema()
//   - WithErrorCodeMapper(dispatcher.OCPPGoErrorCodeMapper())
//   - WithMessageIDPolicy(dispatcher.MessageIDDrop)
//   - WithTimeEncoding(UTC, whole seconds)
//
// The WebSocket keepalive is not changed: ocpp-go's client pings every 54s and
// waits 60s, the same as gocpp's defaults. Options listed after it win. The
// lenient schema mode and the time encoding need WithSchemaRegistry.
func WithOCPPGoCompat() Option {
	return optionFunc(func(c *clientConfig) {
		for _, o := range []Option{
			WithLenientSchema(),
			WithErrorCodeMapper(dispatcher.OCPPGoErrorCodeMapper()),
			WithMessageIDPolicy(dispatcher.MessageIDDrop),
			WithTimeEncoding(schema.TimeEncoding{UTC: true, Precision: schema.TimePrecisionSeconds}),
		} {
			o.apply(c)
		}
	})
}
