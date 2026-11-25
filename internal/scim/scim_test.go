package scim_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/scim"
	"github.com/larueli/biblisann/internal/tokens"
)

type env struct {
	t     *testing.T
	srv   *httptest.Server
	token string
}

func (e *env) do(method, path string, body any) (int, map[string]any) {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	req.Header.Set("Content-Type", "application/scim+json")
	if e.token != "" {
		req.Header.Set("Authorization", "Bearer "+e.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func newEnv(t *testing.T) *env {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	a, _ := auth.NewService(pool, auth.RegistrationOpen)
	admin, _ := a.Register(ctx, "admin@example.org", "tulips in the garden", "Admin")
	tk := tokens.NewService(pool)
	c, err := tk.Create(authz.With(ctx, &authz.Principal{UserID: admin.ID, IsAdmin: true, Via: "session"}), tokens.KindSCIM, "idp", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	scim.New(pool, tk, "https://books.example.org").Routes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv, token: c.Token}
}

func TestSCIMUsersAndGroups(t *testing.T) {
	e := newEnv(t)
	saved := e.token
	e.token = ""
	if st, _ := e.do("GET", "/scim/v2/Users", nil); st != 401 {
		t.Errorf("anonymous: %d", st)
	}
	e.token = "bb_scim_wrong"
	if st, _ := e.do("GET", "/scim/v2/Users", nil); st != 401 {
		t.Errorf("bad token: %d", st)
	}
	e.token = saved

	st, u := e.do("POST", "/scim/v2/Users", map[string]any{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
		"userName": "Marie@Example.org", "displayName": "Marie", "active": true})
	if st != 201 || u["userName"] != "Marie@Example.org" {
		t.Fatalf("create user %d %v", st, u)
	}
	id := u["id"].(string)
	if st, _ := e.do("POST", "/scim/v2/Users", map[string]any{"userName": "marie@example.org"}); st != 409 {
		t.Errorf("duplicate: %d", st)
	}
	if st, _ := e.do("POST", "/scim/v2/Users", map[string]any{"userName": "not-an-email"}); st != 400 {
		t.Errorf("bad userName: %d", st)
	}
	if st, inactive := e.do("POST", "/scim/v2/Users", map[string]any{"userName": "inactive@example.org", "active": false}); st != 201 || inactive["active"] != false {
		t.Errorf("create inactive user: %d %v", st, inactive)
	}
	st, list := e.do("GET", `/scim/v2/Users?filter=userName%20eq%20%22marie%40example.org%22`, nil)
	if st != 200 || list["totalResults"].(float64) != 1 {
		t.Errorf("filter %d %v", st, list)
	}
	if st, _ := e.do("GET", `/scim/v2/Users?filter=name.givenName%20co%20%22x%22`, nil); st != 400 {
		t.Errorf("unsupported filter: %d", st)
	}
	st, u = e.do("PATCH", "/scim/v2/Users/"+id, map[string]any{"Operations": []map[string]any{{"op": "replace", "path": "active", "value": false}}})
	if st != 200 || u["active"] != false {
		t.Errorf("deactivate %d %v", st, u)
	}

	st, g := e.do("POST", "/scim/v2/Groups", map[string]any{"displayName": "Family", "members": []map[string]string{{"value": id}}})
	if st != 201 || len(g["members"].([]any)) != 1 {
		t.Fatalf("create group %d %v", st, g)
	}
	gid := g["id"].(string)
	st, g = e.do("PATCH", "/scim/v2/Groups/"+gid, map[string]any{"Operations": []map[string]any{{"op": "remove", "path": `members[value eq "` + id + `"]`}}})
	if st != 200 || g["members"] != nil {
		t.Errorf("remove member %d %v", st, g)
	}
	if st, _ := e.do("PATCH", "/scim/v2/Groups/"+gid, map[string]any{"Operations": []map[string]any{{"op": "add", "path": "members", "value": []map[string]string{{"value": "00000000-0000-0000-0000-000000000000"}}}}}); st != 400 {
		t.Errorf("unknown member: %d", st)
	}
	if st, _ := e.do("DELETE", "/scim/v2/Groups/"+gid, nil); st != 204 {
		t.Errorf("delete group: %d", st)
	}
	if st, _ := e.do("DELETE", "/scim/v2/Users/"+id, nil); st != 204 {
		t.Errorf("delete user: %d", st)
	}
	if st, u := e.do("GET", "/scim/v2/Users/"+id, nil); st != 200 || u["active"] != false {
		t.Errorf("deleted user stays as an inactive account: %d %v", st, u)
	}
	if st, _ := e.do("GET", "/scim/v2/ServiceProviderConfig", nil); st != 200 {
		t.Errorf("config: %d", st)
	}
}

func TestSCIMTokenNeedsAdmin(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	a, _ := auth.NewService(pool, auth.RegistrationOpen)
	u, _ := a.Register(ctx, "u@example.org", "tulips in the garden", "U")
	if _, err := tokens.NewService(pool).Create(authz.With(ctx, &authz.Principal{UserID: u.ID, Via: "session"}), tokens.KindSCIM, "x", nil, nil, nil); err == nil {
		t.Error("non-admin created a SCIM token")
	}
}
