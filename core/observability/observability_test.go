package observability_test

import (
	"context"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/observability"
	"github.com/stretchr/testify/require"
)

func TestNoOpMetrics(t *testing.T) {
	var m observability.Metrics = observability.NoOp{}
	m.ConnectionCount("1.6", 1)
	m.CallStarted("1.6", "Heartbeat", "inbound")
	m.CallCompleted("1.6", "Heartbeat", "inbound", time.Millisecond, "ok")
	m.PendingCallCount("1.6", 1)
	m.SchemaValidationFailure("1.6", "Authorize", "request")
}

func TestLogAttrs(t *testing.T) {
	require.Equal(t, "cp_id", observability.AttrCPID)
}

func TestTracer_NoopSafe(t *testing.T) {
	tr := observability.NewTracer(nil)
	_, span := tr.Start(context.Background(), "test")
	span.End()
}
