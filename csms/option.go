package csms

import (
	"log/slog"
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
	addr              string
	path              string
	instanceID        string
	registry          *schema.Registry
	strictSchema      bool

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
		dispatcher:   dispatcher.DefaultConfig(),
		subProtocols: []string{"ocpp1.6"},
		path:         "/ocpp/",
		auth:         auth.None{},
		connReg:      memory.NewConnectionRegistry(),
		router:       memory.NewRouter(),
		txStore:      memory.NewTransactionStore(),
		cfgStore:     memory.NewConfigStore(),
		metrics:      observability.NoOp{},
	}
}

// Option configures a Server.
type Option interface{ apply(*serverConfig) }

type optionFunc func(*serverConfig)

func (f optionFunc) apply(c *serverConfig) { f(c) }

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

// WithSchemaRegistry sets the schema registry used for first-layer validation.
func WithSchemaRegistry(r *schema.Registry) Option {
	return optionFunc(func(c *serverConfig) { c.registry = r })
}

// WithStrictSchema controls whether schema validation failures reject the message
// (true) or only log a warning (false). Default false (spec OQ-19).
func WithStrictSchema(strict bool) Option {
	return optionFunc(func(c *serverConfig) { c.strictSchema = strict })
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
