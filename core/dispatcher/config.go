// Package dispatcher implements the version-agnostic OCPP connection lifecycle,
// pending-call tracking, and concurrency control. See spec §3.
package dispatcher

import (
	"log/slog"
	"time"

	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/ocppj"
)

// SchemaMode controls how inbound JSON Schema validation failures are handled.
type SchemaMode int

const (
	SchemaModeOff SchemaMode = iota
	SchemaModeTolerant
	SchemaModeStrict
)

// Config controls a single connection's behavior.
type Config struct {
	CallTimeout           time.Duration
	WriteTimeout          time.Duration
	OutboundQueueSize     int
	MaxConcurrentHandlers int64
	Logger                *slog.Logger
	Metrics               MetricsHook
	Tracer                observability.Tracer
	// SchemaValidate optionally validates an inbound payload for the given version.
	// nil disables validation. SchemaMode controls how returned errors are handled.
	SchemaValidate func(version ocppj.Version, action, kind string, payload []byte) error
	SchemaMode     SchemaMode
}

// DefaultConfig returns production-sane defaults.
func DefaultConfig() Config {
	return Config{
		CallTimeout:           30 * time.Second,
		WriteTimeout:          10 * time.Second,
		OutboundQueueSize:     64,
		MaxConcurrentHandlers: 16,
		Logger:                slog.Default(),
		Metrics:               NoopMetrics,
		Tracer:                observability.NewTracer(nil),
	}
}
