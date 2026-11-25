package telemetry

import (
	"context"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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

// Middleware creates a minimal server span while continuing the incoming W3C trace context.
// It deliberately does not capture paths, hosts, addresses, request headers or baggage: these can
// contain private data or credentials. Probe and metrics paths are not traced.
func Middleware(next http.Handler) http.Handler {
	return traceMiddleware(next, otel.GetTracerProvider())
}

func traceMiddleware(next http.Handler, provider trace.TracerProvider) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := path.Clean(r.URL.Path)
		switch cleanPath {
		case "/healthz", "/livez", "/readyz", "/startupz", "/health", "/metrics":
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(cleanPath, "/opds/") || strings.HasPrefix(cleanPath, "/v1/invitations/") {
			next.ServeHTTP(w, r)
			return
		}
		method := r.Method
		switch method {
		case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
			http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		default:
			method = "_OTHER"
		}
		// Extract only W3C trace context, not untrusted baggage that may contain private data.
		ctx := propagation.TraceContext{}.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		if remote := trace.SpanContextFromContext(ctx); remote.IsValid() {
			ctx = trace.ContextWithRemoteSpanContext(ctx, remote.WithTraceState(trace.TraceState{}))
		}
		ctx, span := provider.Tracer("github.com/larueli/biblisann/internal/telemetry").Start(ctx, "HTTP "+method,
			trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attribute.String("http.request.method", method)))
		defer span.End()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r.WithContext(ctx))
		span.SetAttributes(attribute.Int("http.response.status_code", sw.status))
		if sw.status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, "")
		}
	})
}

// TraceID returns the trace ID of the request context, or "".
func TraceID(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		return sc.TraceID().String()
	}
	return ""
}
