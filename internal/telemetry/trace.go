package telemetry

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// TracingEnabled reports whether the standard OTLP environment asks for traces.
func TracingEnabled() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != ""
}

// SetupTracing installs an OTLP/HTTP trace exporter configured by the standard OTEL_* variables
// (endpoint, headers, protocol, sampler). It returns a shutdown function that flushes spans. When
// tracing is not configured it does nothing.
func SetupTracing(ctx context.Context, version string) (func(context.Context) error, error) {
	if !TracingEnabled() {
		return func(context.Context) error { return nil }, nil
	}
	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(semconv.ServiceName("biblisann"), semconv.ServiceVersion(version)))
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(5*time.Second)), sdktrace.WithResource(res))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return tp.Shutdown, nil
}

// Middleware creates a server span per request (continuing an incoming W3C trace context). The span
// name is the method until the route is known. Probe and metrics paths are not traced.
func Middleware(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "http.server",
		otelhttp.WithFilter(func(r *http.Request) bool {
			switch r.URL.Path {
			case "/healthz", "/livez", "/readyz", "/startupz", "/health", "/metrics":
				return false
			}
			return true
		}),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return r.Method + " " + spanPath(r.URL.Path) }))
}

// TraceID returns the trace ID of the request context, or "".
func TraceID(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		return sc.TraceID().String()
	}
	return ""
}

// spanPath replaces identifier-looking segments, keeping span names low-cardinality.
func spanPath(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		if len(seg) == 36 && strings.Count(seg, "-") == 4 || len(seg) >= 20 && strings.HasPrefix(seg, "bb") {
			parts[i] = "{id}"
		}
	}
	return strings.Join(parts, "/")
}
