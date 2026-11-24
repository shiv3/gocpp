package csms

import (
	"context"
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
	"github.com/shiv3/gocpp/gateway"
	"go.opentelemetry.io/otel/trace"
)

type serverConfig struct {
	// lenientExtras holds the opt-in repairs of lenient mode (empty numbers,
	// default for a missing property, unknown enum values).
	lenientExtras schema.LenientOptions
	// lenientTimeLocation interprets zone-less date-times in lenient mode (nil = UTC).
	lenientTimeLocation *time.Location
	// ocppGoValidation selects ocpp-go's validation policy within lenient mode.
	ocppGoValidation bool
	// keepAcceptingAfterShutdown: Shutdown only closes the current connections.
	keepAcceptingAfterShutdown bool
	dispatcher                 dispatcher.Config
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
	router         storage.MessageRouter //nolint:staticcheck // legacy interface, kept for compatibility (deprecated in favour of gateway.Downstream)
	txStore        storage.TransactionStore
	cfgStore       storage.ConfigStore
	metrics        observability.Metrics
	tracerProvider trace.TracerProvider
	onConnect      func(*Conn)
	onDisconnect   func(*Conn, error)
	sessionObs     observability.SessionObserver
	connConfig     func(r *http.Request, cpID string, t *ConnTimeouts)

	originPatterns           []string
	insecureSkipVerifyOrigin bool
	checkOrigin              func(r *http.Request) bool

	// Gateway mode (see gateway_edge.go, gateway_backend.go).
	edgeLink       gateway.GatewayLink // set: this server is a gateway
	gatewayID      string
	link           gateway.BackendLink // set: this server is a processing tier
	splitInProcess bool
	splitLink      SplitLink
	backendInfo    gateway.BackendInfo
	gwRouter       gateway.Router
	gwAuthCache    *AuthCacheConfig
	attrSource     gateway.AttrSource
	attrSourceOpts AttrSourceOptions
	onOrphan       func(cpID, callID, action string, payload []byte, err error)
	edgeResponders map[string]EdgeResponder
	// edgeTimeEncoding is timeEncoding as the gateway keeps it (the processing tier applies it otherwise).
	edgeTimeEncoding  schema.TimeEncoding
	onEdgeResponse    func(cpID, action string)
	edgeHeartbeat     bool
	edgeHeartbeatWhen []func(EdgeRequest) bool

	tenantLimits *TenantLimits
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
	// DuplicatePolicyRejectNewAfterUpgrade keeps the existing connection and, for
	// the incoming duplicate, completes the WebSocket upgrade and then closes it
	// with status 1008 (policy violation, "a connection with this ID already
	// exists"). This is what upstream ocpp-go v0.19.0 does ("first wins"); unlike
	// DuplicatePolicyRejectNew, the charger sees an open-then-closed socket rather
	// than a refused upgrade (HTTP 409).
	//
	// Part of WithOCPPGoCompat. Be aware that a charger that reconnects after a
	// half-open drop is refused for as long as the server has not noticed the old
	// socket is dead (up to the read timeout), and keeps retrying; put
	// DuplicatePolicyCloseExisting back after WithOCPPGoCompat to avoid that.
	DuplicatePolicyRejectNewAfterUpgrade
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
// 1009. n <= 0 keeps the default; see WithUnlimitedMessageSize for no limit.
func WithMaxMessageSize(n int64) Option {
	return optionFunc(func(c *serverConfig) {
		if n > 0 {
			c.dispatcher.MaxMessageSize = n
		}
	})
}

// WithUnlimitedMessageSize removes the inbound WebSocket message size limit,
// as upstream ocpp-go v0.19.0 has none. Part of WithOCPPGoCompat; not
// recommended on an internet-facing endpoint: a peer can then make this process
// buffer a message of any size. After WithOCPPGoCompat, WithMaxMessageSize(n)
// puts a limit back.
func WithUnlimitedMessageSize() Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.MaxMessageSize = -1 })
}

// WithKeepAcceptingAfterShutdown makes Shutdown close the connections that exist
// when it is called and nothing more: upgrades keep being accepted and served
// afterwards, as with ocpp-go, whose server has no shutdown state beyond closing
// its listener (which the embedder does when it mounts Handler on its own HTTP
// server). Part of WithOCPPGoCompat. WithRefuseUpgradesAfterShutdown, the default,
// answers every upgrade from the moment Shutdown or Close is called with 503.
// Close always stops the server for good.
func WithKeepAcceptingAfterShutdown() Option {
	return optionFunc(func(c *serverConfig) { c.keepAcceptingAfterShutdown = true })
}

// WithRefuseUpgradesAfterShutdown is the default (HTTP 503 once Shutdown or
// Close has begun); it puts that back after WithOCPPGoCompat.
func WithRefuseUpgradesAfterShutdown() Option {
	return optionFunc(func(c *serverConfig) { c.keepAcceptingAfterShutdown = false })
}

// WithAbruptIdleClose makes a connection ended by the read idle timeout
// (WithWebSocketReadTimeout) drop the TCP connection without a close frame, so
// the charger sees close code 1006 as with ocpp-go. Part of WithOCPPGoCompat.
// WithGracefulIdleClose, the default, sends a close frame with 1000 instead.
func WithAbruptIdleClose() Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.AbortOnReadIdle = true })
}

// WithGracefulIdleClose closes an idle connection with a close frame (1000), the
// default; it puts that back after WithOCPPGoCompat.
func WithGracefulIdleClose() Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.AbortOnReadIdle = false })
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

// WithSessionObserver installs a callback that sees the lifecycle of every
// WebSocket session this server terminates: opened once the connection is
// registered, closed once when it ends (with the cause and the duration). It
// works on an embedded server and on a gateway alike (unlike WithOnConnect, which
// belongs to the processing tier), and runs inline, so it must not block; see
// observability/tap for an exporter that does not. Panics are recovered.
func WithSessionObserver(fn observability.SessionObserver) Option {
	return optionFunc(func(c *serverConfig) { c.sessionObs = fn })
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
		c.ocppGoValidation = false
	})
}

// WithOCPPGoValidation validates inbound messages the way upstream ocpp-go
// v0.19.0 effectively does, which is the validation of WithOCPPGoCompat: schema
// violations reject the message (wrong types, enum values including their case,
// out of range or over-long values, missing properties) except for what ocpp-go
// accepts too: unknown properties, a null payload, a zone-less date-time and
// structured JSON in a DataTransfer "data" property. WithLenientSchema, listed
// afterwards, brings back gocpp's broader repairs. Needs WithSchemaRegistry.
func WithOCPPGoValidation() Option {
	return optionFunc(func(c *serverConfig) {
		c.dispatcher.SchemaMode = dispatcher.SchemaModeLenient
		c.ocppGoValidation = true
	})
}

// WithAnswerInvalidResponses makes the server answer a CALLRESULT that fails
// validation with a CALLERROR carrying the request's message id (OCPP 1.6 and
// 2.0.1), as ocpp-go does; the caller's request still fails at once. Part of
// WithOCPPGoCompat. Off by default: the failure stays local and the peer hears
// nothing. WithAnswerInvalidResponses(false) turns it off again.
func WithAnswerInvalidResponses(on bool) Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.AnswerInvalidResponse = on })
}

// WithLenientTimeLocation sets the location in which lenient mode interprets
// date-times that carry no zone designator ("2023-10-19T15:28:07"). Default UTC.
// It has no effect outside lenient mode.
func WithLenientTimeLocation(loc *time.Location) Option {
	return optionFunc(func(c *serverConfig) { c.lenientTimeLocation = loc })
}

// WithLenientEmptyNumbers makes lenient mode read an empty string where the
// schema wants an integer or a number as 0 (some firmware sends "" for a value it
// does not have). It is lossy, so it is off by default. Needs WithLenientSchema;
// it has no effect in any other schema mode or under WithOCPPGoValidation.
func WithLenientEmptyNumbers() Option {
	return optionFunc(func(c *serverConfig) { c.lenientExtras.EmptyNumberAsZero = true })
}

// WithLenientDefaultMissing makes lenient mode fill in value for a required
// string property the charge point left out, before validation (for example
// WithLenientDefaultMissing("chargingRateUnit", "W")). value must satisfy the
// schema. Off by default; needs WithLenientSchema and has no effect under
// WithOCPPGoValidation.
func WithLenientDefaultMissing(property, value string) Option {
	return optionFunc(func(c *serverConfig) {
		m := make(map[string]string, len(c.lenientExtras.DefaultMissing)+1)
		for k, v := range c.lenientExtras.DefaultMissing {
			m[k] = v
		}
		m[property] = value
		c.lenientExtras.DefaultMissing = m
	})
}

// WithLenientMissingChargingRateUnit is WithLenientDefaultMissing for a
// ChargingSchedule without chargingRateUnit (read as "W"), which some stacks
// omit although both 1.6 and 2.0.1 require it.
func WithLenientMissingChargingRateUnit() Option {
	return WithLenientDefaultMissing("chargingRateUnit", "W")
}

// WithLenientUnknownEnumValues makes lenient mode pass values outside the closed
// enum of the named properties unchanged, instead of rejecting the message. It
// is meant for vendor extensions of an open list; see
// WithLenientVendorMeasurands. Off by default; needs WithLenientSchema and has
// no effect under WithOCPPGoValidation.
func WithLenientUnknownEnumValues(properties ...string) Option {
	return optionFunc(func(c *serverConfig) {
		c.lenientExtras.UnknownEnum = append(append([]string(nil), c.lenientExtras.UnknownEnum...), properties...)
	})
}

// WithLenientVendorMeasurands lets vendor-specific SampledValue measurands
// (for example "Energy.Request.Target") through lenient validation:
// WithLenientUnknownEnumValues("measurand").
func WithLenientVendorMeasurands() Option {
	return WithLenientUnknownEnumValues("measurand")
}

// lenientOptions builds the per-message lenient options from the config.
func (c *serverConfig) lenientOptions() schema.LenientOptions {
	o := c.lenientExtras
	o.TimeLocation = c.lenientTimeLocation
	o.OCPPGo = c.ocppGoValidation
	return o
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
//
// Experimental: this API may change before v1.0.
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
//
// Deprecated: use a gateway link (WithGatewayLink on the processing tier,
// NewGatewayServer for the connection tier) so CSMS-initiated calls travel as
// gateway.Downstream requests; csms.DownstreamRouter adapts a Downstream for
// callers that cannot move yet. Removed no earlier than v0.4.
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
// upstream ocpp-go v0.19.0 where gocpp differs, so chargers tuned against ocpp-go keep
// working during a staged migration. It is shorthand for, in order:
//
//   - WithOCPPGoValidation(): ocpp-go's validation instead of WithLenientSchema()
//   - WithAnswerInvalidResponses(true): an invalid CALLRESULT is answered with a CALLERROR
//   - WithErrorCodeMapper(dispatcher.OCPPGoErrorCodeMapper())
//   - WithMessageIDPolicy(dispatcher.MessageIDDrop)
//   - WithTimeEncoding(UTC, whole seconds)
//   - WithWebSocketPingInterval(0): ocpp-go servers never ping
//   - WithWebSocketReadTimeout(60s): ocpp-go's server ping wait
//   - WithAbruptIdleClose(): an idle connection is dropped without a close frame
//   - WithDuplicatePolicy(DuplicatePolicyRejectNewAfterUpgrade): first wins
//   - WithUnlimitedMessageSize(): no inbound size limit
//   - WithKeepAcceptingAfterShutdown(): Shutdown closes the current connections only
//
// The last two are risky on an exposed endpoint (a charger reconnecting after a
// half-open drop is refused until the old socket times out; a peer can make the
// process buffer any message size); put gocpp's behaviour back with
// WithDuplicatePolicy(DuplicatePolicyCloseExisting), WithMaxMessageSize(n),
// WithGracefulIdleClose() and WithRefuseUpgradesAfterShutdown().
// Handler panics stay contained (InternalError) and are not reproduced.
//
// Options listed after it win, so any single behaviour can be put back, e.g.
// csms.WithOCPPGoCompat(), csms.WithWebSocketPingInterval(54*time.Second). The
// lenient schema mode and the time encoding need WithSchemaRegistry. See
// docs/migration-from-ocpp-go.md for what each part changes and why.
func WithOCPPGoCompat() Option {
	return optionFunc(func(c *serverConfig) {
		for _, o := range []Option{
			WithOCPPGoValidation(),
			WithAnswerInvalidResponses(true),
			WithErrorCodeMapper(dispatcher.OCPPGoErrorCodeMapper()),
			WithMessageIDPolicy(dispatcher.MessageIDDrop),
			WithTimeEncoding(schema.TimeEncoding{UTC: true, Precision: schema.TimePrecisionSeconds}),
			WithWebSocketPingInterval(0),
			WithWebSocketReadTimeout(60 * time.Second),
			WithAbruptIdleClose(),
			WithDuplicatePolicy(DuplicatePolicyRejectNewAfterUpgrade),
			WithUnlimitedMessageSize(),
			WithKeepAcceptingAfterShutdown(),
		} {
			o.apply(c)
		}
	})
}

// WithSplitInProcess makes the server run both tiers of a split deployment in
// this process: an internal gateway terminates the WebSockets (Handler returns
// its handler) and forwards every message over the in-process link to this
// server, which acts as the processing tier. The wire behaviour is identical to
// embedded mode and the same options configure both tiers; the point is the
// topology: handlers run behind the same contract and session handles as when
// the tiers are separate processes, which is what the conformance suite checks
// (GOCPP_TEST_SPLIT=1, see internal/splitmode). Get returns a handle for a
// connected charger, Connections lists the gateway's sessions.
//
// Experimental: this API may change before v1.0.
func WithSplitInProcess() Option {
	return optionFunc(func(c *serverConfig) { c.splitInProcess = true })
}

// SplitLink is a transport that has both ends of a split deployment in this
// process, for WithSplitLink. *inproc.Hub is one; a harness can wrap any other
// link (a NATS or gRPC pair on loopback) the same way.
//
// Experimental: this API may change before v1.0.
type SplitLink interface {
	// Gateway is the end the internal gateway uses.
	Gateway() gateway.GatewayLink
	// Backend is the end the processing tier uses to reach the gateway.
	Backend() gateway.BackendLink
	// AttachBackend attaches up as a backend and returns once it can take work.
	// The returned function drains it (no new work, wait for work in flight).
	AttachBackend(up gateway.Upstream, info gateway.BackendInfo) (drain func(ctx context.Context) error)
}

// WithSplitLink is WithSplitInProcess over a link of the caller's choosing: an
// internal gateway and this server, the processing tier, are joined by l. It is
// how the conformance suite is run in split mode over a real transport (see
// internal/splitmode and addons/link-nats/splitharness).
//
// Experimental: this API may change before v1.0.
func WithSplitLink(l SplitLink) Option {
	return optionFunc(func(c *serverConfig) { c.splitLink = l })
}

// WithBackendInfo declares who a processing-tier server is when it attaches to a
// link: its pool ("" = gateway.DefaultPool), build version, role, labels. A
// gateway only sends inbound work to pools its router names, and without a
// router that is the default pool only, so starting a server in another pool
// is never a traffic change by itself. Proto, Caps and ID are filled in.
func WithBackendInfo(info gateway.BackendInfo) Option {
	return optionFunc(func(c *serverConfig) { c.backendInfo = info })
}

// WithRouter sets the routing hook of a gateway (NewGatewayServer): for every
// message it names the backend pool that serves it, from the charger id, the
// action and the session attributes. Default gateway.DefaultRouter.
//
// Experimental: this API may change before v1.0.
func WithRouter(r gateway.Router) Option {
	return optionFunc(func(c *serverConfig) { c.gwRouter = r })
}

// AuthCacheConfig configures the gateway's cache of connect decisions.
type AuthCacheConfig struct {
	// TTL is how long an accepted connect is answered from the cache without
	// asking the backend. Default 5 minutes.
	TTL time.Duration
	// StaleIfError is how long past TTL an accepted decision is still served when
	// the backend cannot be reached (no backend attached, link failure). 0 serves
	// no stale decision. Default 1 hour.
	StaleIfError time.Duration
	// MaxEntries bounds the cache (default 100000); at the limit expired entries
	// go first, then an arbitrary one.
	MaxEntries int
}

// WithGatewayAuthCache makes a gateway (NewGatewayServer) remember the backend's
// positive connect decisions, keyed by a hash of what the decision can depend
// on: the charge point id, request path, Authorization header, offered
// subprotocols and the client certificate. Within TTL a reconnect is admitted
// without a round trip (a reconnect storm does not hit the backend's credential
// store), and after TTL the backend is asked again; if it cannot be reached an
// accepted decision up to StaleIfError old is reused, so charge points keep
// reconnecting through a backend outage. Rejections are never cached and drop
// the entry. Do not enable it when the backend decides on other request headers;
// call Server.InvalidateAuthCache after a credential change.
//
// Experimental: this API may change before v1.0.
func WithGatewayAuthCache(cfg AuthCacheConfig) Option {
	return optionFunc(func(c *serverConfig) { c.gwAuthCache = &cfg })
}

// AttrSourceOptions tunes WithAttrSource.
type AttrSourceOptions struct {
	// Timeout bounds the lookup at connect (default 200 ms); on failure the
	// session starts without external attributes.
	Timeout time.Duration
	// Refresh, if positive, re-reads the attributes of every connected session at
	// this interval and updates them (a failed lookup keeps the old ones).
	Refresh time.Duration
	// OnResult is called once per lookup with "ok", "timeout" or "error", for
	// metrics.
	OnResult func(result string)
}

// WithAttrSource makes a gateway (NewGatewayServer) add the attributes of src to
// every session, prefixed "ext." (gateway.ExtAttrPrefix), after the processing
// tier's own: rules can match them like any attribute, and they can never
// override one the backend set. See gateway.AttrSource.
//
// Experimental: this API may change before v1.0.
func WithAttrSource(src gateway.AttrSource, o AttrSourceOptions) Option {
	return optionFunc(func(c *serverConfig) { c.attrSource, c.attrSourceOpts = src, o })
}
