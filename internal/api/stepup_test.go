package api_test

import (
	"testing"
)

func TestStepUpGuardsSensitiveActions(t *testing.T) {
	srv, _ := newServer(t)
	c := &client{t: t, srv: srv}
	c.do("POST", "/v1/auth/register", map[string]string{"email": "me@example.org", "password": "tulips in the garden"}, nil)
	c.login("me@example.org", "tulips in the garden")

	sensitive := []struct {
		method, path string
		body         map[string]any
	}{
		{"POST", "/v1/me/tokens", map[string]any{"name": "x"}},
		{"POST", "/v1/me/mfa/totp", nil},
		{"DELETE", "/v1/me/mfa/totp", map[string]any{"password": "x"}},
		{"POST", "/v1/auth/password", map[string]any{"current": "x", "new": "x"}},
		{"DELETE", "/v1/me", map[string]any{"confirm_email": "me@example.org"}},
	}
	for _, e := range sensitive {
		st, out := c.w(e.method, e.path, e.body)
		if st != 403 || out["detail"] != "step_up_required" {
			t.Errorf("%s %s without step-up: %d %v", e.method, e.path, st, out)
		}
	}

	if st, _ := c.w("POST", "/v1/auth/step-up", map[string]string{"password": "wrong wrong wrong"}); st != 401 {
		t.Errorf("wrong password: %d", st)
	}
	c.stepUp()
	st, out := c.w("GET", "/v1/auth/session", nil)
	if st != 200 || out["step_up_until"] == nil {
		t.Errorf("session after step-up: %d %v", st, out)
	}
	if st, out := c.w("POST", "/v1/me/tokens", map[string]any{"name": "script", "scopes": []string{"read"}}); st != 201 {
		t.Errorf("token after step-up: %d %v", st, out)
	}

	// a second session does not inherit the window
	other := &client{t: t, srv: srv}
	other.login("me@example.org", "tulips in the garden")
	if st, out := other.w("POST", "/v1/me/tokens", map[string]any{"name": "again"}); st != 403 || out["detail"] != "step_up_required" {
		t.Errorf("other session: %d %v", st, out)
	}
}

func TestAdminSetsPasswordAndSSOUsersCannot(t *testing.T) {
	srv, authSvc := newServer(t)
	admin := &client{t: t, srv: srv}
	admin.do("POST", "/v1/auth/register", map[string]string{"email": "admin@example.org", "password": "tulips in the garden"}, nil)
	admin.login("admin@example.org", "tulips in the garden")
	bob, err := authSvc.CreateUser(t.Context(), "bob@example.org", "first password here", "Bob", false)
	if err != nil {
		t.Fatal(err)
	}

	path := "/v1/admin/users/" + bob.ID.String() + "/password"
	if st, out := admin.w("PUT", path, map[string]string{"password": "a brand new passphrase"}); st != 403 || out["detail"] != "step_up_required" {
		t.Fatalf("without step-up: %d %v", st, out)
	}
	admin.stepUp()
	if st, out := admin.w("PUT", path, map[string]string{"password": "short"}); st != 422 {
		t.Errorf("weak password: %d %v", st, out)
	}
	if st, out := admin.w("PUT", path, map[string]string{"password": "a brand new passphrase"}); st != 204 {
		t.Fatalf("set password: %d %v", st, out)
	}
	b := &client{t: t, srv: srv}
	b.login("bob@example.org", "a brand new passphrase")

	// a non-admin gets a 404 on the admin route
	b.stepUp()
	if st, _ := b.w("PUT", "/v1/admin/users/"+bob.ID.String()+"/password", map[string]string{"password": "another new passphrase"}); st != 404 {
		t.Errorf("non-admin: %d", st)
	}
}
