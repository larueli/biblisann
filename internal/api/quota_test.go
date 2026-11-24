package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/ratelimit"
)

func TestQuotaMiddlewareHeadersAndRetryAfter(t *testing.T) {
	user := uuid.New()
	h := quotaMiddleware(ratelimit.NewMemory(10, time.Minute/10), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	call := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r = r.WithContext(authz.With(r.Context(), &authz.Principal{UserID: user, Via: "session"}))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	w := call("GET", "/v1/libraries")
	if w.Code != http.StatusNoContent || w.Header().Get("RateLimit-Policy") != `"default";q=10;w=60` || w.Header().Get("RateLimit") != `"default";r=9;t=6` {
		t.Fatalf("first call: %d policy=%q ratelimit=%q", w.Code, w.Header().Get("RateLimit-Policy"), w.Header().Get("RateLimit"))
	}
	// a provider lookup weighs 5 tokens
	if w := call("GET", "/v1/isbn/9782070612758"); w.Header().Get("RateLimit") != `"default";r=4;t=36` {
		t.Errorf("weighted call: %q", w.Header().Get("RateLimit"))
	}
	// the next lookup does not fit: 429 with Retry-After
	w = call("POST", "/v1/libraries/x/scan")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("over quota: %d retry-after=%q", w.Code, w.Header().Get("Retry-After"))
	}
	// covers and anonymous routes are not metered
	if w := call("GET", "/v1/covers/abc"); w.Code != http.StatusNoContent || w.Header().Get("RateLimit") != "" {
		t.Errorf("covers metered: %d", w.Code)
	}
	anon := httptest.NewRecorder()
	h.ServeHTTP(anon, httptest.NewRequest("GET", "/v1/auth/login", nil))
	if anon.Code != http.StatusNoContent || anon.Header().Get("RateLimit") != "" {
		t.Errorf("anonymous metered: %d", anon.Code)
	}
}
