package prom_test

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
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
