package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/larueli/biblisann/internal/authz"
)

func TestImportGuard(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	h := guardImports(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	request := func(p *authz.Principal) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/v1/me/import", nil)
		if p != nil {
			r = r.WithContext(authz.With(context.Background(), p))
		}
		return r
	}
	for _, tc := range []struct {
		p      *authz.Principal
		status int
	}{
		{nil, http.StatusUnauthorized},
		{&authz.Principal{Scopes: []string{"read"}}, http.StatusForbidden},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, request(tc.p))
		if w.Code != tc.status {
			t.Fatalf("got status %d, want %d", w.Code, tc.status)
		}
	}
	p := &authz.Principal{Scopes: []string{"write"}}
	done := make(chan struct{}, 2)
	for range 2 {
		go func() { h.ServeHTTP(httptest.NewRecorder(), request(p)); done <- struct{}{} }()
	}
	<-entered
	<-entered
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request(p))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("saturation: %d", w.Code)
	}
	close(release)
	<-done
	<-done
}
