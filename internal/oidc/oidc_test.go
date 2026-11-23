package oidc_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/larueli/biblisann/internal/auth"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/oidc"
)

// idp is a minimal OpenID provider: discovery, JWKS and a token endpoint returning a signed ID
// token whose claims the test sets.
type idp struct {
	srv    *httptest.Server
	key    *rsa.PrivateKey
	claims map[string]any
	nonce  string
}

func newIDP(t *testing.T) *idp {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &idp{key: key}
	mux := http.NewServeMux()
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": p.srv.URL, "authorization_endpoint": p.srv.URL + "/auth",
			"token_endpoint": p.srv.URL + "/token", "jwks_uri": p.srv.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("code_verifier") == "" {
			http.Error(w, "PKCE required", 400)
			return
		}
		sig, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
		c := map[string]any{"iss": p.srv.URL, "aud": "client1", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": p.nonce}
		for k, v := range p.claims {
			c[k] = v
		}
		raw, _ := jwt.Signed(sig).Claims(c).Serialize()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": raw})
	})
	return p
}

// login runs Begin then Complete, echoing the nonce the service put in the authorization URL.
func (p *idp) login(t *testing.T, s *oidc.Service) (string, error) {
	t.Helper()
	ctx := context.Background()
	u, _, err := s.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(u)
	q := parsed.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatalf("PKCE missing in %s", u)
	}
	p.nonce = q.Get("nonce")
	uid, err := s.Complete(ctx, "code", q.Get("state"))
	return uid.String(), err
}

func TestOIDC(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	a, _ := auth.NewService(pool, auth.RegistrationClosed)
	idp := newIDP(t)
	svc := oidc.New(oidc.Config{Issuer: idp.srv.URL, ClientID: "client1", ClientSecret: "s", RedirectURL: "http://x/cb",
		AdminGroup: "library-admins", AutoCreate: true}, pool, a, http.DefaultClient)
	q := dbsqlc.New(pool)

	// unverified email: refused, no account created
	idp.claims = map[string]any{"sub": "u1", "email": "new@example.org", "email_verified": false}
	if _, err := idp.login(t, svc); !errors.Is(err, oidc.ErrUnverifiedEmail) {
		t.Fatalf("unverified: %v", err)
	}
	if n, _ := q.CountUsers(ctx); n != 0 {
		t.Error("account created for an unverified email")
	}

	// verified email: account created, groups mirrored, admin group applied
	idp.claims = map[string]any{"sub": "u1", "email": "new@example.org", "email_verified": true, "name": "New",
		"groups": []string{"family", "library-admins"}}
	id1, err := idp.login(t, svc)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := q.GetUserByEmail(ctx, "new@example.org")
	if !u.IsAdmin || u.PasswordHash != nil || u.ID.String() != id1 {
		t.Errorf("user: %+v", u)
	}
	gs, _ := q.ListExternalGroupsOfUser(ctx, u.ID)
	if len(gs) != 2 {
		t.Errorf("groups: %+v", gs)
	}

	// leaving a group and the admin group removes them on the next sign-in; same identity, new email claim ignored
	idp.claims = map[string]any{"sub": "u1", "email": "other@example.org", "email_verified": true, "groups": []string{"family"}}
	id2, err := idp.login(t, svc)
	if err != nil || id2 != id1 {
		t.Fatalf("second login: %v %s", err, id2)
	}
	u, _ = q.GetUser(ctx, u.ID)
	gs, _ = q.ListExternalGroupsOfUser(ctx, u.ID)
	if u.IsAdmin || len(gs) != 1 || gs[0].Name != "family" {
		t.Errorf("after sync: admin=%v groups=%+v", u.IsAdmin, gs)
	}

	// linking: a local account with that verified email gets the identity attached
	local, _ := a.CreateUser(ctx, "local@example.org", "tulips in the garden", "Local", false)
	idp.claims = map[string]any{"sub": "u2", "email": "LOCAL@example.org", "email_verified": true}
	if id, err := idp.login(t, svc); err != nil || id != local.ID.String() {
		t.Errorf("link: %v %s", err, id)
	}

	// a different issuer with the same subject is a different identity: no auto-linking without verified email
	svc2 := oidc.New(oidc.Config{Issuer: idp.srv.URL, ClientID: "client1", RedirectURL: "http://x/cb", AutoCreate: false}, pool, a, http.DefaultClient)
	idp.claims = map[string]any{"sub": "u3", "email": "nobody@example.org", "email_verified": true}
	if _, err := idp.login(t, svc2); !errors.Is(err, oidc.ErrNotAllowed) {
		t.Errorf("auto-create off: %v", err)
	}

	// replayed or forged state, wrong nonce
	if _, err := svc.Complete(ctx, "code", "forged"); !errors.Is(err, oidc.ErrBadFlow) {
		t.Errorf("forged state: %v", err)
	}
	u3, _, _ := svc.Begin(ctx)
	pu, _ := url.Parse(u3)
	idp.nonce = "wrong-nonce"
	idp.claims = map[string]any{"sub": "u1", "email": "x@example.org", "email_verified": true}
	if _, err := svc.Complete(ctx, "code", pu.Query().Get("state")); !errors.Is(err, oidc.ErrBadFlow) {
		t.Errorf("wrong nonce: %v", err)
	}
	_ = strings.TrimSpace
}
