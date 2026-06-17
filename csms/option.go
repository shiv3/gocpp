package csms

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/shiv3/gocpp/core/auth"
	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/storage/memory"
	"go.opentelemetry.io/otel/trace"
)

type serverConfig struct {
	dispatcher      dispatcher.Config
	subProtocols    []string
	path            string
	cpIDExtractor   CPIDExtractor
	instanceID      string
	registry        *schema.Registry
	duplicatePolicy DuplicatePolicy

	globalConcurrencyLimit int

	auth           auth.Authenticator
	connReg        storage.ConnectionRegistry
	router         storage.MessageRouter
	txStore        storage.TransactionStore
	cfgStore       storage.ConfigStore
	metrics        observability.Metrics
	tracerProvider trace.TracerProvider
	onConnect      func(*Conn)
	onDisconnect   func(*Conn, error)

	originPatterns           []string
	insecureSkipVerifyOrigin bool
	checkOrigin              func(r *http.Request) bool
}

func defaultServerConfig() serverConfig {
	return serverConfig{
		dispatcher:      dispatcher.DefaultConfig(),
		subProtocols:    []string{"ocpp1.6"},
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

// WithSerializedCalls limits outbound OCPP Calls to one outstanding request.
func WithSerializedCalls() Option {
	return optionFunc(func(c *serverConfig) { c.dispatcher.SerializeOutboundCalls = true })
}

// WithAsyncQueueSize bounds the per-connection FIFO queue used by CallAsync when
// WithSerializedCalls is set (default 64). Enqueuing beyond it returns
// ocppj.ErrQueueFull.
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

// WithInstanceID sets the CSMS instance identifier (multi-instance deployments).
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

// WithLenientSchema enables lenient JSON-Schema validation: structurally broken
// messages are rejected, while benign violations are logged and passed, and
// enum case mismatches are normalized to canonical values. Last-wins with
// WithStrictSchema / WithTolerantSchema.
func WithLenientSchema() Option {
	return optionFunc(func(c *serverConfig) {
		c.dispatcher.SchemaMode = dispatcher.SchemaModeLenient
	})
}

// WithAuthenticator sets the connection authenticator (default auth.None).
func WithAuthenticator(a auth.Authenticator) Option {
	return optionFunc(func(c *serverConfig) { c.auth = a })
}

// WithConnectionRegistry sets the connection registry (default in-memory).
func WithConnectionRegistry(r storage.ConnectionRegistry) Option {
	return optionFunc(func(c *serverConfig) { c.connReg = r })
}

// WithMessageRouter sets the inter-instance message router (default no-op).
func WithMessageRouter(r storage.MessageRouter) Option {
	return optionFunc(func(c *serverConfig) { c.router = r })
}

// WithTransactionStore sets the transaction store (default in-memory).
func WithTransactionStore(s storage.TransactionStore) Option {
	return optionFunc(func(c *serverConfig) { c.txStore = s })
}

// WithConfigStore sets the configuration store (default in-memory).
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
