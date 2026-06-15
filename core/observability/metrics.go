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
