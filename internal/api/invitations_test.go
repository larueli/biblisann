package api_test

import (
	"strings"
	"testing"

	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

func TestInvitationFlow(t *testing.T) {
	srv, authSvc, pool := newServerWithPool(t)
	admin := &client{t: t, srv: srv}
	admin.do("POST", "/v1/auth/register", map[string]string{"email": "admin@example.org", "password": "tulips in the garden"}, nil)
	admin.login("admin@example.org", "tulips in the garden")
	_, lib := admin.w("POST", "/v1/libraries", map[string]any{"name": "Home"})
	share := "/v1/libraries/" + lib["id"].(string) + "/sharing/users"
	body := map[string]any{"email": "newcomer@example.org", "grant": map[string]any{"role": "edit"}}

	// the instance administrator is asked to confirm; nothing is created before
	if st, out := admin.w("PUT", share, body); st != 409 || out["detail"] != "no_account" {
		t.Fatalf("unknown address: %d %v", st, out)
	}
	body["invite"] = true
	if st, out := admin.w("PUT", share, body); st != 403 || out["detail"] != "step_up_required" {
		t.Fatalf("invitation without step-up: %d %v", st, out)
	}
	admin.stepUp()
	st, out := admin.w("PUT", share, body)
	if st != 200 || out["status"] != "invited" {
		t.Fatalf("invite: %d %v", st, out)
	}
	link := out["invitation_url"].(string)
	token := link[strings.Index(link, "token=")+len("token="):]

	// a library administrator who is not an instance administrator learns nothing
	bobUser, err := authSvc.CreateUser(t.Context(), "bob@example.org", "bobs long password", "Bob", false)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := dbsqlc.New(pool).VerifyUserEmail(t.Context(), dbsqlc.VerifyUserEmailParams{ID: bobUser.ID, Email: bobUser.Email}); err != nil || n != 1 {
		t.Fatalf("verify fixture address: %d %v", n, err)
	}
	if st, _ := admin.w("PUT", share, map[string]any{"email": "bob@example.org", "grant": map[string]any{"role": "admin"}}); st != 200 {
		t.Fatalf("share with bob: %d", st)
	}
	bob := &client{t: t, srv: srv}
	bob.login("bob@example.org", "bobs long password")
	if st, out := bob.w("PUT", share, map[string]any{"email": "ghost@example.org", "grant": map[string]any{"role": "read"}, "invite": true}); st != 200 || out["status"] != "ok" || out["invitation_url"] != nil {
		t.Errorf("non-instance admin, unknown address: %d %v", st, out)
	}

	// the invitation page, then registration (closed registration does not matter)
	anon := &client{t: t, srv: srv}
	if st, out, _ := anon.do("GET", "/v1/invitations/"+token, nil, nil); st != 200 || out["email"] != "newcomer@example.org" || out["library_name"] != "Home" {
		t.Fatalf("peek: %d %v", st, out)
	}
	if st, out, _ := anon.do("POST", "/v1/auth/register", map[string]string{"email": "other@example.org", "password": "plain registration"}, nil); st != 403 {
		t.Fatalf("closed registration: %d %v", st, out)
	}
	st, out, _ = anon.do("POST", "/v1/auth/register", map[string]string{"email": "ignored@example.org", "password": "a fine passphrase", "invitation_token": token}, nil)
	if st != 201 {
		t.Fatalf("register with invitation: %d %v", st, out)
	}
	newcomer := &client{t: t, srv: srv}
	newcomer.login("newcomer@example.org", "a fine passphrase")
	if _, libs, _ := newcomer.do("GET", "/v1/libraries", nil, nil); len(libs["_array"].([]any)) != 1 {
		t.Errorf("invited user libraries: %v", libs)
	}
	if st, _, _ := anon.do("GET", "/v1/invitations/"+token, nil, nil); st != 404 {
		t.Errorf("used invitation: %d", st)
	}
}
