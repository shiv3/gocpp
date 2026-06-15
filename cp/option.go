package cp

import (
	"log/slog"
	"time"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/schema"
)

type clientConfig struct {
	dispatcher        dispatcher.Config
	subProtocols      []string
	heartbeatInterval time.Duration
	pingInterval      time.Duration
	registry          *schema.Registry
	strictSchema      bool
}

func defaultClientConfig() clientConfig {
	return clientConfig{
		dispatcher:   dispatcher.DefaultConfig(),
		subProtocols: []string{"ocpp1.6"},
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

// WithSchemaRegistry sets the schema registry used for first-layer validation.
func WithSchemaRegistry(r *schema.Registry) Option {
	return optionFunc(func(c *clientConfig) { c.registry = r })
}

// WithStrictSchema controls whether schema validation failures reject the message
// (true) or only log a warning (false). Default false (spec OQ-19).
func WithStrictSchema(strict bool) Option {
	return optionFunc(func(c *clientConfig) { c.strictSchema = strict })
}
