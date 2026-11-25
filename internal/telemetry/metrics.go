// Package telemetry exposes Prometheus metrics and OpenTelemetry traces.
//
// Metrics are served at /metrics in the Prometheus text format. Traces are exported over OTLP/HTTP
// when the standard OpenTelemetry environment variables are set (OTEL_EXPORTER_OTLP_ENDPOINT, or
// OTEL_EXPORTER_OTLP_TRACES_ENDPOINT); otherwise tracing is off and costs nothing. The OTEL_*
// variables are the standard ones on purpose, so every OpenTelemetry tool and operator knows them.
package telemetry

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics owns the registry.
type Metrics struct {
	reg      *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inflight prometheus.Gauge
}

// New builds the registry with Go runtime, process, HTTP and (when pool is set) database pool metrics.
func New(version string, pool *pgxpool.Pool) *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{reg: reg,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "biblisann_http_requests_total", Help: "HTTP requests by method, route pattern and status."}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "biblisann_http_request_duration_seconds", Help: "HTTP request duration by method and route pattern.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}}, []string{"method", "route"}),
		inflight: prometheus.NewGauge(prometheus.GaugeOpts{Name: "biblisann_http_requests_in_flight", Help: "Requests being served."}),
	}
	info := prometheus.NewGauge(prometheus.GaugeOpts{Name: "biblisann_build_info", Help: "Build information.", ConstLabels: prometheus.Labels{"version": version}})
	info.Set(1)
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}), m.requests, m.duration, m.inflight, info)
	if pool != nil {
		reg.MustRegister(poolCollector{pool})
	}
	return m
}

// Wrap records request metrics. Place it directly around the mux: the route pattern is only known
// once the mux has matched (low-cardinality label: never the raw path).
func (m *Metrics) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		m.inflight.Inc()
		defer m.inflight.Dec()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		if _, p, ok := strings.Cut(route, " "); ok {
			route = p // "GET /v1/x" -> "/v1/x": the method has its own label
		}
		method := r.Method
		switch method {
		case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
			http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		default:
			method = "_OTHER"
		}
		m.requests.WithLabelValues(method, route, strconv.Itoa(sw.status)).Inc()
		m.duration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int)        { s.status = code; s.ResponseWriter.WriteHeader(code) }
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Handler serves /metrics. With a token, callers must send "Authorization: Bearer <token>"; with
// public set the endpoint is open (the operator protects it, for example at the reverse proxy).
// With neither, the endpoint does not exist (404): metrics are never exposed by accident.
func (m *Metrics) Handler(token string, public bool) http.Handler {
	h := promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{EnableOpenMetrics: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !public {
			if token == "" {
				http.NotFound(w, r)
				return
			}
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if got == r.Header.Get("Authorization") || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

// poolCollector reports pgxpool statistics at scrape time.
type poolCollector struct{ pool *pgxpool.Pool }

var (
	descAcquired = prometheus.NewDesc("biblisann_db_pool_acquired_connections", "Connections in use.", nil, nil)
	descIdle     = prometheus.NewDesc("biblisann_db_pool_idle_connections", "Idle connections.", nil, nil)
	descTotal    = prometheus.NewDesc("biblisann_db_pool_total_connections", "Open connections.", nil, nil)
	descMax      = prometheus.NewDesc("biblisann_db_pool_max_connections", "Pool size limit.", nil, nil)
	descWait     = prometheus.NewDesc("biblisann_db_pool_acquire_wait_seconds_total", "Time spent waiting for a connection.", nil, nil)
)

func (c poolCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{descAcquired, descIdle, descTotal, descMax, descWait} {
		ch <- d
	}
}

func (c poolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.pool.Stat()
	ch <- prometheus.MustNewConstMetric(descAcquired, prometheus.GaugeValue, float64(s.AcquiredConns()))
	ch <- prometheus.MustNewConstMetric(descIdle, prometheus.GaugeValue, float64(s.IdleConns()))
	ch <- prometheus.MustNewConstMetric(descTotal, prometheus.GaugeValue, float64(s.TotalConns()))
	ch <- prometheus.MustNewConstMetric(descMax, prometheus.GaugeValue, float64(s.MaxConns()))
	ch <- prometheus.MustNewConstMetric(descWait, prometheus.CounterValue, s.AcquireDuration().Seconds())
}
