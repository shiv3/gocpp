package dispatcher

import "time"

// MetricsHook receives connection/call lifecycle events. The default is a no-op;
// Phase 4 wires Prometheus/OTel implementations.
type MetricsHook interface {
	ConnectionOpened()
	ConnectionClosed()
	CallStarted(action, direction string)
	CallCompleted(action, direction string, dur time.Duration, status string)
	PendingDelta(delta int)
}

type noopMetrics struct{}

func (noopMetrics) ConnectionOpened()                                   {}
func (noopMetrics) ConnectionClosed()                                   {}
func (noopMetrics) CallStarted(string, string)                          {}
func (noopMetrics) CallCompleted(string, string, time.Duration, string) {}
func (noopMetrics) PendingDelta(int)                                    {}
func (noopMetrics) SchemaValidationFailure(string, string, string)      {}

// NoopMetrics is the default metrics hook.
var NoopMetrics MetricsHook = noopMetrics{}

type schemaValidationMetricsHook interface {
	SchemaValidationFailure(version, action, direction string)
}

// schemaSoftViolationMetricsHook is an optional MetricsHook extension that
// receives one event per soft schema violation keyword in lenient mode.
type schemaSoftViolationMetricsHook interface {
	SchemaSoftViolation(version, action, kind, keyword string)
}
