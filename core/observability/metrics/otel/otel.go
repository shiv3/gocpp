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
	queueDepth  metric.Int64Histogram
	pingLatency metric.Float64Histogram
	softViol    metric.Int64Counter
	badFrames   metric.Int64Counter
	rateLimited metric.Int64Counter
	sessDur     metric.Float64Histogram
	closes      metric.Int64Counter
	accepts     metric.Int64Counter
	tenantSess  metric.Int64UpDownCounter
	tenantRej   metric.Int64Counter
	tenantInfl  metric.Int64UpDownCounter
	edgeResp    metric.Int64Counter
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
	queueDepth, err := m.Int64Histogram("gocpp.outbound_queue.depth",
		metric.WithDescription("senders waiting for or holding the connection write lock (1 = uncontended write), sampled at each send"))
	if err != nil {
		return nil, err
	}
	pingLatency, err := m.Float64Histogram("gocpp.ping.latency",
		metric.WithDescription("WebSocket ping round-trip time"),
		metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}

	softViol, err := m.Int64Counter("gocpp.schema_soft_violations",
		metric.WithDescription("lenient-mode schema violations that were repaired or tolerated"))
	if err != nil {
		return nil, err
	}

	badFrames, err := m.Int64Counter("gocpp.bad_frames",
		metric.WithDescription("inbound frames that were malformed or rejected for their content, by reason"))
	if err != nil {
		return nil, err
	}

	rateLimited, err := m.Int64Counter("gocpp.inbound_rate_limited",
		metric.WithDescription("inbound CALLs and SENDs refused by the per-connection rate limit, by outcome"))
	if err != nil {
		return nil, err
	}

	sessDur, err := m.Float64Histogram("gocpp.session.duration",
		metric.WithDescription("connection lifetime, observed at close"), metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	closes, err := m.Int64Counter("gocpp.closes",
		metric.WithDescription("connections closed, by WebSocket close code and cause"))
	if err != nil {
		return nil, err
	}
	accepts, err := m.Int64Counter("gocpp.accepts",
		metric.WithDescription("upgrade attempts by result"))
	if err != nil {
		return nil, err
	}

	tenantSess, err := m.Int64UpDownCounter("gocpp.tenant.sessions",
		metric.WithDescription("sessions per tenant (csms.WithTenantLimits)"))
	if err != nil {
		return nil, err
	}
	tenantInfl, err := m.Int64UpDownCounter("gocpp.tenant.inflight",
		metric.WithDescription("inbound messages in flight per tenant (csms.WithTenantLimits)"))
	if err != nil {
		return nil, err
	}
	tenantRej, err := m.Int64Counter("gocpp.tenant.rejected",
		metric.WithDescription("upgrades and messages refused by a per-tenant limit"))
	if err != nil {
		return nil, err
	}
	edgeResp, err := m.Int64Counter("gocpp.edge.responses",
		metric.WithDescription("CALLs answered at the gateway without a backend hop"))
	if err != nil {
		return nil, err
	}

	return &metrics{
		tenantSess:  tenantSess,
		tenantRej:   tenantRej,
		tenantInfl:  tenantInfl,
		edgeResp:    edgeResp,
		sessDur:     sessDur,
		closes:      closes,
		accepts:     accepts,
		connections: conns,
		pending:     pending,
		calls:       calls,
		callDur:     callDur,
		schemaFails: schemaFails,
		queueDepth:  queueDepth,
		pingLatency: pingLatency,
		softViol:    softViol,
		badFrames:   badFrames,
		rateLimited: rateLimited,
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

func (m *metrics) OutboundQueueDepth(version string, depth int) {
	m.queueDepth.Record(context.Background(), int64(depth),
		metric.WithAttributes(attribute.String("version", version)))
}

func (m *metrics) PingLatency(version string, d time.Duration) {
	m.pingLatency.Record(context.Background(), d.Seconds(),
		metric.WithAttributes(attribute.String("version", version)))
}

func (m *metrics) SchemaSoftViolation(version, action, kind, keyword string) {
	m.softViol.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("version", version),
		attribute.String("action", action),
		attribute.String("kind", kind),
		attribute.String("keyword", keyword),
	))
}

func (m *metrics) SessionClosed(version string, d time.Duration, code, cause string) {
	m.sessDur.Record(context.Background(), d.Seconds(), metric.WithAttributes(attribute.String("version", version)))
	m.closes.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("version", version), attribute.String("code", code), attribute.String("cause", cause)))
}

func (m *metrics) UpgradeResult(result string) {
	m.accepts.Add(context.Background(), 1, metric.WithAttributes(attribute.String("result", result)))
}

func (m *metrics) TenantSessions(tenant string, delta int) {
	m.tenantSess.Add(context.Background(), int64(delta), metric.WithAttributes(attribute.String("tenant", tenant)))
}

func (m *metrics) TenantRejected(tenant, kind string) {
	m.tenantRej.Add(context.Background(), 1, metric.WithAttributes(attribute.String("tenant", tenant), attribute.String("kind", kind)))
}

func (m *metrics) EdgeResponse(version, action string) {
	m.edgeResp.Add(context.Background(), 1, metric.WithAttributes(attribute.String("version", version), attribute.String("action", action)))
}

func (m *metrics) TenantInflight(tenant string, delta int) {
	m.tenantInfl.Add(context.Background(), int64(delta), metric.WithAttributes(attribute.String("tenant", tenant)))
}

func (m *metrics) BadFrame(version, reason string) {
	m.badFrames.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("version", version), attribute.String("reason", reason)))
}

func (m *metrics) InboundRateLimited(version, outcome string) {
	m.rateLimited.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("version", version), attribute.String("outcome", outcome)))
}
