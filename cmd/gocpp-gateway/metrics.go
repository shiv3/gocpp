package main

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// linkMetrics implements linkgrpc.Metrics (design 5.4: labels pool,
// backend_version; gateway_id and gateway_version are constant labels).
type linkMetrics struct {
	attach      *prometheus.CounterVec
	upstream    *prometheus.CounterVec
	upstreamSec *prometheus.HistogramVec
	downstream  *prometheus.CounterVec
	unsupported *prometheus.CounterVec
}

func newLinkMetrics(reg prometheus.Registerer) *linkMetrics {
	m := &linkMetrics{
		attach: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gocpp_gateway_link_attach_total", Help: "backend attach attempts"}, []string{"proto", "result"}),
		upstream: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gocpp_gateway_upstream_requests_total", Help: "inbound CALLs forwarded to a backend"},
			[]string{"pool", "backend_version", "action", "outcome"}),
		upstreamSec: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "gocpp_gateway_upstream_seconds", Help: "time a backend took to answer", Buckets: prometheus.ExponentialBuckets(0.0005, 2, 16)},
			[]string{"pool", "backend_version", "action"}),
		downstream: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gocpp_gateway_downstream_calls_total", Help: "CSMS calls served for backends"},
			[]string{"caller_pool", "caller_version", "action", "outcome"}),
		unsupported: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gocpp_gateway_link_unsupported_total", Help: "link frames or ops the peer or this gateway did not know"},
			[]string{"direction", "kind"}),
	}
	reg.MustRegister(m.attach, m.upstream, m.upstreamSec, m.downstream, m.unsupported)
	return m
}

func (m *linkMetrics) AttachResult(proto uint32, result string) {
	m.attach.WithLabelValues(itoa(proto), result).Inc()
}

func (m *linkMetrics) UpstreamRequest(pool, ver, action, outcome string, d time.Duration) {
	m.upstream.WithLabelValues(pool, ver, action, outcome).Inc()
	m.upstreamSec.WithLabelValues(pool, ver, action).Observe(d.Seconds())
}

func (m *linkMetrics) DownstreamCall(pool, ver, action, outcome string) {
	m.downstream.WithLabelValues(pool, ver, action, outcome).Inc()
}

func (m *linkMetrics) Unsupported(direction, what string) {
	m.unsupported.WithLabelValues(direction, what).Inc()
}

func itoa(v uint32) string {
	if v == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
