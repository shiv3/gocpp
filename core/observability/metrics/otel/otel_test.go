package otel_test

import (
	"context"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/observability"
	otelmetrics "github.com/shiv3/gocpp/core/observability/metrics/otel"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestOTel_RecordsInstruments(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	m, err := otelmetrics.New(mp)
	require.NoError(t, err)

	m.ConnectionCount("1.6", 1)
	m.PendingCallCount("1.6", 1)
	m.CallCompleted("1.6", "Heartbeat", "inbound", 5*time.Millisecond, "ok")
	m.SchemaValidationFailure("1.6", "BootNotification", "inbound")

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	got := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		for _, mt := range sm.Metrics {
			got[mt.Name] = true
		}
	}
	for _, want := range []string{
		"gocpp.connections", "gocpp.pending_calls",
		"gocpp.calls", "gocpp.call.duration", "gocpp.schema_failures",
	} {
		require.Truef(t, got[want], "expected metric %q to be recorded", want)
	}
}

func TestOTel_NilProviderIsNoOp(t *testing.T) {
	m, err := otelmetrics.New(nil)
	require.NoError(t, err)
	require.IsType(t, observability.NoOp{}, m)
	// must not panic
	m.CallCompleted("1.6", "Heartbeat", "inbound", time.Millisecond, "ok")
}
