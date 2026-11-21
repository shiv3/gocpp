package csms

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/shiv3/gocpp/core/auth"
	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/ocppj/signing"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/storage/memory"
	"github.com/shiv3/gocpp/core/transport"
	"go.opentelemetry.io/otel/trace"
)

type serverConfig struct {
	// lenientTimeLocation interprets zone-less date-times in lenient mode (nil = UTC).
	lenientTimeLocation *time.Location
	dispatcher          dispatcher.Config
	// timeEncoding rewrites outbound date-times (zero = untouched).
	timeEncoding    schema.TimeEncoding
	subProtocols    []string
	compressionMode transport.CompressionMode
	path            string
	cpIDExtractor   CPIDExtractor
	instanceID      string
	sessionReg      storage.SessionRegistry
	sessionAttrs    func(r *http.Request, cpID string) map[string]string
	sessionCfg      SessionConfig
	registry        *schema.Registry
	duplicatePolicy DuplicatePolicy

	globalConcurrencyLimit int
	maxConnections         int
	acceptLimit            *tokenBucket
	softMax                int
	ioReadBuffer           int
	ioWriteBuffer          int

	auth           auth.Authenticator
	connReg        storage.ConnectionRegistry
	router         storage.MessageRouter
	txStore        storage.TransactionStore
	cfgStore       storage.ConfigStore
	metrics        observability.Metrics
	tracerProvider trace.TracerProvider
	onConnect      func(*Conn)
	onDisconnect   func(*Conn, error)
	connConfig     func(r *http.Request, cpID string, t *ConnTimeouts)

	originPatterns           []string
	insecureSkipVerifyOrigin bool
	checkOrigin              func(r *http.Request) bool
}

func defaultServerConfig() serverConfig {
	return serverConfig{
		dispatcher:      dispatcher.DefaultConfig(),
		subProtocols:    []string{"ocpp1.6"},
		compressionMode: transport.CompressionNoContextTakeover,
		ioReadBuffer:    defaultIOReadBuffer,
		ioWriteBuffer:   defaultIOWriteBuffer,
		path:            "/ocpp/",
		duplicatePolicy: DuplicatePolicyCloseExisting,
		auth:            auth.None{},
		connReg:         memory.NewConnectionRegistry(),
		router:          memory.NewRouter(),
		txStore:         memory.NewTransactionStore(),
		cfgStore:        memory.NewConfigStore(),
		metrics:         observability.NoOp{},
	}
}

// Option configures a Server.
type Option interface{ apply(*serverConfig) }

type optionFunc func(*serverConfig)

func (f optionFunc) apply(c *serverConfig) { f(c) }

// CPIDExtractor extracts the charge point id from an HTTP upgrade request.
type CPIDExtractor func(r *http.Request) (cpID string, ok bool)

// DuplicatePolicy controls how the CSMS handles a second connection for the
// same charge point id.
type DuplicatePolicy int

const (
	// DuplicatePolicyCloseExisting closes the existing connection and accepts
	// the new one. This is the default behavior.
	DuplicatePolicyCloseExisting DuplicatePolicy = iota
	// DuplicatePolicyRejectNew rejects the incoming duplicate and keeps the
	// existing connection.
	DuplicatePolicyRejectNew
)

// ConnTimeouts are the timing parameters that can be overridden for a single
// charge point connection. It is pre-filled with the server-wide values; a zero
// PingInterval or ReadTimeout disables that check, as for the server options.
type ConnTimeouts struct {
	CallTimeout  time.Duration
	WriteTimeout time.Duration
	PingInterval time.Duration
	PongWait     time.Duration
	ReadTimeout  time.Duration
}

// WithConnConfig registers a hook evaluated for every accepted upgrade (after
// authentication, before the WebSocket handshake) that may adjust the timeouts of
// that one connection, e.g. a longer ping interval for a charger on a cellular
// link. cpID is the authenticated charge point id. Without the option every
// connection uses the server-wide settings.
func WithConnConfig(fn func(r *http.Request, cpID string, t *ConnTimeouts)) Option {
	return optionFunc(func(c *serverConfig) { c.connConfig = fn })
}

// WithCallTimeout sets the per-call timeout.
func WithCallTimeout(d time.Duration) Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.CallTimeout = d })
}

// WithWriteTimeout sets the per-write timeout.
func WithWriteTimeout(d time.Duration) Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.WriteTimeout = d })
}

// WithSubProtocols sets the offered WebSocket subprotocols (in preference order).
func WithSubProtocols(p ...string) Option {
	return optionFunc(func(c *serverConfig) { c.subProtocols = p })
}

// WithCompression sets the RFC 7692 permessage-deflate mode the server offers.
// Defaults to transport.CompressionNoContextTakeover; pass
// transport.CompressionDisabled to opt out.
func WithCompression(m transport.CompressionMode) Option {
	return optionFunc(func(c *serverConfig) { c.compressionMode = m })
}

// WithIOBufferSizes sets the size in bytes of each connection's socket read and
// write buffers (default 1024 each; net/http would give 4096 each). Smaller
// buffers cut the memory of idle connections by about 6 KB each; a frame larger
// than the write buffer costs one extra write syscall, and reads of large frames
// bypass the buffer. 0 keeps net/http's size for that side (4096); negative
// values are ignored.
func WithIOBufferSizes(read, write int) Option {
	return optionFunc(func(c *serverConfig) {
		if read >= 0 {
			c.ioReadBuffer = read
		}
		if write >= 0 {
			c.ioWriteBuffer = write
		}
	})
}

// WithHandlerPool runs inbound handlers on the resident workers of pool when one
// is idle, and on a new goroutine otherwise (never queued, so a slow handler
// cannot delay another connection). It saves the stack growth of a fresh
// goroutine per message; measure before enabling. The caller owns the pool and
// closes it after Shutdown. nil keeps a goroutine per message.
func WithHandlerPool(pool *dispatcher.HandlerPool) Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.HandlerPool = pool })
}

// WithSigner signs outbound CSMS->CP CALL/SEND messages (OCPP 2.1 Signed Messages).
func WithSigner(s *signing.Signer) Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.Signer = s })
}

// WithVerifier verifies inbound signed CP->CSMS messages.
func WithVerifier(v *signing.Verifier) Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.Verifier = v })
}

// WithRequireSignature rejects inbound signed messages that fail verification.
func WithRequireSignature(require bool) Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.RequireSignatureVerification = require })
}

// WithLogger sets the structured logger.
func WithLogger(l *slog.Logger) Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.Logger = l })
}

// WithWebSocketPingInterval sets the transport ping interval.
func WithWebSocketPingInterval(d time.Duration) Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.PingInterval = d })
}

// WithWebSocketPongWait sets the transport pong timeout.
func WithWebSocketPongWait(d time.Duration) Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.PongWait = d })
}

// WithWebSocketReadTimeout sets the read idle timeout: the connection is closed
// if no inbound frame (data, ping, or pong) arrives within d. 0 disables it.
func WithWebSocketReadTimeout(d time.Duration) Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.ReadTimeout = d })
}

// WithSerializedCalls limits outbound OCPP Calls to one outstanding request per
// connection (OCPP 1.6J §4.1.1, 2.0.1 Part 4 §4.1). This is the default; the
// option only re-enables it after WithConcurrentCalls.
func WithSerializedCalls() Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.SerializeOutboundCalls = true })
}

// WithConcurrentCalls lifts the one-outstanding-CALL rule: every Call is written
// at once. This violates OCPP's synchronicity requirement and some charge points
// misbehave or drop requests; use it only for peers known to cope, or in tests.
func WithConcurrentCalls() Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.SerializeOutboundCalls = false })
}

// WithHandlerTimeout bounds every inbound handler: its context gets this
// deadline (default 60s, 0 = none). A handler that returns the context's
// deadline error is answered with an InternalError CALLERROR. The deadline is
// cooperative; the handler must honor ctx. Handler contexts are not cancelled
// when the connection drops, so work in flight (e.g. a database write) can
// finish; a handler that wants to stop on disconnect watches ConnFromContext(ctx).Done().
func WithHandlerTimeout(d time.Duration) Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.HandlerTimeout = d })
}

// WithMaxMessageSize sets the largest inbound WebSocket message accepted, in
// bytes (default 1 MiB); a larger message closes the connection with status
// 1009. n <= 0 keeps the default.
func WithMaxMessageSize(n int64) Option {
	return optionFunc(func(c *serverConfig) {
		if n > 0 {
			c.dispatcher.MaxMessageSize = n
		}
	})
}

// WithMaxConnections caps the number of simultaneously connected charge points
// (0 = unlimited, the default). Beyond the cap an upgrade is refused before the
// WebSocket handshake with HTTP 503 and a Retry-After header, so the charge point
// sees an ordinary failed connect and backs off. A charge point reconnecting
// under an id that is already connected replaces its old connection and is never
// refused because of the cap.
func WithMaxConnections(n int) Option {
	return optionFunc(func(c *serverConfig) {
		if n < 0 {
			n = 0
		}
		c.maxConnections = n
	})
}

// WithInboundQueueSize sets the depth of the per-connection queue between the
// socket reader and the handler dispatcher (default 4). The queue only fills
// while all handler slots are busy; once it is full the reader stops, so replies
// on that connection wait too. Raise it for handlers that block for long, for
// example while awaiting a reply to a Call they sent to the peer. n <= 0 keeps
// the default.
func WithInboundQueueSize(n int) Option {
	return optionFunc(func(c *serverConfig) {
		if n > 0 {
			c.dispatcher.InboundQueueSize = n
		}
	})
}

// WithOutboundQueueSize has no effect: connections no longer have an outbound
// queue (senders write under a per-connection FIFO lock).
//
// Deprecated: remove the option.
func WithOutboundQueueSize(n int) Option {
	return optionFunc(func(c *serverConfig) {
		if n > 0 {
			c.dispatcher.OutboundQueueSize = n
		}
	})
}

// WithAsyncQueueSize bounds the per-connection queue of Calls (sync and async)
// waiting for the outstanding-call slot (default 64). Waiting beyond it fails at
// once with ocppj.ErrQueueFull. Unused with WithConcurrentCalls.
func WithAsyncQueueSize(n int) Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.AsyncQueueSize = n })
}

// WithOnConnect registers a callback fired after a charge point connection is accepted.
func WithOnConnect(fn func(*Conn)) Option {
	return optionFunc(func(c *serverConfig) { c.onConnect = fn })
}

// WithOnDisconnect registers a callback fired after a charge point connection drops.
func WithOnDisconnect(fn func(*Conn, error)) Option {
	return optionFunc(func(c *serverConfig) { c.onDisconnect = fn })
}

// WithInstanceID sets this CSMS instance's identifier and turns on multi-instance
// routing: the instance registers every charge point it holds in the registry's
// global table under this id, serves calls forwarded by other instances through
// the message router (WithMessageRouter), and Server.Lookup can forward calls to
// charge points held elsewhere. The id must be unique per running instance. See
// docs/clustering.md.
//
// Routes are recorded through a storage.SessionRegistry: the connection registry
// itself when it implements the interface (the in-memory registries do), else an
// adapter over its PutGlobal / LookupGlobal / DeleteGlobal with the guarantees
// those calls always had. Prefer WithSessionRegistry for new code.
func WithInstanceID(id string) Option {
	return optionFunc(func(c *serverConfig) { c.instanceID = id })
}

// WithPath sets the HTTP path prefix charge points connect to.
func WithPath(p string) Option {
	return optionFunc(func(c *serverConfig) { c.path = p })
}

// WithCPIDExtractor sets a custom charge point id extractor. When set, it
// replaces the default WithPath prefix + trailing path segment behavior.
// The returned id must be non-empty and must not contain a slash.
func WithCPIDExtractor(extract CPIDExtractor) Option {
	return optionFunc(func(c *serverConfig) { c.cpIDExtractor = extract })
}

// WithDuplicatePolicy sets how duplicate charge point connections are handled.
func WithDuplicatePolicy(p DuplicatePolicy) Option {
	return optionFunc(func(c *serverConfig) { c.duplicatePolicy = p })
}

// WithGlobalConcurrencyLimit caps the total number of inbound handlers running
// concurrently across all connections. This is a server-wide bound applied in
// addition to the per-connection handler budget; when the cap is reached,
// further inbound calls wait (backpressure) until a slot frees. A value <= 0
// disables the global cap (the default), leaving only per-connection limits.
func WithGlobalConcurrencyLimit(n int) Option {
	return optionFunc(func(c *serverConfig) { c.globalConcurrencyLimit = n })
}

// WithOriginPatterns lists host patterns for authorized WebSocket origins
// (cross-origin allowlist). The request host is always authorized. Patterns are
// matched per coder/websocket's AcceptOptions.OriginPatterns. Charge points are
// non-browser clients and usually send no Origin header (always allowed), so
// this is mainly relevant for browser-based clients.
func WithOriginPatterns(patterns ...string) Option {
	return optionFunc(func(c *serverConfig) { c.originPatterns = patterns })
}

// WithInsecureSkipVerifyOrigin disables WebSocket origin verification entirely,
// accepting upgrades from any origin. This mirrors disabling the origin check in
// other OCPP stacks. Use with care: enabling it on a browser-reachable endpoint
// exposes it to cross-site WebSocket hijacking. Prefer WithOriginPatterns when
// you only need to allow specific origins.
func WithInsecureSkipVerifyOrigin() Option {
	return optionFunc(func(c *serverConfig) { c.insecureSkipVerifyOrigin = true })
}

// WithCheckOrigin sets a custom WebSocket origin validator. When set, the
// function is called for every upgrade request; returning false rejects it with
// 403 Forbidden, and gocpp bypasses coder/websocket's built-in origin check (the
// supplied function is fully responsible for the decision). Use this for logic
// that WithOriginPatterns can't express; otherwise prefer WithOriginPatterns.
func WithCheckOrigin(fn func(r *http.Request) bool) Option {
	return optionFunc(func(c *serverConfig) { c.checkOrigin = fn })
}

// WithSchemaRegistry sets the schema registry used for first-layer validation.
func WithSchemaRegistry(r *schema.Registry) Option {
	return optionFunc(func(c *serverConfig) { c.registry = r })
}

// WithStrictSchema controls whether schema validation failures reject the message.
// Passing false turns schema validation off. If WithStrictSchema and
// WithTolerantSchema are both used, the last option in the list wins.
func WithStrictSchema(strict bool) Option {
	return optionFunc(func(c *serverConfig) {
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
	return optionFunc(func(c *serverConfig) {
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
	return optionFunc(func(c *serverConfig) { c.dispatcher.SkipOutboundValidation = !on })
}

// WithLenientSchema enables lenient JSON-Schema validation: structurally broken
// messages are rejected, while benign violations are logged and passed, and
// enum case mismatches are normalized to canonical values. Last-wins with
// WithStrictSchema / WithTolerantSchema.
func WithLenientSchema() Option {
	return optionFunc(func(c *serverConfig) {
		c.dispatcher.SchemaMode = dispatcher.SchemaModeLenient
	})
}

// WithLenientTimeLocation sets the location in which lenient mode interprets
// date-times that carry no zone designator ("2023-10-19T15:28:07"). Default UTC.
// It has no effect outside lenient mode.
func WithLenientTimeLocation(loc *time.Location) Option {
	return optionFunc(func(c *serverConfig) { c.lenientTimeLocation = loc })
}

// WithAuthenticator sets the connection authenticator (default auth.None).
func WithAuthenticator(a auth.Authenticator) Option {
	return optionFunc(func(c *serverConfig) { c.auth = a })
}

// WithSessionRegistry makes the server record every connection it holds in reg
// under a registry-assigned epoch and turns on multi-instance routing, like
// WithInstanceID (whose value, if set, becomes the gateway id unless cfg gives
// one). The registry claims the session before the WebSocket upgrade is accepted,
// releases it with a compare-and-delete when the connection ends, keeps a
// gateway lease alive and runs a reconcile loop that closes sockets superseded
// by a newer connection elsewhere. At most one SessionConfig may be given; omit
// it for the defaults. See docs/clustering.md.
//
// Servers that share one registry see each other's sessions; use
// memory.NewSessionRegistry in tests and addons/registry-redis in production.
func WithSessionRegistry(reg storage.SessionRegistry, cfg ...SessionConfig) Option {
	return optionFunc(func(c *serverConfig) {
		c.sessionReg = reg
		if len(cfg) > 0 {
			c.sessionCfg = cfg[0]
		}
	})
}

// WithSessionAttributes sets a function that derives the session attributes
// (site, vendor, model, firmware, tenant, ...) from the upgrade request, e.g. from
// the authenticated identity or headers. They are stored in the session record
// and can be matched by DrainOptions.Select and by rollout tooling reading the
// registry; update them later with Conn.SetAttrs or Server.SetSessionAttrs. The
// function must not retain the returned map.
func WithSessionAttributes(fn func(r *http.Request, cpID string) map[string]string) Option {
	return optionFunc(func(c *serverConfig) { c.sessionAttrs = fn })
}

// WithConnectionRegistry sets the connection registry (default in-memory).
func WithConnectionRegistry(r storage.ConnectionRegistry) Option {
	return optionFunc(func(c *serverConfig) { c.connReg = r })
}

// WithMessageRouter sets the inter-instance message router (default no-op).
func WithMessageRouter(r storage.MessageRouter) Option {
	return optionFunc(func(c *serverConfig) { c.router = r })
}

// WithTransactionStore sets the transaction store (default in-memory). It is an
// application-facing extension point: csms keeps it but never reads or writes it.
func WithTransactionStore(s storage.TransactionStore) Option {
	return optionFunc(func(c *serverConfig) { c.txStore = s })
}

// WithConfigStore sets the configuration store (default in-memory). It is an
// application-facing extension point: csms keeps it but never reads or writes it.
func WithConfigStore(s storage.ConfigStore) Option {
	return optionFunc(func(c *serverConfig) { c.cfgStore = s })
}

// WithMetrics sets the observability metrics sink (default NoOp).
func WithMetrics(m observability.Metrics) Option {
	return optionFunc(func(c *serverConfig) { c.metrics = m })
}

// WithTracerProvider sets the OpenTelemetry tracer provider (default no-op).
func WithTracerProvider(tp trace.TracerProvider) Option {
	return optionFunc(func(c *serverConfig) { c.tracerProvider = tp })
}

// WithFrameObserver installs a callback that sees every raw OCPP-J frame on
// every connection, inbound (after read, including unparsable frames) and
// outbound (after the write attempt, with the error if it failed). It runs
// inline on the connection's reader/writer goroutine, so it must be fast and
// must not block; panics are recovered. See observability.SlogFrameObserver for
// a ready-made logger.
func WithFrameObserver(fn observability.FrameObserver) Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.FrameObserver = fn })
}

// WithTimeEncoding controls how date-time values are written on the wire in
// every outbound message (CALL, CALLRESULT, SEND): convert to UTC and/or fix the
// number of fractional-second digits, for chargers that reject fractional
// seconds or a non-UTC offset. The zero value (the default) leaves values as the
// message types encode them. Fields are found through the schema registry, so it
// needs WithSchemaRegistry; messages without a registered schema are sent
// unchanged.
func WithTimeEncoding(e schema.TimeEncoding) Option {
	return optionFunc(func(c *serverConfig) { c.timeEncoding = e })
}

// WithErrorCodeMapper overrides the CALLERROR code (or suppresses the reply) for
// errors gocpp generates itself: unknown action, schema rejection, undecodable
// payload, malformed frame, unsupported message type. It never sees errors a
// handler returns. dispatcher.OCPPGoErrorCodeMapper is a ready-made mapper.
func WithErrorCodeMapper(m dispatcher.ErrorCodeMapper) Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.ErrorCodeMapper = m })
}

// WithMessageIDPolicy sets how an inbound CALL whose message id is empty or
// longer than 36 characters (the OCPP-J limit) is handled. The default,
// dispatcher.MessageIDAuto, rejects under WithStrictSchema(true) and accepts
// (logging a warning) otherwise.
func WithMessageIDPolicy(p dispatcher.MessageIDPolicy) Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.MessageIDPolicy = p })
}

// WithOCPPGoCompat makes the server reproduce the charger-visible behaviour of
// the ocpp-go library where gocpp differs, so chargers tuned against ocpp-go keep
// working during a staged migration. It is shorthand for, in order:
//
//   - WithLenientSchema()
//   - WithErrorCodeMapper(dispatcher.OCPPGoErrorCodeMapper())
//   - WithMessageIDPolicy(dispatcher.MessageIDDrop)
//   - WithTimeEncoding(UTC, whole seconds)
//   - WithWebSocketPingInterval(0): ocpp-go servers never ping
//   - WithWebSocketReadTimeout(60s): ocpp-go's server ping wait
//
// Options listed after it win, so any single behaviour can be put back, e.g.
// csms.WithOCPPGoCompat(), csms.WithWebSocketPingInterval(54*time.Second). The
// lenient schema mode and the time encoding need WithSchemaRegistry. See
// docs/migration-from-ocpp-go.md for what each part changes and why.
func WithOCPPGoCompat() Option {
	return optionFunc(func(c *serverConfig) {
		for _, o := range []Option{
			WithLenientSchema(),
			WithErrorCodeMapper(dispatcher.OCPPGoErrorCodeMapper()),
			WithMessageIDPolicy(dispatcher.MessageIDDrop),
			WithTimeEncoding(schema.TimeEncoding{UTC: true, Precision: schema.TimePrecisionSeconds}),
			WithWebSocketPingInterval(0),
			WithWebSocketReadTimeout(60 * time.Second),
		} {
			o.apply(c)
		}
	})
}
