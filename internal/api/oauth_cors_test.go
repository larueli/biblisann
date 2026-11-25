package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestOAuthPublicEndpointsAcceptCrossOriginPost(t *testing.T) {
	a := &Authenticator{BaseURL: &url.URL{Scheme: "https", Host: "example.org"}}
	h := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/oauth/register", http.StatusNoContent},
		{"/oauth/token", http.StatusNoContent},
		{"/oauth/authorize", http.StatusForbidden},
		{"/v1/me", http.StatusForbidden},
	} {
		r := httptest.NewRequest(http.MethodPost, tc.path, nil)
		r.Header.Set("Origin", "https://example.net")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s: got %d, want %d", tc.path, w.Code, tc.want)
		}
	}
}
