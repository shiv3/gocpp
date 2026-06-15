// Package dispatcher implements the version-agnostic OCPP connection lifecycle,
// pending-call tracking, and concurrency control. See spec §3.
package dispatcher

import (
	"log/slog"
	"time"
)

// Config controls a single connection's behavior.
type Config struct {
	CallTimeout           time.Duration
	WriteTimeout          time.Duration
	OutboundQueueSize     int
	MaxConcurrentHandlers int64
	Logger                *slog.Logger
	Metrics               MetricsHook
	// SchemaValidate optionally validates an inbound payload. kind is "request".
	// Returning an error rejects the message. nil disables validation.
	SchemaValidate func(action, kind string, payload []byte) error
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
	}
}
