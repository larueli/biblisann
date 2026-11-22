package httpmw

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// Middleware wraps a handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares so that the first one is the outermost.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

type ctxKey int

const (
	keyClientIP ctxKey = iota
	keyRequestID
)

// ClientIP returns the client address resolved by the ClientIP middleware.
func ClientIP(ctx context.Context) netip.Addr {
	ip, _ := ctx.Value(keyClientIP).(netip.Addr)
	return ip
}

// RequestID returns the request ID set by the RequestID middleware.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(keyRequestID).(string)
	return id
}

// ResolveClientIP returns the client address of r. X-Forwarded-For is honored only when the
// direct peer is a trusted proxy, and is read from the right: the first address that is not
// itself a trusted proxy is the client. A client can forge the left part of the header, never
// the part appended by a proxy we trust.
func ResolveClientIP(r *http.Request, trusted []netip.Prefix) netip.Addr {
	peer := parseHostPort(r.RemoteAddr)
	if !peer.IsValid() || !contains(trusted, peer) {
		return peer
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return peer // malformed header: do not guess
		}
		addr = addr.Unmap()
		if !contains(trusted, addr) {
			return addr
		}
	}
	return peer
}

func parseHostPort(s string) netip.Addr {
	host, _, err := net.SplitHostPort(s)
	if err != nil {
		host = s
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return a.Unmap()
}

func contains(prefixes []netip.Prefix, a netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// WithClientIP stores the resolved client address in the request context.
func WithClientIP(trusted []netip.Prefix) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), keyClientIP, ResolveClientIP(r, trusted))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// WithRequestID tags each request with a random ID, echoed in the X-Request-ID response header.
// An incoming ID is never trusted: it could be used to forge log lines.
func WithRequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var b [8]byte
			_, _ = rand.Read(b[:])
			id := hex.EncodeToString(b[:])
			w.Header().Set("X-Request-ID", id)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyRequestID, id)))
		})
	}
}

// SecurityHeaders sets restrictive default headers. hsts enables Strict-Transport-Security and
// must only be on when the public URL is https.
func SecurityHeaders(hsts bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy",
				"default-src 'self'; img-src 'self' data: blob:; object-src 'none'; base-uri 'none'; "+
					"frame-ancestors 'none'; form-action 'self'; worker-src 'self'; manifest-src 'self'")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			// The camera is needed by the barcode scanner, same origin only.
			h.Set("Permissions-Policy", "camera=(self), microphone=(), geolocation=(), payment=()")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Recover turns a panic into a 500 problem response and logs the panic.
func Recover(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				//nolint:errorlint // recover() returns a value, not an error chain
				if v := recover(); v != nil && v != any(http.ErrAbortHandler) {
					log.ErrorContext(r.Context(), "panic in handler", "panic", v, "request_id", RequestID(r.Context()))
					w.Header().Set("Content-Type", "application/problem+json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"title":"Internal Server Error","status":500}`))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// AccessLog logs one line per request. The query string is not logged (it may hold tokens).
func AccessLog(log *slog.Logger, skip func(*http.Request) bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			if skip != nil && skip(r) {
				return
			}
			log.InfoContext(r.Context(), "request",
				"method", r.Method, "path", r.URL.Path, "status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"client_ip", ClientIP(r.Context()).String(), "request_id", RequestID(r.Context()))
		})
	}
}
