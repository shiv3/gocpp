// Package prom implements observability.Metrics using Prometheus.
package prom

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shiv3/gocpp/core/observability"
)

type metrics struct {
	connections *prometheus.GaugeVec
	pending     *prometheus.GaugeVec
	calls       *prometheus.CounterVec
	callDur     *prometheus.HistogramVec
	schemaFails *prometheus.CounterVec
	queueDepth  *prometheus.HistogramVec
	pingLatency *prometheus.HistogramVec
	softViol    *prometheus.CounterVec
}

// New creates a Prometheus-backed Metrics and registers collectors with reg.
func New(reg prometheus.Registerer) observability.Metrics {
	m := &metrics{
		connections: prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gocpp_connections", Help: "active connections"}, []string{"version"}),
		pending:     prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gocpp_pending_calls", Help: "in-flight calls"}, []string{"version"}),
		calls:       prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gocpp_calls_total", Help: "calls completed"}, []string{"version", "action", "direction", "status"}),
		callDur:     prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "gocpp_call_duration_seconds", Help: "call duration"}, []string{"version", "action", "direction"}),
		schemaFails: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gocpp_schema_failures_total", Help: "schema validation failures"}, []string{"version", "action", "direction"}),
		queueDepth:  prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "gocpp_outbound_queue_depth", Help: "senders waiting for or holding the connection write lock (1 = uncontended write), sampled at each send", Buckets: []float64{0, 1, 2, 4, 8, 16, 32, 64, 128, 256}}, []string{"version"}),
		pingLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "gocpp_ping_latency_seconds", Help: "WebSocket ping round-trip time"}, []string{"version"}),
		softViol:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gocpp_schema_soft_violations_total", Help: "lenient-mode schema violations that were repaired or tolerated"}, []string{"version", "action", "kind", "keyword"}),
	}
	reg.MustRegister(m.connections, m.pending, m.calls, m.callDur, m.schemaFails, m.queueDepth, m.pingLatency, m.softViol)
	return m
}

func (m *metrics) ConnectionCount(version string, delta int) {
	m.connections.WithLabelValues(version).Add(float64(delta))
}
func (m *metrics) CallStarted(string, string, string) {}
func (m *metrics) CallCompleted(version, action, direction string, dur time.Duration, status string) {
	m.calls.WithLabelValues(version, action, direction, status).Inc()
	m.callDur.WithLabelValues(version, action, direction).Observe(dur.Seconds())
}
func (m *metrics) PendingCallCount(version string, delta int) {
	m.pending.WithLabelValues(version).Add(float64(delta))
}
func (m *metrics) SchemaValidationFailure(version, action, direction string) {
	m.schemaFails.WithLabelValues(version, action, direction).Inc()
}

func (m *metrics) OutboundQueueDepth(version string, depth int) {
	m.queueDepth.WithLabelValues(version).Observe(float64(depth))
}
func (m *metrics) SchemaSoftViolation(version, action, kind, keyword string) {
	m.softViol.WithLabelValues(version, action, kind, keyword).Inc()
}
func (m *metrics) PingLatency(version string, d time.Duration) {
	m.pingLatency.WithLabelValues(version).Observe(d.Seconds())
}
