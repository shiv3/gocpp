package dispatcher

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type recordingMetrics struct {
	conns       int
	calls       int
	schemaFails int
}

func (r *recordingMetrics) ConnectionCount(string, int)        { r.conns++ }
func (r *recordingMetrics) CallStarted(string, string, string) {}
func (r *recordingMetrics) CallCompleted(string, string, string, time.Duration, string) {
	r.calls++
}
func (r *recordingMetrics) PendingCallCount(string, int)                   {}
func (r *recordingMetrics) SchemaValidationFailure(string, string, string) { r.schemaFails++ }

func TestMetricsBridge(t *testing.T) {
	rm := &recordingMetrics{}
	hook := MetricsHookFrom(rm, "1.6")
	hook.ConnectionOpened()
	hook.CallCompleted("Heartbeat", "inbound", time.Millisecond, "ok")
	hook.(schemaValidationMetricsHook).SchemaValidationFailure("1.6", "Heartbeat", "request")
	require.Equal(t, 1, rm.conns)
	require.Equal(t, 1, rm.calls)
	require.Equal(t, 1, rm.schemaFails)
}
