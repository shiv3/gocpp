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

func TestProm_SchemaSoftViolation(t *testing.T) {
	reg := prometheus.NewRegistry()
	sm := prom.New(reg).(observability.SchemaSoftViolationMetrics)
	sm.SchemaSoftViolation("1.6", "StartTransaction", "request", "format")
	sm.SchemaSoftViolation("1.6", "StartTransaction", "request", "format")
	sm.SchemaSoftViolation("1.6", "StartTransaction", "request", "enum")
	require.Equal(t, 2, testutil.CollectAndCount(reg, "gocpp_schema_soft_violations_total"))
}

func TestProm_SessionMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	sm := prom.New(reg).(observability.SessionMetrics)
	sm.SessionClosed("ocpp1.6", 90*time.Second, "1012", "drain")
	sm.SessionClosed("ocpp1.6", time.Second, "1006", "dropped")
	sm.UpgradeResult("ok")
	sm.UpgradeResult("rate_limited")
	require.Equal(t, 2, testutil.CollectAndCount(reg, "gocpp_closes_total"))
	require.Equal(t, 1, testutil.CollectAndCount(reg, "gocpp_session_duration_seconds"))
	require.Equal(t, 2, testutil.CollectAndCount(reg, "gocpp_accepts_total"))
}

func TestProm_TenantAndEdgeMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := prom.New(reg)
	tm := m.(observability.TenantMetrics)
	tm.TenantSessions("a", 1)
	tm.TenantSessions("a", 1)
	tm.TenantSessions("a", -1)
	tm.TenantRejected("a", "sessions")
	tm.TenantRejected("a", "messages")
	tm.TenantRejected("a", "inflight")
	m.(observability.TenantInflightMetrics).TenantInflight("a", 2)
	m.(observability.EdgeMetrics).EdgeResponse("1.6", "Heartbeat")
	require.Equal(t, 1, testutil.CollectAndCount(reg, "gocpp_tenant_sessions"))
	require.Equal(t, 3, testutil.CollectAndCount(reg, "gocpp_tenant_rejected_total"))
	require.Equal(t, 1, testutil.CollectAndCount(reg, "gocpp_tenant_inflight"))
	require.Equal(t, 1, testutil.CollectAndCount(reg, "gocpp_edge_responses_total"))
}

func TestProm_BadFrameMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	bm := prom.New(reg).(observability.BadFrameMetrics)
	bm.BadFrame("1.6", "parse")
	bm.BadFrame("1.6", "parse")
	n := testutil.CollectAndCount(reg, "gocpp_bad_frames_total")
	require.Equal(t, 1, n)
}

func TestProm_RateLimitMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	rm := prom.New(reg).(observability.RateLimitMetrics)
	rm.InboundRateLimited("1.6", "rejected")
	require.Equal(t, 1, testutil.CollectAndCount(reg, "gocpp_inbound_rate_limited_total"))
}
