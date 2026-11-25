package oauth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/oauth"
	"github.com/larueli/biblisann/internal/ratelimit"
)

const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk" // 43 chars

func challenge(v string) string {
	h := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func TestRevocationSerializesWithRefresh(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	a, _ := auth.NewService(pool, auth.RegistrationOpen)
	u, err := a.Register(ctx, "user@example.org", "tulips in the garden", "Reader")
	if err != nil {
		t.Fatal(err)
	}
	session, err := a.OpenSessionFor(ctx, u.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	svc := oauth.New(pool, "https://books.example.org")
	cid, err := svc.RegisterClient(ctx, "client", []string{"https://client.example.org/cb"})
	if err != nil {
		t.Fatal(err)
	}
	req := oauth.Request{ClientID: cid, RedirectURI: "https://client.example.org/cb", CodeChallenge: challenge(verifier), CodeChallengeMethod: "S256", Scope: "read", Resource: "https://books.example.org/mcp"}
	grant := func() oauth.TokenResponse {
		t.Helper()
		redirect, err := svc.Approve(ctx, u.ID, session.Token, req, []string{"read"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		parsed, _ := url.Parse(redirect)
		tokens, err := svc.ExchangeCode(ctx, cid, parsed.Query().Get("code"), req.RedirectURI, verifier, req.Resource)
		if err != nil {
			t.Fatal(err)
		}
		return tokens
	}
	tokens := grant()
	// Hold the refresh row so rotation can take the user lock but cannot issue a descendant yet.
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(ctx) }()
	hash := sha256.Sum256([]byte(tokens.RefreshToken))
	if _, err := blocker.Exec(ctx, "SELECT token_hash FROM oauth_refresh_tokens WHERE token_hash = $1 FOR UPDATE", hash[:]); err != nil {
		t.Fatal(err)
	}
	type refreshResult struct {
		tokens oauth.TokenResponse
		err    error
	}
	rotated := make(chan refreshResult, 1)
	go func() { tr, err := svc.Refresh(ctx, cid, tokens.RefreshToken); rotated <- refreshResult{tr, err} }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, "SELECT id FROM users WHERE id = $1 FOR UPDATE NOWAIT", u.ID)
		_ = tx.Rollback(ctx)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("refresh did not acquire the user lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	revoked := make(chan error, 1)
	go func() { revoked <- svc.RevokeAll(ctx, u.ID) }()
	select {
	case err := <-revoked:
		t.Fatalf("revocation finished before rotation: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result := <-rotated
	if result.err != nil {
		t.Fatal(result.err)
	}
	if err := <-revoked; err != nil {
		t.Fatal(err)
	}
	if p, err := svc.Resolve(ctx, result.tokens.AccessToken); err != nil || p != nil {
		t.Errorf("rotated access token survived revocation: %v", err)
	}
	if _, err := svc.Refresh(ctx, cid, result.tokens.RefreshToken); !errors.Is(err, oauth.ErrInvalidGrant) {
		t.Errorf("rotated refresh survived revocation: %v", err)
	}

	// Even when no refresh token exists, revocation consumes a pending authorization code.
	redirect, err := svc.Approve(ctx, u.ID, session.Token, req, []string{"read"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(redirect)
	if err := svc.RevokeGrant(ctx, u.ID, cid); !errors.Is(err, oauth.ErrInvalidClient) {
		t.Errorf("empty grant: %v", err)
	}
	if _, err := svc.ExchangeCode(ctx, cid, parsed.Query().Get("code"), req.RedirectURI, verifier, req.Resource); !errors.Is(err, oauth.ErrInvalidGrant) {
		t.Errorf("pending code survived revocation: %v", err)
	}
	if err := a.Logout(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(ctx, u.ID, session.Token, req, []string{"read"}, nil); !errors.Is(err, oauth.ErrInvalidGrant) {
		t.Errorf("revoked session approved consent: %v", err)
	}
}

func post(t *testing.T, srv *httptest.Server, path string, form url.Values) (int, map[string]any) {
	t.Helper()
	resp, err := http.PostForm(srv.URL+path, form)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestAuthorizationCodeFlow(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	a, _ := auth.NewService(pool, auth.RegistrationOpen)
	u, _ := a.Register(ctx, "me@example.org", "tulips in the garden", "Me")
	session, err := a.OpenSessionFor(ctx, u.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	svc := oauth.New(pool, "https://books.example.org")
	mux := http.NewServeMux()
	svc.Routes(mux, ratelimit.NewMemory(100, time.Second), func(*http.Request) string { return "ip" })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// discovery
	resp, _ := http.Get(srv.URL + "/.well-known/oauth-authorization-server")
	var meta map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&meta)
	_ = resp.Body.Close()
	if meta["token_endpoint"] != "https://books.example.org/oauth/token" || meta["code_challenge_methods_supported"].([]any)[0] != "S256" {
		t.Errorf("metadata: %v", meta)
	}

	// registration: bad redirect URIs are refused
	if _, err := svc.RegisterClient(ctx, "evil", []string{"javascript:alert(1)"}); err == nil {
		t.Error("javascript: redirect accepted")
	}
	if _, err := svc.RegisterClient(ctx, "evil", []string{"http://evil.example/cb"}); err == nil {
		t.Error("plain http redirect to a remote host accepted")
	}
	cid, err := svc.RegisterClient(ctx, "Claude", []string{"https://client.example/cb", "http://127.0.0.1:7777/cb"})
	if err != nil {
		t.Fatal(err)
	}
	req := oauth.Request{ClientID: cid, RedirectURI: "https://client.example/cb", CodeChallenge: challenge(verifier), CodeChallengeMethod: "S256",
		State: "st", Scope: "read write", Resource: "https://books.example.org/mcp"}

	// validation
	bad := req
	bad.RedirectURI = "https://client.example/other"
	if _, _, err := svc.Validate(ctx, bad); !errors.Is(err, oauth.ErrInvalidClient) {
		t.Errorf("redirect mismatch: %v", err)
	}
	bad = req
	bad.CodeChallengeMethod = "plain"
	if _, _, err := svc.Validate(ctx, bad); err == nil {
		t.Error("plain PKCE accepted")
	}
	bad = req
	bad.Resource = "https://elsewhere.example/mcp"
	if _, _, err := svc.Validate(ctx, bad); err == nil {
		t.Error("foreign resource accepted")
	}
	bad = req
	bad.Scope = "read admin"
	if _, _, err := svc.Validate(ctx, bad); !errors.Is(err, oauth.ErrInvalidScope) {
		t.Errorf("unknown scope: %v", err)
	}

	// consent: the user grants read only, although write was requested
	lib := uuid.New()
	redirect, err := svc.Approve(ctx, u.ID, session.Token, req, []string{"read"}, []uuid.UUID{lib})
	if err != nil {
		t.Fatal(err)
	}
	ru, _ := url.Parse(redirect)
	code := ru.Query().Get("code")
	if code == "" || ru.Query().Get("state") != "st" || ru.Query().Get("iss") != "https://books.example.org" {
		t.Fatalf("redirect: %s", redirect)
	}
	if !strings.HasPrefix(svc.Deny(req), "https://client.example/cb?error=access_denied") {
		t.Errorf("deny: %s", svc.Deny(req))
	}

	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {cid}, "code": {code}, "redirect_uri": {req.RedirectURI},
		"code_verifier": {"wrong-verifier-wrong-verifier-wrong-verifier-123"}}
	if st, out := post(t, srv, "/oauth/token", form); st != 400 || out["error"] != "invalid_grant" {
		t.Errorf("wrong verifier: %d %v", st, out)
	}
	// the code is burned by the failed attempt: even the right verifier fails now
	form.Set("code_verifier", verifier)
	if st, _ := post(t, srv, "/oauth/token", form); st != 400 {
		t.Errorf("code reuse after failure: %d", st)
	}

	redirect, _ = svc.Approve(ctx, u.ID, session.Token, req, []string{"read"}, []uuid.UUID{lib})
	ru, _ = url.Parse(redirect)
	form.Set("code", ru.Query().Get("code"))
	st, tok := post(t, srv, "/oauth/token", form)
	if st != 200 || tok["scope"] != "read" || tok["token_type"] != "Bearer" {
		t.Fatalf("token: %d %v", st, tok)
	}
	if st, _ := post(t, srv, "/oauth/token", form); st != 400 {
		t.Errorf("authorization code used twice: %d", st)
	}

	// the access token resolves to a limited, MCP-bound principal
	p, err := svc.Resolve(ctx, tok["access_token"].(string))
	if err != nil || p == nil || p.Audience != "mcp" || p.HasScope("write") || !p.AllowsLibrary(lib) || p.AllowsLibrary(uuid.New()) {
		t.Fatalf("principal: %+v %v", p, err)
	}
	if p, _ := svc.Resolve(ctx, "bb_oat_nope"); p != nil {
		t.Error("unknown token resolved")
	}

	// refresh tokens rotate; the old one dies
	rf := url.Values{"grant_type": {"refresh_token"}, "client_id": {cid}, "refresh_token": {tok["refresh_token"].(string)}}
	st, tok2 := post(t, srv, "/oauth/token", rf)
	if st != 200 || tok2["refresh_token"] == tok["refresh_token"] {
		t.Fatalf("refresh: %d %v", st, tok2)
	}
	if st, _ := post(t, srv, "/oauth/token", rf); st != 400 {
		t.Errorf("rotated refresh token reused: %d", st)
	}
	// Reuse reveals a copy: the whole family is dead, including the newest tokens.
	if p, _ := svc.Resolve(ctx, tok2["access_token"].(string)); p != nil {
		t.Error("the access token of a compromised family must stop working")
	}
	good := url.Values{"grant_type": {"refresh_token"}, "client_id": {cid}, "refresh_token": {tok2["refresh_token"].(string)}}
	if st, _ := post(t, srv, "/oauth/token", good); st != 400 {
		t.Errorf("the newest refresh token of a compromised family must be refused: %d", st)
	}
	grants, _ := svc.Grants(ctx, u.ID)
	if len(grants) != 0 {
		t.Errorf("a revoked family must not stay listed as an authorized app: %+v", grants)
	}
	rf.Set("client_id", "bbc_other")
	rf.Set("refresh_token", tok2["refresh_token"].(string))
	if st, _ := post(t, srv, "/oauth/token", rf); st != 400 {
		t.Errorf("refresh with another client: %d", st)
	}
	if st, out := post(t, srv, "/oauth/token", url.Values{"grant_type": {"password"}}); st != 400 || out["error"] != "unsupported_grant_type" {
		t.Errorf("password grant: %d %v", st, out)
	}
}
