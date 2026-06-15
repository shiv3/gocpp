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
}

// New creates a Prometheus-backed Metrics and registers collectors with reg.
func New(reg prometheus.Registerer) observability.Metrics {
	m := &metrics{
		connections: prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gocpp_connections", Help: "active connections"}, []string{"version"}),
		pending:     prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gocpp_pending_calls", Help: "in-flight calls"}, []string{"version"}),
		calls:       prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gocpp_calls_total", Help: "calls completed"}, []string{"version", "action", "direction", "status"}),
		callDur:     prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "gocpp_call_duration_seconds", Help: "call duration"}, []string{"version", "action", "direction"}),
		schemaFails: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gocpp_schema_failures_total", Help: "schema validation failures"}, []string{"version", "action", "direction"}),
	}
	reg.MustRegister(m.connections, m.pending, m.calls, m.callDur, m.schemaFails)
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
