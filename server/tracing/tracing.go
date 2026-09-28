// Package tracing configures OpenTelemetry for Atlantis.
//
// Export is configured with the standard OTEL_* environment variables
// (OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_EXPORTER_OTLP_HEADERS, ...). When tracing
// is disabled a no-op provider is used, so instrumented code costs nothing,
// but W3C trace context is still propagated between replicas.
package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/runatlantis/atlantis"

// Config configures tracing.
type Config struct {
	Enabled     bool
	ServiceName string
	Version     string
	// Identity and Namespace are recorded as k8s.pod.name / k8s.namespace.name.
	Identity  string
	Namespace string
}

// Setup installs the global tracer provider and propagator. The returned
// function flushes and shuts down the exporter.
func Setup(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if !cfg.Enabled {
		return func(context.Context) error { return nil }, nil
	}
	exp, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP exporter: %w", err)
	}
	attrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.Version),
	}
	if cfg.Identity != "" {
		attrs = append(attrs, semconv.K8SPodName(cfg.Identity), semconv.ServiceInstanceID(cfg.Identity))
	}
	if cfg.Namespace != "" {
		attrs = append(attrs, semconv.K8SNamespaceName(cfg.Namespace))
	}
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(semconv.SchemaURL, attrs...))
	if err != nil {
		return nil, err
	}
	// The sampler is left to OTEL_TRACES_SAMPLER / OTEL_TRACES_SAMPLER_ARG
	// (default: parent-based, always on).
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// Tracer returns the Atlantis tracer.
func Tracer() trace.Tracer { return otel.Tracer(instrumentationName) }

// Start starts a span. A nil ctx is treated as context.Background().
func Start(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	return Tracer().Start(ctx, name, trace.WithAttributes(attrs...))
}

// End records err on the span, if any, and ends it.
func End(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// Fail marks the span failed with a message that is not a Go error
// (e.g. an Atlantis "failure" such as unmet apply requirements).
func Fail(span trace.Span, msg string) {
	if msg != "" {
		span.SetStatus(codes.Error, msg)
	}
}

// LogFields returns trace_id/span_id key-value pairs for structured loggers,
// or nil when ctx carries no sampled span.
func LogFields(ctx context.Context) []any {
	if ctx == nil {
		return nil
	}
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return nil
	}
	return []any{"trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String()}
}

// Common attribute keys.
var (
	AttrRepo      = attribute.Key("atlantis.repo")
	AttrPull      = attribute.Key("atlantis.pull")
	AttrCommand   = attribute.Key("atlantis.command")
	AttrProject   = attribute.Key("atlantis.project")
	AttrWorkspace = attribute.Key("atlantis.workspace")
	AttrDir       = attribute.Key("atlantis.dir")
	AttrStep      = attribute.Key("atlantis.step")
	AttrReplica   = attribute.Key("atlantis.replica")
)
