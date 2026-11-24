package dispatcher

import (
	"time"

	"github.com/shiv3/gocpp/core/observability"
)

type metricsBridge struct {
	m       observability.Metrics
	version string
	// known reports whether an action a charge point sent is one the server
	// knows; nil means every action is. Unknown inbound actions are reported
	// under OtherAction so a misbehaving or hostile charger cannot create
	// unbounded metric series by inventing action names.
	known func(action string) bool
}

// OtherAction is the metric label that stands for every action the server does
// not know (see MetricsHookBounded).
const OtherAction = "other"

// MetricsHookFrom adapts an observability.Metrics to a MetricsHook bound to a
// specific OCPP version.
func MetricsHookFrom(m observability.Metrics, version string) MetricsHook {
	return &metricsBridge{m: m, version: version}
}

// MetricsHookBounded is MetricsHookFrom with a bound on the action label: an
// inbound action for which known returns false is reported as OtherAction. Use
// the set of actions of the version plus the registered handlers. Outbound
// actions are chosen by the server's own code and pass through unchanged.
//
// Experimental: this API may change before v1.0.
func MetricsHookBounded(m observability.Metrics, version string, known func(action string) bool) MetricsHook {
	return &metricsBridge{m: m, version: version, known: known}
}

// label bounds an inbound action.
func (b *metricsBridge) label(action, direction string) string {
	if b.known == nil || direction == "outbound" || b.known(action) {
		return action
	}
	return OtherAction
}

func (b *metricsBridge) ConnectionOpened() { b.m.ConnectionCount(b.version, 1) }
func (b *metricsBridge) ConnectionClosed() { b.m.ConnectionCount(b.version, -1) }
func (b *metricsBridge) CallStarted(action, direction string) {
	b.m.CallStarted(b.version, b.label(action, direction), direction)
}
func (b *metricsBridge) CallCompleted(action, direction string, dur time.Duration, status string) {
	b.m.CallCompleted(b.version, b.label(action, direction), direction, dur, status)
}
func (b *metricsBridge) PendingDelta(delta int) { b.m.PendingCallCount(b.version, delta) }
func (b *metricsBridge) SchemaValidationFailure(version, action, direction string) {
	b.m.SchemaValidationFailure(version, b.label(action, "inbound"), direction)
}

func (b *metricsBridge) SchemaSoftViolation(version, action, kind, keyword string) {
	if m, ok := b.m.(observability.SchemaSoftViolationMetrics); ok {
		m.SchemaSoftViolation(version, b.label(action, "inbound"), kind, keyword)
	}
}

func (b *metricsBridge) OutboundQueueDepth(version string, depth int) {
	if m, ok := b.m.(observability.ConnectionMetrics); ok {
		m.OutboundQueueDepth(version, depth)
	}
}
func (b *metricsBridge) PingLatency(version string, d time.Duration) {
	if m, ok := b.m.(observability.ConnectionMetrics); ok {
		m.PingLatency(version, d)
	}
}

func (b *metricsBridge) BadFrame(version, reason string) {
	if m, ok := b.m.(observability.BadFrameMetrics); ok {
		m.BadFrame(version, reason)
	}
}

func (b *metricsBridge) InboundRateLimited(version, outcome string) {
	if m, ok := b.m.(observability.RateLimitMetrics); ok {
		m.InboundRateLimited(version, outcome)
	}
}
