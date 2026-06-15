package csms

import (
	"log/slog"
	"time"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/schema"
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
}

func defaultServerConfig() serverConfig {
	return serverConfig{
		dispatcher:   dispatcher.DefaultConfig(),
		subProtocols: []string{"ocpp1.6"},
		path:         "/ocpp/",
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
