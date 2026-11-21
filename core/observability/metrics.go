// Package observability defines logging, tracing, and metrics abstractions.
package observability

import "time"

// Metrics receives library metrics. Default is NoOp; prom/otel subpackages provide
// real implementations.
type Metrics interface {
	ConnectionCount(version string, delta int)
	CallStarted(version, action, direction string)
	CallCompleted(version, action, direction string, dur time.Duration, status string)
	PendingCallCount(version string, delta int)
	SchemaValidationFailure(version, action, direction string)
}

// NoOp is a Metrics implementation that does nothing.
type NoOp struct{}

func (NoOp) ConnectionCount(string, int)                                 {}
func (NoOp) CallStarted(string, string, string)                          {}
func (NoOp) CallCompleted(string, string, string, time.Duration, string) {}
func (NoOp) PendingCallCount(string, int)                                {}
func (NoOp) SchemaValidationFailure(string, string, string)              {}

// SchemaSoftViolationMetrics is an optional extension of Metrics. In lenient
// schema mode each repaired or tolerated violation is reported once, with the
// schema keyword that triggered it (for example "enum", "format", "type").
type SchemaSoftViolationMetrics interface {
	SchemaSoftViolation(version, action, kind, keyword string)
}

// ConnectionMetrics is an optional extension of Metrics. A Metrics value that
// also implements it receives per-connection health samples; implementations
// of Metrics that do not are unaffected.
type ConnectionMetrics interface {
	// OutboundQueueDepth samples the number of senders that are waiting for, or
	// holding, the connection's write lock (so 1 means an uncontended write),
	// taken each time a frame is sent.
	OutboundQueueDepth(version string, depth int)
	// PingLatency reports one WebSocket ping round trip (ping sent until pong).
	PingLatency(version string, d time.Duration)
}
