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

	"github.com/larueli/biblisann/internal/account"
	"github.com/larueli/biblisann/internal/api"
	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/catalog"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/importer"
	"github.com/larueli/biblisann/internal/library"
	"github.com/larueli/biblisann/internal/lists"
	"github.com/larueli/biblisann/internal/loans"
	"github.com/larueli/biblisann/internal/notify"
	"github.com/larueli/biblisann/internal/providers"
	"github.com/larueli/biblisann/internal/ratelimit"
	"github.com/larueli/biblisann/internal/reading"
	"github.com/larueli/biblisann/internal/scan"
	"github.com/larueli/biblisann/internal/storage"
	"github.com/larueli/biblisann/internal/tokens"
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
	lib := library.NewService(pool)
	tk := tokens.NewService(pool)
	catSvc := catalog.NewService(pool, catalog.Config{Providers: []providers.Provider{fakeProvider{}}, Covers: covers, Log: log})
	h, _ := api.New(api.Deps{Version: "test", DB: pool, Log: log, BaseURL: &url.URL{Scheme: "http", Host: "localhost:8080"},
		Auth: authSvc, Library: lib, Catalog: catSvc,
		Account:  account.NewService(pool, lib, catSvc, lists.NewService(pool, lib), loans.NewService(pool, lib), reading.NewService(pool)),
		Importer: importer.New(pool, catSvc, lib, reading.NewService(pool), lists.NewService(pool, lib)),
		Backup:   func(_ context.Context, w io.Writer) error { _, err := io.WriteString(w, "TAR"); return err },
		Scan:     scan.New(catSvc, lib), Reading: reading.NewService(pool), Lists: lists.NewService(pool, lib), Loans: loans.NewService(pool, lib),
		TokenSvc: tk, Tokens: tk, Channels: notify.NewChannels(dbsqlc.New(pool)),
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

func TestOrganiseFlow(t *testing.T) {
	srv, _ := newServer(t)
	c := &client{t: t, srv: srv}
	c.do("POST", "/v1/auth/register", map[string]string{"email": "me@example.org", "password": "tulips in the garden"}, nil)
	c.login("me@example.org", "tulips in the garden")
	_, lib := c.w("POST", "/v1/libraries", map[string]string{"name": "Home"})
	libID := lib["id"].(string)
	_, r := c.w("POST", "/v1/libraries/"+libID+"/scan", map[string]any{"isbn": "9782070612758"}, "Idempotency-Key", "scan-key-0001")
	inst := r["instance"].(map[string]any)["id"].(string)
	work := r["edition"].(map[string]any)["work_id"].(string)

	// reading status, grade, stats
	if st, out := c.w("PUT", "/v1/works/"+work+"/reading", map[string]any{"status": "reading"}); st != 200 || out["status"] != "reading" {
		t.Errorf("reading %d %v", st, out)
	}
	if st, _ := c.w("PUT", "/v1/works/"+work+"/grade", map[string]any{"grade": 9, "comment": "great"}); st != 200 {
		t.Errorf("grade %d", st)
	}
	if st, _ := c.w("PUT", "/v1/works/"+work+"/grade", map[string]any{"grade": 11, "comment": "x"}); st != 422 {
		t.Errorf("grade 11: %d", st)
	}
	_, out, _ := c.do("GET", "/v1/me/stats", nil, nil)
	if out["average_grade"].(float64) != 9 {
		t.Errorf("stats %v", out)
	}

	// wishlist
	_, out, _ = c.do("GET", "/v1/lists", nil, nil)
	wl := out["_array"].([]any)[0].(map[string]any)
	if wl["kind"] != "wishlist" {
		t.Fatalf("lists %v", out)
	}
	if st, _ := c.w("POST", "/v1/lists/"+wl["id"].(string)+"/items", map[string]any{"title": "Wanted book"}); st != 201 {
		t.Errorf("add item %d", st)
	}

	// loans + feed
	_, contact := c.w("POST", "/v1/contacts", map[string]any{"name": "Marie"})
	due := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	st, loan := c.w("POST", "/v1/instances/"+inst+"/lend", map[string]any{"contact_id": contact["id"], "due_on": due})
	if st != 201 {
		t.Fatalf("lend %d %v", st, loan)
	}
	if st, _ := c.w("POST", "/v1/instances/"+inst+"/lend", map[string]any{"counterparty": "Paul"}); st != 409 {
		t.Errorf("double lend: %d", st)
	}
	_, tok := c.w("POST", "/v1/me/tokens", map[string]any{"name": "calendar", "kind": "feed"})
	resp, err := http.Get(srv.URL + "/v1/feeds/loans.ics?token=" + tok["token"].(string))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "SUMMARY:Get back: Le Petit Prince (from Marie)") {
		t.Errorf("feed %d:\n%s", resp.StatusCode, body)
	}
	bad, err := http.Get(srv.URL + "/v1/feeds/loans.ics?token=bb_feed_wrong")
	if err != nil {
		t.Fatal(err)
	}
	_ = bad.Body.Close()
	if bad.StatusCode != 404 {
		t.Errorf("bad feed token: %d", bad.StatusCode)
	}

	// read-only personal access token: bearer auth, no CSRF needed, cannot write
	st, pat := c.w("POST", "/v1/me/tokens", map[string]any{"name": "script", "scopes": []string{"read"}})
	if st != 201 {
		t.Fatalf("create pat %d %v", st, pat)
	}
	bearer := map[string]string{"Authorization": "Bearer " + pat["token"].(string)}
	if st, _, _ := (&client{t: t, srv: srv}).do("GET", "/v1/libraries", nil, bearer); st != 200 {
		t.Errorf("bearer read: %d", st)
	}
	if st, _, _ := (&client{t: t, srv: srv}).do("POST", "/v1/libraries", map[string]string{"name": "x"}, bearer); st != 403 {
		t.Errorf("read-only token must not write: %d", st)
	}
	if st, _, _ := (&client{t: t, srv: srv}).do("GET", "/v1/libraries", nil, map[string]string{"Authorization": "Bearer bb_pat_nope"}); st != 401 {
		t.Errorf("bad token: %d", st)
	}
	if st, _, _ := (&client{t: t, srv: srv}).do("POST", "/v1/me/tokens", map[string]any{"name": "child", "scopes": []string{"read"}}, bearer); st != 403 {
		t.Errorf("token minting token: %d", st)
	}

	// sharing: unknown user is a validation error; admin-only group creation
	if st, _ := c.w("PUT", "/v1/libraries/"+libID+"/sharing/users", map[string]any{"email": "ghost@example.org", "grant": map[string]any{"role": "read"}}); st != 422 {
		t.Errorf("share with unknown user: %d", st)
	}
	if st, _ := c.w("POST", "/v1/groups", map[string]any{"name": "Family"}); st != 201 {
		t.Errorf("admin creates group: %d", st)
	}
	_, sh, _ := c.do("GET", "/v1/libraries/"+libID+"/sharing", nil, nil)
	if len(sh["users"].([]any)) != 1 {
		t.Errorf("sharing %v", sh)
	}
}

func TestImportInventoryBackup(t *testing.T) {
	srv, authSvc := newServer(t)
	c := &client{t: t, srv: srv}
	c.do("POST", "/v1/auth/register", map[string]string{"email": "me@example.org", "password": "tulips in the garden"}, nil)
	c.login("me@example.org", "tulips in the garden")
	_, lib := c.w("POST", "/v1/libraries", map[string]string{"name": "Home"})
	libID := lib["id"].(string)

	csv := "Title,Author,ISBN13,My Rating,Exclusive Shelf,Owned Copies\nDune,Frank Herbert,=\"9780441172719\",5,read,1\n"
	req, _ := http.NewRequest("POST", srv.URL+"/v1/libraries/"+libID+"/import/goodreads", strings.NewReader(csv))
	req.Header.Set("Content-Type", "text/csv")
	req.Header.Set("X-CSRF-Token", c.csrf)
	req.AddCookie(c.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var rep map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&rep)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || rep["instances_added"].(float64) != 1 {
		t.Fatalf("import %d %v", resp.StatusCode, rep)
	}

	_, place := c.w("POST", "/v1/libraries/"+libID+"/places", map[string]any{"name": "Shelf"})
	st, inv := c.w("POST", "/v1/places/"+place["id"].(string)+"/inventory", map[string]any{"isbns": []string{"9780441172719"}})
	if st != 200 || len(inv["unexpected"].([]any)) != 1 {
		t.Errorf("inventory %d %v", st, inv)
	}

	// backup: administrators only, and anonymous callers learn nothing
	if st, _, _ := (&client{t: t, srv: srv}).do("GET", "/v1/admin/backup", nil, nil); st != 401 {
		t.Errorf("anonymous backup: %d", st)
	}
	if st, _, _ := c.do("GET", "/v1/admin/backup", nil, nil); st != 200 {
		t.Errorf("admin backup: %d", st)
	}
	if _, err := authSvc.CreateUser(context.Background(), "plain@example.org", "tulips in the garden", "P", false); err != nil {
		t.Fatal(err)
	}
	p := &client{t: t, srv: srv}
	p.login("plain@example.org", "tulips in the garden")
	if st, _, _ := p.do("GET", "/v1/admin/backup", nil, nil); st != 404 {
		t.Errorf("non-admin backup: %d", st)
	}
}

func TestMFAOverAPI(t *testing.T) {
	srv, authSvc := newServer(t)
	if err := authSvc.EnableMFA(bytes.Repeat([]byte{3}, 32)); err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, srv: srv}
	c.do("POST", "/v1/auth/register", map[string]string{"email": "me@example.org", "password": "tulips in the garden"}, nil)
	c.login("me@example.org", "tulips in the garden")
	st, enr := c.w("POST", "/v1/me/mfa/totp", nil)
	if st != 200 {
		t.Fatalf("enroll %d %v", st, enr)
	}
	code := auth.CurrentTOTP(enr["secret"].(string), time.Now())
	st, conf := c.w("POST", "/v1/me/mfa/totp/confirm", map[string]string{"code": code})
	if st != 200 || len(conf["recovery_codes"].([]any)) != 10 {
		t.Fatalf("confirm %d %v", st, conf)
	}
	st, out, hdr := (&client{t: t, srv: srv}).do("POST", "/v1/auth/login", map[string]string{"email": "me@example.org", "password": "tulips in the garden"}, nil)
	if st != 200 || out["mfa_required"] != true || hdr.Get("Set-Cookie") != "" {
		t.Fatalf("login with mfa: %d %v cookie=%q", st, out, hdr.Get("Set-Cookie"))
	}
	token := out["mfa_token"].(string)
	if st, _, _ := (&client{t: t, srv: srv}).do("POST", "/v1/auth/mfa", map[string]string{"mfa_token": token, "code": "000000"}, nil); st != 401 {
		t.Errorf("wrong code: %d", st)
	}
	recovery := conf["recovery_codes"].([]any)[0].(string)
	st, out, hdr = (&client{t: t, srv: srv}).do("POST", "/v1/auth/mfa", map[string]string{"mfa_token": token, "code": recovery}, nil)
	if st != 200 || hdr.Get("Set-Cookie") == "" || out["csrf_token"] == nil {
		t.Errorf("complete mfa: %d %v", st, out)
	}
	if st, _ := c.w("POST", "/v1/auth/password-reset", map[string]string{"email": "me@example.org"}); st != 503 {
		t.Errorf("reset without mailer: %d", st)
	}
}

func TestSessionsExportImportDelete(t *testing.T) {
	srv, authSvc := newServer(t)
	c := &client{t: t, srv: srv}
	c.do("POST", "/v1/auth/register", map[string]string{"email": "admin@example.org", "password": "tulips in the garden"}, nil)
	c.login("admin@example.org", "tulips in the garden")
	other := &client{t: t, srv: srv}
	other.login("admin@example.org", "tulips in the garden")

	// sessions: list shows both, mine is flagged; revoke the other one
	_, out, _ := c.do("GET", "/v1/me/sessions", nil, nil)
	list := out["_array"].([]any)
	if len(list) != 3 { // registration, and two logins
		t.Fatalf("sessions: %v", out)
	}
	var otherID string
	for _, e := range list {
		m := e.(map[string]any)
		if m["current"] != true {
			otherID = m["id"].(string)
			if st, _ := c.w("DELETE", "/v1/me/sessions/"+otherID, nil); st != 204 {
				t.Errorf("revoke: %d", st)
			}
		}
	}
	if st, _, _ := other.do("GET", "/v1/libraries", nil, nil); st != 401 {
		t.Errorf("revoked session still works: %d", st)
	}
	if st, _ := c.w("DELETE", "/v1/me/sessions/"+otherID, nil); st != 404 {
		t.Errorf("double revoke: %d", st)
	}

	// library export, then import as a new library
	_, lib := c.w("POST", "/v1/libraries", map[string]string{"name": "Home"})
	libID := lib["id"].(string)
	c.w("POST", "/v1/libraries/"+libID+"/scan", map[string]any{"isbn": "9782070612758"}, "Idempotency-Key", "scan-key-0001")
	req, _ := http.NewRequest("GET", srv.URL+"/v1/libraries/"+libID+"/export", nil)
	req.AddCookie(c.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	archive, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/x-tar" || len(archive) < 512 {
		t.Fatalf("export %d %s %d bytes", resp.StatusCode, resp.Header.Get("Content-Type"), len(archive))
	}
	req, _ = http.NewRequest("POST", srv.URL+"/v1/libraries/import", bytes.NewReader(archive))
	req.Header.Set("Content-Type", "application/x-tar")
	req.Header.Set("X-CSRF-Token", c.csrf)
	req.AddCookie(c.cookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var rep map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&rep)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || rep["instances"].(float64) != 1 {
		t.Errorf("import %d %v", resp.StatusCode, rep)
	}
	req, _ = http.NewRequest("POST", srv.URL+"/v1/me/import", strings.NewReader("garbage"))
	req.Header.Set("Content-Type", "application/x-tar")
	req.Header.Set("X-CSRF-Token", c.csrf)
	req.AddCookie(c.cookie)
	resp, _ = http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != 422 {
		t.Errorf("garbage import: %d", resp.StatusCode)
	}

	// administration: only admins; disabling signs the user out
	if _, err := authSvc.CreateUser(context.Background(), "bob@example.org", "tulips in the garden", "Bob", false); err != nil {
		t.Fatal(err)
	}
	bob := &client{t: t, srv: srv}
	bob.login("bob@example.org", "tulips in the garden")
	if st, _, _ := bob.do("GET", "/v1/admin/users", nil, nil); st != 404 {
		t.Errorf("non-admin listing users: %d", st)
	}
	_, users, _ := c.do("GET", "/v1/admin/users", nil, nil)
	var bobID string
	for _, u := range users["_array"].([]any) {
		if u.(map[string]any)["email"] == "bob@example.org" {
			bobID = u.(map[string]any)["id"].(string)
		}
	}
	if st, _ := c.w("PUT", "/v1/admin/users/"+bobID+"/disabled", map[string]bool{"disabled": true}); st != 204 {
		t.Errorf("disable: %d", st)
	}
	if st, _, _ := bob.do("GET", "/v1/libraries", nil, nil); st != 401 {
		t.Errorf("disabled user keeps access: %d", st)
	}
	if st, _ := c.w("POST", "/v1/admin/users/"+bobID+"/revoke-sessions", nil); st != 204 {
		t.Errorf("revoke sessions: %d", st)
	}

	// self-deletion needs the typed email and the password
	if st, _ := c.w("DELETE", "/v1/me", map[string]any{"confirm_email": "admin@example.org", "password": "wrong wrong wrong"}); st != 401 {
		t.Errorf("wrong password: %d", st)
	}
	if st, _ := c.w("DELETE", "/v1/me", map[string]any{"confirm_email": "someone@else.org", "password": "tulips in the garden"}); st != 401 {
		t.Errorf("wrong email: %d", st)
	}
	// the only active admin cannot leave
	if st, _ := c.w("DELETE", "/v1/me", map[string]any{"confirm_email": "admin@example.org", "password": "tulips in the garden"}); st != 409 {
		t.Errorf("last admin deletion: %d", st)
	}
	if st, _ := c.w("DELETE", "/v1/admin/users/"+bobID, nil); st != 204 {
		t.Errorf("admin deletes bob: %d", st)
	}
}

func TestHardcoverLinkIsOffWithoutKey(t *testing.T) {
	srv, _ := newServer(t) // no BIBLISANN_SECRET_KEY, so no Hardcover service
	c := &client{t: t, srv: srv}
	c.do("POST", "/v1/auth/register", map[string]string{"email": "me@example.org", "password": "tulips in the garden"}, nil)
	c.login("me@example.org", "tulips in the garden")
	_, out, _ := c.do("GET", "/v1/me/hardcover", nil, nil)
	if out["available"] != false || out["linked"] != false {
		t.Errorf("status: %v", out)
	}
	if st, _ := c.w("PUT", "/v1/me/hardcover", map[string]string{"mode": "both", "token": "x"}); st != 403 {
		t.Errorf("link without a service: %d", st)
	}
}
