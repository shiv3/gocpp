// Package otel implements observability.Metrics on top of the OpenTelemetry
// metrics API. Instruments are created from the supplied MeterProvider; the
// application owns the provider (and thus the OTLP/stdout exporter wiring), so
// this package depends only on the OTel metric API, not the SDK.
package otel

import (
	"context"
	"time"

	"github.com/shiv3/gocpp/core/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const scopeName = "github.com/shiv3/gocpp"

type metrics struct {
	connections metric.Int64UpDownCounter
	pending     metric.Int64UpDownCounter
	calls       metric.Int64Counter
	callDur     metric.Float64Histogram
	schemaFails metric.Int64Counter
}

// New builds an observability.Metrics backed by OTel instruments from mp.
// A nil provider falls back to the global MeterProvider.
func New(mp metric.MeterProvider) (observability.Metrics, error) {
	if mp == nil {
		return observability.NoOp{}, nil
	}
	m := mp.Meter(scopeName)

	conns, err := m.Int64UpDownCounter("gocpp.connections",
		metric.WithDescription("active connections"))
	if err != nil {
		return nil, err
	}
	pending, err := m.Int64UpDownCounter("gocpp.pending_calls",
		metric.WithDescription("in-flight calls awaiting a response"))
	if err != nil {
		return nil, err
	}
	calls, err := m.Int64Counter("gocpp.calls",
		metric.WithDescription("OCPP calls completed"))
	if err != nil {
		return nil, err
	}
	callDur, err := m.Float64Histogram("gocpp.call.duration",
		metric.WithDescription("OCPP call duration"),
		metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	schemaFails, err := m.Int64Counter("gocpp.schema_failures",
		metric.WithDescription("schema validation failures"))
	if err != nil {
		return nil, err
	}

	return &metrics{
		connections: conns,
		pending:     pending,
		calls:       calls,
		callDur:     callDur,
		schemaFails: schemaFails,
	}, nil
}

func (m *metrics) ConnectionCount(version string, delta int) {
	m.connections.Add(context.Background(), int64(delta),
		metric.WithAttributes(attribute.String("version", version)))
}

func (m *metrics) CallStarted(string, string, string) {}

func (m *metrics) CallCompleted(version, action, direction string, dur time.Duration, status string) {
	attrs := metric.WithAttributes(
		attribute.String("version", version),
		attribute.String("action", action),
		attribute.String("direction", direction),
		attribute.String("status", status),
	)
	m.calls.Add(context.Background(), 1, attrs)
	m.callDur.Record(context.Background(), dur.Seconds(), attrs)
}

func (m *metrics) PendingCallCount(version string, delta int) {
	m.pending.Add(context.Background(), int64(delta),
		metric.WithAttributes(attribute.String("version", version)))
}

func (m *metrics) SchemaValidationFailure(version, action, direction string) {
	m.schemaFails.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("version", version),
		attribute.String("action", action),
		attribute.String("direction", direction),
	))
}
