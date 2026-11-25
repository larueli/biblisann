package api_test

import "testing"

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
