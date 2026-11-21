package main

import (
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shiv3/gocpp/gateway/routepolicy"
)

// routeMetrics implements routepolicy.Metrics (design 5.4).
type routeMetrics struct {
	decisions *prometheus.CounterVec
	fallback  *prometheus.CounterVec
	suspended *prometheus.GaugeVec
	revision  prometheus.Gauge
	missing   prometheus.Gauge
	rejected  prometheus.Counter

	known map[string]bool // rules with a suspended series, to clear them on the next revision
}

func newRouteMetrics(reg prometheus.Registerer) *routeMetrics {
	m := &routeMetrics{
		decisions: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gocpp_gateway_route_decisions_total", Help: "routing decisions by matching rule, chosen pool and policy revision"}, []string{"rule", "pool", "rev"}),
		fallback:  prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gocpp_gateway_route_fallback_total", Help: "messages sent to the fallback pool"}, []string{"rule", "reason"}),
		suspended: prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gocpp_gateway_route_suspended", Help: "1 while a rule is suspended on this gateway"}, []string{"rule"}),
		revision:  prometheus.NewGauge(prometheus.GaugeOpts{Name: "gocpp_gateway_route_policy_revision", Help: "routing policy revision in force"}),
		missing:   prometheus.NewGauge(prometheus.GaugeOpts{Name: "gocpp_gateway_route_policy_missing", Help: "1 while no routing policy is loaded (everything goes to the default pool)"}),
		rejected:  prometheus.NewCounter(prometheus.CounterOpts{Name: "gocpp_gateway_route_policy_rejected_total", Help: "invalid routing policies refused"}),
		known:     map[string]bool{},
	}
	reg.MustRegister(m.decisions, m.fallback, m.suspended, m.revision, m.missing, m.rejected)
	return m
}

func (m *routeMetrics) Decision(rule, pool string, rev uint64) {
	m.decisions.WithLabelValues(rule, pool, strconv.FormatUint(rev, 10)).Inc()
}
func (m *routeMetrics) Fallback(rule, reason string) { m.fallback.WithLabelValues(rule, reason).Inc() }
func (m *routeMetrics) Suspended(rule string, on bool) {
	v := 0.0
	if on {
		v = 1
	}
	m.suspended.WithLabelValues(rule).Set(v)
}
func (m *routeMetrics) PolicyApplied(rev uint64) { m.revision.Set(float64(rev)) }
func (m *routeMetrics) PolicyRejected()          { m.rejected.Inc() }
func (m *routeMetrics) PolicyMissing(missing bool) {
	v := 0.0
	if missing {
		v = 1
	}
	m.missing.Set(v)
}

// heldCollector exports gocpp_gateway_route_held_sessions.
type heldCollector struct{ e *routepolicy.Engine }

var heldDesc = prometheus.NewDesc("gocpp_gateway_route_held_sessions", "busy sessions held on their previous routing", []string{"rule"}, nil)

func (heldCollector) Describe(ch chan<- *prometheus.Desc) { ch <- heldDesc }
func (c heldCollector) Collect(ch chan<- prometheus.Metric) {
	for rule, n := range c.e.HeldSessions() {
		ch <- prometheus.MustNewConstMetric(heldDesc, prometheus.GaugeValue, float64(n), rule)
	}
}

func joinRules(rules []string) string { return strings.Join(rules, ",") }
