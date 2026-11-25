package api_test

import (
	"testing"

	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

func TestCatalogAdministration(t *testing.T) {
	srv, authSvc := newServer(t)
	admin := &client{t: t, srv: srv}
	admin.do("POST", "/v1/auth/register", map[string]string{"email": "admin@example.org", "password": "tulips in the garden"}, nil)
	admin.login("admin@example.org", "tulips in the garden")
	_, lib := admin.w("POST", "/v1/libraries", map[string]any{"name": "Home"})
	libID := lib["id"].(string)
	_, r := admin.w("POST", "/v1/libraries/"+libID+"/scan", map[string]any{"isbn": "9782070612758"}, "Idempotency-Key", "scan-key-0001")
	ed := r["edition"].(map[string]any)["id"].(string)
	path := "/v1/editions/" + ed

	// a manual edit locks the field; a refresh keeps it and fills the rest
	st, out := admin.w("PATCH", path, map[string]any{"title": "Hand-made title", "pages": 120})
	if st != 200 || out["title"] != "Hand-made title" {
		t.Fatalf("edit: %d %v", st, out)
	}
	if locked := out["locked_fields"].([]any); len(locked) != 2 {
		t.Errorf("locked fields: %v", locked)
	}
	st, out = admin.w("POST", path+"/refresh", nil)
	if st != 200 || out["title"] != "Hand-made title" || out["pages"].(float64) != 120 || out["language"] != "fr" {
		t.Fatalf("refresh keeps manual fields: %d %v", st, out)
	}

	// an excluded edition is never refreshed
	if st, out = admin.w("PUT", path+"/sync-excluded", map[string]any{"excluded": true}); st != 200 || out["sync_excluded"] != true {
		t.Fatalf("exclude: %d %v", st, out)
	}
	if st, _ = admin.w("POST", path+"/refresh", nil); st != 409 {
		t.Errorf("refresh of an excluded edition: %d", st)
	}

	// a book no provider knows is a normal outcome, and the exclusion still applies to it
	_, unknown := admin.w("POST", "/v1/editions", map[string]any{"isbn": "9780306406157", "title": "Obscure pamphlet", "authors": []string{"Nobody"}})
	unknownPath := "/v1/editions/" + unknown["id"].(string)
	st, out = admin.w("POST", unknownPath+"/refresh", nil)
	if st != 200 || out["sync_status"] != "not_found" || out["title"] != "Obscure pamphlet" || out["synced_at"] == nil {
		t.Fatalf("refresh of an unknown ISBN: %d %v", st, out)
	}
	_, noisbn := admin.w("POST", "/v1/editions", map[string]any{"title": "Hand-written notebook"})
	if st, out = admin.w("POST", "/v1/editions/"+noisbn["id"].(string)+"/refresh", nil); st != 200 || out["sync_status"] != "no_isbn" {
		t.Fatalf("refresh without ISBN: %d %v", st, out)
	}
	admin.w("PUT", unknownPath+"/sync-excluded", map[string]any{"excluded": true})
	if st, _ = admin.w("POST", unknownPath+"/refresh", nil); st != 409 {
		t.Errorf("refresh of an excluded unknown book: %d", st)
	}

	// still held by a copy: cannot be deleted
	if st, _ = admin.w("DELETE", path, nil); st != 409 {
		t.Errorf("delete of a held edition: %d", st)
	}

	// ordinary members get a 404, like every administration route
	if _, err := authSvc.CreateUser(t.Context(), "bob@example.org", "bobs long password", "Bob", false); err != nil {
		t.Fatal(err)
	}
	bob := &client{t: t, srv: srv}
	bob.login("bob@example.org", "bobs long password")
	if st, _ := bob.w("PATCH", path, map[string]any{"title": "x"}); st != 404 {
		t.Errorf("non-admin edit: %d", st)
	}
}

func TestCatalogRefreshIsAdministratorOnly(t *testing.T) {
	srv, authSvc := newServer(t)
	admin := &client{t: t, srv: srv}
	admin.do("POST", "/v1/auth/register", map[string]string{"email": "admin@example.org", "password": "tulips in the garden"}, nil)
	admin.login("admin@example.org", "tulips in the garden")
	if _, err := authSvc.CreateUser(t.Context(), "bob@example.org", "bobs long password", "Bob", false); err != nil {
		t.Fatal(err)
	}
	bob := &client{t: t, srv: srv}
	bob.login("bob@example.org", "bobs long password")
	if st, _ := bob.w("POST", "/v1/admin/catalog/refresh", map[string]any{}); st != 404 {
		t.Errorf("non-admin: %d", st)
	}
	// the test server runs no job queue: an administrator is told so instead of a silent success
	if st, _ := admin.w("POST", "/v1/admin/catalog/refresh", map[string]any{"all": true}); st != 503 {
		t.Errorf("admin without a queue: %d", st)
	}
}

func TestMembersCanRefreshWhatTheirLibraryHolds(t *testing.T) {
	srv, authSvc, pool := newServerWithPool(t)
	owner := &client{t: t, srv: srv}
	owner.do("POST", "/v1/auth/register", map[string]string{"email": "owner@example.org", "password": "tulips in the garden"}, nil)
	owner.login("owner@example.org", "tulips in the garden")
	_, lib := owner.w("POST", "/v1/libraries", map[string]any{"name": "Home"})
	libID := lib["id"].(string)
	_, r := owner.w("POST", "/v1/libraries/"+libID+"/scan", map[string]any{"isbn": "9782070612758"}, "Idempotency-Key", "scan-key-0001")
	edition := "/v1/editions/" + r["edition"].(map[string]any)["id"].(string)

	roles := map[string]string{"editor": "edit", "reader": "read", "libadmin": "admin"}
	people := map[string]*client{}
	for name, role := range roles {
		email := name + "@example.org"
		user, err := authSvc.CreateUser(t.Context(), email, "a long password here", name, false)
		if err != nil {
			t.Fatal(err)
		}
		if n, err := dbsqlc.New(pool).VerifyUserEmail(t.Context(), dbsqlc.VerifyUserEmailParams{ID: user.ID, Email: email}); err != nil || n != 1 {
			t.Fatalf("verify fixture address: %d %v", n, err)
		}
		if st, _ := owner.w("PUT", "/v1/libraries/"+libID+"/sharing/users", map[string]any{"email": email, "grant": map[string]any{"role": role}}); st != 200 {
			t.Fatalf("share with %s: %d", name, st)
		}
		c := &client{t: t, srv: srv}
		c.login(email, "a long password here")
		people[name] = c
	}
	if _, err := authSvc.CreateUser(t.Context(), "stranger@example.org", "a long password here", "stranger", false); err != nil {
		t.Fatal(err)
	}
	stranger := &client{t: t, srv: srv}
	stranger.login("stranger@example.org", "a long password here")

	// one book: editors and library administrators may, readers and strangers may not
	for name, want := range map[string]int{"editor": 200, "libadmin": 200, "reader": 404} {
		if st, out := people[name].w("POST", edition+"/refresh", nil); st != want {
			t.Errorf("%s refreshes one book: %d %v", name, st, out)
		}
	}
	if st, _ := stranger.w("POST", edition+"/refresh", nil); st != 404 {
		t.Errorf("stranger refreshes one book: %d", st)
	}

	// a whole library (the test server runs no queue: 503 means the permission check passed)
	path := "/v1/libraries/" + libID + "/catalog/refresh"
	for name, want := range map[string]int{"editor": 503, "libadmin": 503, "reader": 403} {
		if st, _ := people[name].w("POST", path, map[string]any{}); st != want {
			t.Errorf("%s refreshes the library: %d, want %d", name, st, want)
		}
	}
	if st, _ := stranger.w("POST", path, map[string]any{}); st != 404 {
		t.Errorf("stranger refreshes the library: %d", st)
	}
	// including books providers already described is for library administrators
	if st, _ := people["editor"].w("POST", path, map[string]any{"all": true}); st != 403 {
		t.Errorf("editor asks for all: %d", st)
	}
	if st, _ := people["libadmin"].w("POST", path, map[string]any{"all": true}); st != 503 {
		t.Errorf("library admin asks for all: %d", st)
	}
	// the whole server stays an instance administrator's
	if st, _ := people["libadmin"].w("POST", "/v1/admin/catalog/refresh", map[string]any{}); st != 404 {
		t.Errorf("library admin refreshes the server: %d", st)
	}
}
