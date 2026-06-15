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
	registry          *schema.Registry
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
