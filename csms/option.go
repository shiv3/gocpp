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
	dispatcher        dispatcher.Config
	subProtocols      []string
	heartbeatInterval time.Duration
	pingInterval      time.Duration
	path              string
	cpIDExtractor     CPIDExtractor
	instanceID        string
	registry          *schema.Registry
	duplicatePolicy   DuplicatePolicy

	globalConcurrencyLimit int

	auth           auth.Authenticator
	connReg        storage.ConnectionRegistry
	router         storage.MessageRouter
	txStore        storage.TransactionStore
	cfgStore       storage.ConfigStore
	metrics        observability.Metrics
	tracerProvider trace.TracerProvider
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

// WithHeartbeatInterval sets the application-layer OCPP Heartbeat interval.
func WithHeartbeatInterval(d time.Duration) Option {
	return optionFunc(func(c *serverConfig) { c.heartbeatInterval = d })
}

// WithWebSocketPingInterval sets the transport ping interval.
func WithWebSocketPingInterval(d time.Duration) Option {
	return optionFunc(func(c *serverConfig) { c.pingInterval = d })
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
