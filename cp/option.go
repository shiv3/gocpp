package cp

import (
	"crypto/tls"
	"log/slog"
	"net/http"
	"time"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/core/transport"
)

type clientConfig struct {
	dispatcher           dispatcher.Config
	subProtocols         []string
	compressionMode      transport.CompressionMode
	heartbeatInterval    time.Duration
	basicAuthUser        string
	basicAuthPass        string
	httpHeader           http.Header
	tlsConfig            *tls.Config
	offlineQueueCapacity int
	retryInFlight        bool
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

// WithSerializedCalls limits outbound OCPP Calls to one outstanding request.
func WithSerializedCalls() Option {
	return optionFunc(func(c *clientConfig) { c.dispatcher.SerializeOutboundCalls = true })
}

// WithAsyncQueueSize bounds the per-connection FIFO queue used by CallAsync when
// WithSerializedCalls is set (default 64). Enqueuing beyond it returns
// ocppj.ErrQueueFull.
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

// WithLenientSchema enables lenient JSON-Schema validation: structurally broken
// messages are rejected, while benign violations are logged and passed, and
// enum case mismatches are normalized to canonical values. Last-wins with
// WithStrictSchema / WithTolerantSchema.
func WithLenientSchema() Option {
	return optionFunc(func(c *clientConfig) {
		c.dispatcher.SchemaMode = dispatcher.SchemaModeLenient
	})
}
