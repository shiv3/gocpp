package observability

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Tracer wraps an OTel tracer; the default is a no-op.
type Tracer struct {
	tracer trace.Tracer
}

// NewTracer builds a Tracer from a TracerProvider. A nil provider yields a no-op.
func NewTracer(tp trace.TracerProvider) Tracer {
	if tp == nil {
		tp = noop.NewTracerProvider()
	}
	return Tracer{tracer: tp.Tracer("github.com/shiv3/gocpp")}
}

// Start begins a span.
func (t Tracer) Start(ctx context.Context, name string) (context.Context, trace.Span) {
	if t.tracer == nil {
		t.tracer = noop.NewTracerProvider().Tracer("github.com/shiv3/gocpp")
	}
	return t.tracer.Start(ctx, name)
}
