package telemetry

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTraceOmitsRequestData(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	h := traceMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/private-name" || r.URL.RawQuery != "secret=value" || r.RemoteAddr == "" {
			t.Fatal("the handler did not receive the original request")
		}
		if TraceID(r.Context()) == "" {
			t.Error("the handler did not receive a trace context")
		}
		w.WriteHeader(http.StatusBadGateway)
	}), provider)
	r := httptest.NewRequest(http.MethodGet, "https://example.org/private-name?secret=value", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Authorization", "Bearer secret")
	r.Header.Set("Baggage", "private=secret")
	r.Header.Set("Traceparent", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
	r.Header.Set("Tracestate", "vendor=secret")
	h.ServeHTTP(httptest.NewRecorder(), r)
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Name != "HTTP GET" {
		t.Fatalf("spans: %+v", spans)
	}
	if got := spans[0].SpanContext.TraceID().String(); got != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("trace context not propagated: %s", got)
	}
	if got := spans[0].SpanContext.TraceState().String(); got != "" {
		t.Errorf("trace state retained: %q", got)
	}
	for _, a := range spans[0].Attributes {
		if a.Key != "http.request.method" && a.Key != "http.response.status_code" {
			t.Errorf("unexpected span attribute %s: %v", a.Key, a.Value)
		}
	}
	for _, private := range []string{"private-name", "secret", "192.0.2.1", "example.org"} {
		if strings.Contains(fmt.Sprint(spans[0].Attributes), private) || strings.Contains(spans[0].Name, private) {
			t.Errorf("trace contains private request data %q", private)
		}
	}
}

func TestTraceSkipsSensitiveAndProbePaths(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	h := traceMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), provider)
	for _, p := range []string{"/healthz", "/opds/token", "/v1/invitations/token", "/other/../opds/token"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, p, nil))
	}
	if got := exporter.GetSpans(); len(got) != 0 {
		t.Fatalf("sensitive paths generated spans: %+v", got)
	}
}
