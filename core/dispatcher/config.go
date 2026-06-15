// Package dispatcher implements the version-agnostic OCPP connection lifecycle,
// pending-call tracking, and concurrency control. See spec §3.
package dispatcher

import (
	"log/slog"
	"time"

	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/ocppj"
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
	// Returning an error rejects the message. nil disables validation.
	SchemaValidate func(version ocppj.Version, action, kind string, payload []byte) error
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
