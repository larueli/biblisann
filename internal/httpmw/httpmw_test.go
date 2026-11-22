package httpmw

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func pfx(ss ...string) []netip.Prefix {
	var out []netip.Prefix
	for _, s := range ss {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

func TestResolveClientIP(t *testing.T) {
	tests := []struct {
		name    string
		remote  string
		xff     []string
		trusted []netip.Prefix
		want    string
	}{
		{"no proxy configured ignores header", "203.0.113.9:1234", []string{"1.2.3.4"}, nil, "203.0.113.9"},
		{"untrusted peer cannot spoof", "203.0.113.9:1234", []string{"1.2.3.4"}, pfx("10.0.0.0/8"), "203.0.113.9"},
		{"trusted peer, single hop", "10.0.0.2:80", []string{"198.51.100.7"}, pfx("10.0.0.0/8"), "198.51.100.7"},
		{"client-forged left part is ignored", "10.0.0.2:80", []string{"6.6.6.6, 198.51.100.7"}, pfx("10.0.0.0/8"), "198.51.100.7"},
		{"chain of trusted proxies", "10.0.0.2:80", []string{"198.51.100.7, 10.0.0.9"}, pfx("10.0.0.0/8"), "198.51.100.7"},
		{"several header lines", "10.0.0.2:80", []string{"6.6.6.6", "198.51.100.7"}, pfx("10.0.0.0/8"), "198.51.100.7"},
		{"malformed header falls back to peer", "10.0.0.2:80", []string{"garbage"}, pfx("10.0.0.0/8"), "10.0.0.2"},
		{"all hops trusted", "10.0.0.2:80", []string{"10.0.0.5"}, pfx("10.0.0.0/8"), "10.0.0.2"},
		{"ipv6 peer", "[2001:db8::1]:80", nil, nil, "2001:db8::1"},
		{"mapped ipv4 peer", "[::ffff:10.0.0.2]:80", []string{"198.51.100.7"}, pfx("10.0.0.0/8"), "198.51.100.7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remote
			for _, x := range tt.xff {
				r.Header.Add("X-Forwarded-For", x)
			}
			if got := ResolveClientIP(r, tt.trusted).String(); got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestChainRecoverHeadersLog(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	h := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }),
		WithRequestID(), WithClientIP(nil), AccessLog(log, nil), Recover(log), SecurityHeaders(true))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x?token=SECRET", nil)
	req.Header.Set("X-Request-ID", "forged")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d", rec.Code)
	}
	if rec.Header().Get("X-Request-ID") == "forged" || rec.Header().Get("X-Request-ID") == "" {
		t.Error("request ID must be generated server-side")
	}
	for _, hdr := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Strict-Transport-Security"} {
		if rec.Header().Get(hdr) == "" {
			t.Errorf("missing %s", hdr)
		}
	}
	if strings.Contains(buf.String(), "SECRET") {
		t.Error("query string must not be logged")
	}
	if !strings.Contains(buf.String(), `"status":500`) {
		t.Errorf("access log should record the 500: %s", buf.String())
	}
}
