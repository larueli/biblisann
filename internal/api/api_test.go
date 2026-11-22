package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/danielgtaylor/huma/v2"

	"github.com/larueli/biblisann/internal/webui"
)

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func do(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func newHandler(db Pinger) http.Handler {
	h, _ := New(Deps{Version: "1.2.3", DB: db,
		Web: webui.NewHandler(fstest.MapFS{"index.html": {Data: []byte("<html></html>")}})})
	return h
}

func TestHealth(t *testing.T) {
	if rec := do(newHandler(fakeDB{}), "GET", "/healthz"); rec.Code != 200 {
		t.Errorf("healthz = %d", rec.Code)
	}
	if rec := do(newHandler(fakeDB{}), "GET", "/readyz"); rec.Code != 200 {
		t.Errorf("readyz = %d", rec.Code)
	}
	rec := do(newHandler(fakeDB{err: errors.New("conn refused to postgres://u:pw@h")}), "GET", "/readyz")
	if rec.Code != 503 {
		t.Errorf("readyz with down db = %d, want 503", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "pw@h") {
		t.Error("readyz must not leak the dependency error")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content type = %q", ct)
	}
}

func TestVersionEndpoint(t *testing.T) {
	rec := do(newHandler(nil), "GET", "/v1/version")
	var body struct{ Version string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Version != "1.2.3" {
		t.Errorf("body = %s (%v)", rec.Body.String(), err)
	}
}

func TestOpenAPIServedUnauthenticated(t *testing.T) {
	h := newHandler(nil)
	for _, p := range []string{"/openapi.json", "/openapi.yaml"} {
		if rec := do(h, "GET", p); rec.Code != 200 {
			t.Errorf("%s = %d", p, rec.Code)
		}
	}
	var spec struct {
		OpenAPI string `json:"openapi"`
		Info    struct{ Version string }
	}
	if err := json.Unmarshal(do(h, "GET", "/openapi.json").Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(spec.OpenAPI, "3.1") || spec.Info.Version != "1.2.3" {
		t.Errorf("spec = %+v", spec)
	}
}

func TestEveryOperationDeclaresPermission(t *testing.T) {
	_, a := New(Deps{Version: "x"})
	n := 0
	for path, item := range a.OpenAPI().Paths {
		for method, op := range map[string]*huma.Operation{
			"GET": item.Get, "POST": item.Post, "PUT": item.Put, "PATCH": item.Patch, "DELETE": item.Delete,
		} {
			if op == nil {
				continue
			}
			n++
			if op.Extensions["x-permission"] == nil {
				t.Errorf("%s %s has no x-permission", method, path)
			}
		}
	}
	if n == 0 {
		t.Error("no operations found")
	}
}

func TestRegisterPanicsWithoutPermission(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected a panic")
		}
	}()
	_, a := New(Deps{Version: "x"})
	Register(a, huma.Operation{OperationID: "x", Method: "GET", Path: "/v1/x"}, "",
		func(context.Context, *struct{}) (*struct{}, error) { return nil, nil })
}

func TestUnknownAPIRouteIsProblemJSON(t *testing.T) {
	rec := do(newHandler(nil), "GET", "/v1/nope")
	if rec.Code != 404 || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Errorf("got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestWebFallback(t *testing.T) {
	rec := do(newHandler(nil), "GET", "/library")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<html>") {
		t.Errorf("got %d %s", rec.Code, rec.Body.String())
	}
}
