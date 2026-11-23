package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/larueli/biblisann/internal/api"
	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/catalog"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/library"
	"github.com/larueli/biblisann/internal/providers"
	"github.com/larueli/biblisann/internal/ratelimit"
	"github.com/larueli/biblisann/internal/storage"
)

type fakeProvider struct{}

func (fakeProvider) Name() string { return "fake" }
func (fakeProvider) Lookup(_ context.Context, isbn string) (*providers.Record, error) {
	if isbn == "9782070612758" {
		return &providers.Record{Title: "Le Petit Prince", Authors: []string{"Saint-Exupéry"}, Language: "fr", Pages: 93}, nil
	}
	return nil, nil
}

type client struct {
	t      *testing.T
	srv    *httptest.Server
	cookie *http.Cookie
	csrf   string
}

func (c *client) do(method, path string, body any, hdr map[string]string) (int, map[string]any, http.Header) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.srv.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		var arr []any
		if json.Unmarshal(raw, &arr) == nil {
			out = map[string]any{"_array": arr}
		}
	}
	return resp.StatusCode, out, resp.Header
}

// w sends a state-changing request with the CSRF header.
func (c *client) w(method, path string, body any, extra ...string) (int, map[string]any) {
	h := map[string]string{"X-CSRF-Token": c.csrf}
	for i := 0; i+1 < len(extra); i += 2 {
		h[extra[i]] = extra[i+1]
	}
	st, out, _ := c.do(method, path, body, h)
	return st, out
}

func (c *client) login(email, pw string) {
	c.t.Helper()
	st, out, hdr := c.do("POST", "/v1/auth/login", map[string]string{"email": email, "password": pw}, nil)
	if st != 200 {
		c.t.Fatalf("login %d %v", st, out)
	}
	for _, ck := range (&http.Response{Header: hdr}).Cookies() {
		if ck.Name == "bb_session" {
			c.cookie = ck
		}
	}
	c.csrf = out["csrf_token"].(string)
}

func newServer(t *testing.T) (*httptest.Server, *auth.Service) {
	pool := dbtest.Pool(t)
	authSvc, _ := auth.NewService(pool, auth.RegistrationFirstUser)
	covers, _ := storage.NewLocal(t.TempDir())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h, _ := api.New(api.Deps{Version: "test", DB: pool, Log: log, BaseURL: &url.URL{Scheme: "http", Host: "localhost:8080"},
		Auth: authSvc, Library: library.NewService(pool),
		Catalog: catalog.NewService(pool, catalog.Config{Providers: []providers.Provider{fakeProvider{}}, Covers: covers, Log: log}),
		Limiter: ratelimit.NewMemory(5, time.Hour)})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, authSvc
}

func TestFullFlow(t *testing.T) {
	srv, authSvc := newServer(t)
	c := &client{t: t, srv: srv}

	// anonymous access is refused
	if st, _, _ := c.do("GET", "/v1/libraries", nil, nil); st != 401 {
		t.Errorf("anonymous list: %d", st)
	}
	st, out, hdr := c.do("POST", "/v1/auth/register", map[string]string{"email": "me@example.org", "password": "tulips in the garden", "name": "Me"}, nil)
	if st != 201 {
		t.Fatalf("register %d %v", st, out)
	}
	setCookie := hdr.Get("Set-Cookie")
	if !strings.Contains(setCookie, "HttpOnly") || !strings.Contains(setCookie, "SameSite=Lax") {
		t.Errorf("cookie flags: %s", setCookie)
	}
	c.login("me@example.org", "tulips in the garden")

	// CSRF and origin protection
	if st, _, _ := c.do("POST", "/v1/libraries", map[string]string{"name": "x"}, nil); st != 403 {
		t.Errorf("missing CSRF token: %d", st)
	}
	if st, _, _ := c.do("POST", "/v1/libraries", map[string]string{"name": "x"},
		map[string]string{"X-CSRF-Token": c.csrf, "Origin": "https://evil.example"}); st != 403 {
		t.Errorf("cross-origin: %d", st)
	}
	st, lib := c.w("POST", "/v1/libraries", map[string]string{"name": "Home"})
	if st != 201 {
		t.Fatalf("create library %d %v", st, lib)
	}
	libID := lib["id"].(string)

	// scan: added, replay-safe, duplicate warning
	st, r := c.w("POST", "/v1/libraries/"+libID+"/scan", map[string]any{"isbn": "978-2-07-061275-8"}, "Idempotency-Key", "scan-key-0001")
	if st != 200 || r["status"] != "added" {
		t.Fatalf("scan %d %v", st, r)
	}
	first := r["instance"].(map[string]any)["id"]
	_, r = c.w("POST", "/v1/libraries/"+libID+"/scan", map[string]any{"isbn": "9782070612758"}, "Idempotency-Key", "scan-key-0001")
	if r["status"] != "added" || r["instance"].(map[string]any)["id"] != first {
		t.Errorf("replay must return the first result: %v", r)
	}
	_, r = c.w("POST", "/v1/libraries/"+libID+"/scan", map[string]any{"isbn": "9782070612758"}, "Idempotency-Key", "scan-key-0002")
	if r["status"] != "already_owned" {
		t.Errorf("duplicate scan: %v", r)
	}
	_, r = c.w("POST", "/v1/libraries/"+libID+"/scan", map[string]any{"isbn": "9782070612758", "allow_duplicate": true}, "Idempotency-Key", "scan-key-0003")
	if r["status"] != "added" {
		t.Errorf("allow_duplicate: %v", r)
	}
	_, r = c.w("POST", "/v1/libraries/"+libID+"/scan", map[string]any{"isbn": "9780306406157"}, "Idempotency-Key", "scan-key-0004")
	if r["status"] != "not_found" {
		t.Errorf("unknown isbn: %v", r)
	}
	if st, _ := c.w("POST", "/v1/libraries/"+libID+"/scan", map[string]any{"isbn": "123"}, "Idempotency-Key", "scan-key-0005"); st != 422 {
		t.Errorf("bad isbn: %d", st)
	}

	// lookup says whether I own it
	_, r, _ = c.do("GET", "/v1/isbn/9782070612758", nil, nil)
	if r["source"] != "catalog" || len(r["owned"].([]any)) != 2 {
		t.Errorf("lookup: %v", r)
	}

	// list, search, sync
	_, r, _ = c.do("GET", "/v1/libraries/"+libID+"/instances?q=petit", nil, nil)
	if len(r["items"].([]any)) != 2 {
		t.Errorf("search: %v", r)
	}
	_, r, _ = c.do("GET", "/v1/libraries/"+libID+"/sync?cursor=0", nil, nil)
	if len(r["entries"].([]any)) != 2 || r["cursor"].(float64) == 0 {
		t.Errorf("sync: %v", r)
	}

	// another user cannot see the library (404, not 403)
	if _, err := authSvc.CreateUser(context.Background(), "other@example.org", "tulips in the garden", "O", false); err != nil {
		t.Fatal(err)
	}
	o := &client{t: t, srv: srv}
	o.login("other@example.org", "tulips in the garden")
	if st, _, _ := o.do("GET", "/v1/libraries/"+libID, nil, nil); st != 404 {
		t.Errorf("non-member get: %d", st)
	}
	if st, _, _ := o.do("GET", "/v1/libraries/"+libID+"/instances", nil, nil); st != 404 {
		t.Errorf("non-member list: %d", st)
	}

	// logout kills the session
	if st, _ := c.w("POST", "/v1/auth/logout", nil); st != 204 {
		t.Errorf("logout %d", st)
	}
	if st, _, _ := c.do("GET", "/v1/libraries", nil, nil); st != 401 {
		t.Errorf("after logout: %d", st)
	}
}

func TestLoginThrottling(t *testing.T) {
	srv, _ := newServer(t)
	c := &client{t: t, srv: srv}
	c.do("POST", "/v1/auth/register", map[string]string{"email": "me@example.org", "password": "tulips in the garden"}, nil)
	var last int
	for range 8 {
		last, _, _ = c.do("POST", "/v1/auth/login", map[string]string{"email": "me@example.org", "password": "wrong wrong wrong"}, nil)
	}
	if last != 429 {
		t.Errorf("brute force not throttled: %d", last)
	}
}

func TestSecondRegistrationClosed(t *testing.T) {
	srv, _ := newServer(t)
	c := &client{t: t, srv: srv}
	c.do("POST", "/v1/auth/register", map[string]string{"email": "a@example.org", "password": "tulips in the garden"}, nil)
	st, _, _ := c.do("POST", "/v1/auth/register", map[string]string{"email": "b@example.org", "password": "tulips in the garden"}, nil)
	if st != 403 {
		t.Errorf("second registration: %d", st)
	}
}
