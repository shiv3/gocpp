package prom_test

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/observability/metrics/prom"
	"github.com/stretchr/testify/require"
)

func TestProm_CallCompleted(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := prom.New(reg)
	m.CallCompleted("1.6", "Heartbeat", "inbound", 5*time.Millisecond, "ok")
	m.ConnectionCount("1.6", 1)

	count := testutil.CollectAndCount(reg, "gocpp_calls_total")
	require.GreaterOrEqual(t, count, 1)
}

func TestProm_ConnectionMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	cm := prom.New(reg).(observability.ConnectionMetrics)
	cm.OutboundQueueDepth("1.6", 4)
	cm.PingLatency("1.6", 2*time.Millisecond)
	require.Equal(t, 1, testutil.CollectAndCount(reg, "gocpp_outbound_queue_depth"))
	require.Equal(t, 1, testutil.CollectAndCount(reg, "gocpp_ping_latency_seconds"))
}
