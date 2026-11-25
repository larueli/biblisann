package safehttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestIsPublic(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"93.184.216.34", true},
		{"8.8.8.8", true},
		{"2606:4700:4700::1111", true},
		{"127.0.0.1", false},
		{"::1", false},
		{"10.0.0.1", false},
		{"172.16.5.5", false},
		{"192.168.1.1", false},
		{"169.254.169.254", false}, // cloud metadata
		{"fe80::1", false},
		{"fd00:ec2::254", false}, // AWS IPv6 metadata (ULA)
		{"100.64.0.1", false},
		{"0.0.0.0", false},
		{"::", false},
		{"224.0.0.1", false},
		{"255.255.255.255", false},
		{"::ffff:10.0.0.1", false}, // IPv4-mapped private
		{"::ffff:127.0.0.1", false},
		{"::ffff:8.8.8.8", true},
		{"64:ff9b::a00:1", false}, // NAT64 to 10.0.0.1
		{"2002:a00:1::", false},   // 6to4 of 10.0.0.1
		{"198.18.0.1", false},
		{"192.0.2.1", false},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			if got := isPublic(netip.MustParseAddr(tt.ip)); got != tt.want {
				t.Errorf("isPublic(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestProductionClientBlocksLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "secret")
	}))
	defer srv.Close()

	_, err := New(Options{}).Get(context.Background(), srv.URL)
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("err = %v, want ErrBlockedAddress", err)
	}
}

func TestRejectsBadURLs(t *testing.T) {
	c := New(Options{})
	for _, u := range []string{"file:///etc/passwd", "gopher://example.org", "ftp://example.org/x",
		"http://user:pw@example.org/", "http:///nohost"} {
		if _, err := c.Get(context.Background(), u); err == nil {
			t.Errorf("Get(%q) should fail", u)
		}
	}
}

func allowAll(netip.Addr) bool { return true }

func TestBodyLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("a", 100))
	}))
	defer srv.Close()

	read := func(limit int64) error {
		resp, err := newClient(Options{MaxBodyBytes: limit}, allowAll).Get(context.Background(), srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		_, err = io.ReadAll(resp.Body)
		return err
	}
	if err := read(100); err != nil {
		t.Errorf("body exactly at the limit must be readable: %v", err)
	}
	if err := read(99); !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}

func TestRedirectToBlockedHostIsRefused(t *testing.T) {
	// The first hop is allowed (test policy), the policy then changes to the real one for
	// the redirect target: a public-looking host redirecting to loopback must be refused.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "internal")
	}))
	defer target.Close()
	hops := 0
	policy := func(a netip.Addr) bool {
		hops++
		return hops == 1 // first connection allowed, any later one denied
	}
	start := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer start.Close()

	_, err := newClient(Options{}, policy).Get(context.Background(), start.URL)
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("err = %v, want ErrBlockedAddress", err)
	}
}

func TestRedirectLimit(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/again", http.StatusFound)
	}))
	defer srv.Close()
	_, err := newClient(Options{MaxRedirects: 2}, allowAll).Get(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "redirects") {
		t.Fatalf("err = %v, want a redirect limit error", err)
	}
}

func TestRedirectToBadSchemeIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
	}))
	defer srv.Close()
	if _, err := newClient(Options{}, allowAll).Get(context.Background(), srv.URL); err == nil {
		t.Fatal("redirect to file:// must fail")
	}
}

func TestRedirectProtectsCredentialsAndBodies(t *testing.T) {
	c := newClient(Options{}, allowAll)
	for _, tc := range []struct {
		name, start, target string
		body, auth          bool
		allowed             bool
	}{
		{"downgrade", "https://example.org/start", "http://example.org/end", false, false, false},
		{"credential to other origin", "https://example.org/start", "https://other.example.org/end", false, true, false},
		{"body to other origin", "https://example.org/start", "https://other.example.org/end", true, false, false},
		{"credential same origin", "https://example.org/start", "https://example.org/end", false, true, true},
		{"public redirect", "https://example.org/start", "https://other.example.org/end", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body io.Reader
			if tc.body {
				body = strings.NewReader("payload")
			}
			start, _ := http.NewRequest(http.MethodGet, tc.start, body)
			if tc.auth {
				start.Header.Set("Authorization", "Bearer secret")
			}
			target, _ := http.NewRequest(http.MethodGet, tc.target, nil)
			err := c.http.CheckRedirect(target, []*http.Request{start})
			if (err == nil) != tc.allowed {
				t.Errorf("redirect error = %v", err)
			}
		})
	}
}

func TestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	start := time.Now()
	_, err := newClient(Options{Timeout: 100 * time.Millisecond}, allowAll).Get(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected a timeout")
	}
	if time.Since(start) > time.Second {
		t.Error("timeout was not enforced")
	}
}

func TestUserAgentAndErrorsDoNotLeakQuery(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.UserAgent()
	}))
	defer srv.Close()
	resp, err := newClient(Options{UserAgent: "biblisann-test"}, allowAll).Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got != "biblisann-test" {
		t.Errorf("user agent = %q", got)
	}

	_, err = New(Options{}).Get(context.Background(), "http://127.0.0.1:1/x?api_key=SECRET")
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("error must not leak the query string: %v", err)
	}
	_, err = New(Options{}).Get(context.Background(), "http://127.0.0.1:1/SECRET")
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("error must not leak capability paths: %v", err)
	}
}

func TestIPv6TransitionRangesBlocked(t *testing.T) {
	for _, s := range []string{"::a00:1", "::ffff:0:a00:1", "2001:0:4136:e378:8000:63bf:3fff:fdd2"} {
		if isPublic(netip.MustParseAddr(s)) {
			t.Errorf("%s must be blocked", s)
		}
	}
}
