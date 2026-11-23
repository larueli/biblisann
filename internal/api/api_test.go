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

	"github.com/larueli/biblisann/internal/telemetry"
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

func TestProbeRoutes(t *testing.T) {
	up, _ := New(Deps{Version: "9.9.9", DB: fakeDB{}})
	down, _ := New(Deps{Version: "9.9.9", DB: fakeDB{err: errors.New("postgres://u:pw@h refused")}})
	for _, p := range []string{"/livez", "/healthz", "/readyz", "/startupz", "/health"} {
		if rec := do(up, "GET", p); rec.Code != 200 {
			t.Errorf("%s with a healthy database: %d", p, rec.Code)
		}
	}
	// liveness never depends on the database; readiness, startup and the aggregate do
	for p, want := range map[string]int{"/livez": 200, "/healthz": 200, "/readyz": 503, "/startupz": 503, "/health": 503} {
		rec := do(down, "GET", p)
		if rec.Code != want {
			t.Errorf("%s with the database down: %d, want %d", p, rec.Code, want)
		}
		if strings.Contains(rec.Body.String(), "pw@h") {
			t.Errorf("%s leaks the dependency error", p)
		}
	}
	var body struct {
		Status  string
		Version string
		Checks  map[string]struct{ Status string }
	}
	_ = json.Unmarshal(do(up, "GET", "/health").Body.Bytes(), &body)
	if body.Status != "ok" || body.Version != "9.9.9" || body.Checks["database"].Status != "ok" {
		t.Errorf("health body: %+v", body)
	}
}

func TestMetricsAccessModes(t *testing.T) {
	m := telemetry.New("1.2.3", nil)
	get := func(h http.Handler, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/metrics", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	withToken, _ := New(Deps{Version: "1.2.3", Metrics: m, MetricsToken: "s3cret-token"})
	public, _ := New(Deps{Version: "1.2.3", Metrics: m, MetricsPublic: true})
	disabled, _ := New(Deps{Version: "1.2.3", Metrics: m})

	if get(withToken, "").Code != 401 || get(withToken, "wrong").Code != 401 {
		t.Error("a token-protected /metrics must refuse missing and wrong tokens")
	}
	do(withToken, "GET", "/v1/version") // produces a labelled request metric
	rec := get(withToken, "s3cret-token")
	if rec.Code != 200 {
		t.Fatalf("with the token: %d", rec.Code)
	}
	for _, want := range []string{`biblisann_build_info{version="1.2.3"} 1`, "go_goroutines", "biblisann_http_requests_total"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("missing %q in metrics", want)
		}
	}
	if !strings.Contains(rec.Body.String(), `route="/v1/version"`) {
		t.Error("requests are labelled by route pattern")
	}
	if get(public, "").Code != 200 {
		t.Error("the public flag removes authentication")
	}
	if get(disabled, "anything").Code != 404 {
		t.Error("without a token and without the public flag /metrics must not exist")
	}
}

func TestAbout(t *testing.T) {
	get := func(version, src string) map[string]string {
		h, _ := New(Deps{Version: version, SourceURL: src})
		var out map[string]string
		_ = json.Unmarshal(do(h, "GET", "/v1/about").Body.Bytes(), &out)
		return out
	}
	if o := get("1.2.3", "https://github.com/larueli/biblisann"); o["source_url"] != "https://github.com/larueli/biblisann/tree/v1.2.3" || o["license"] != "AGPL-3.0-or-later" {
		t.Errorf("release on GitHub: %v", o)
	}
	if o := get("dev+abc1234", "https://github.com/larueli/biblisann"); o["source_url"] != "https://github.com/larueli/biblisann" {
		t.Errorf("dev build: %v", o)
	}
	if o := get("1.2.3", "https://git.example.org/me/fork"); o["source_url"] != "https://git.example.org/me/fork" {
		t.Errorf("fork: %v", o)
	}
}
