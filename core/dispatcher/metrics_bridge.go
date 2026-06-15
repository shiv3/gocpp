package dispatcher

import (
	"time"

	"github.com/shiv3/gocpp/core/observability"
)

type metricsBridge struct {
	m       observability.Metrics
	version string
}

// MetricsHookFrom adapts an observability.Metrics to a MetricsHook bound to a
// specific OCPP version.
func MetricsHookFrom(m observability.Metrics, version string) MetricsHook {
	return &metricsBridge{m: m, version: version}
}

func (b *metricsBridge) ConnectionOpened() { b.m.ConnectionCount(b.version, 1) }
func (b *metricsBridge) ConnectionClosed() { b.m.ConnectionCount(b.version, -1) }
func (b *metricsBridge) CallStarted(action, direction string) {
	b.m.CallStarted(b.version, action, direction)
}
func (b *metricsBridge) CallCompleted(action, direction string, dur time.Duration, status string) {
	b.m.CallCompleted(b.version, action, direction, dur, status)
}
func (b *metricsBridge) PendingDelta(delta int) { b.m.PendingCallCount(b.version, delta) }
