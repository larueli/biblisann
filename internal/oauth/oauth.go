// Package oauth is the OAuth 2.1 authorization server used by MCP clients: authorization code
// with mandatory PKCE (S256), dynamic client registration (RFC 7591) for public clients,
// rotating refresh tokens, resource indicators (RFC 8707) and the discovery documents of
// RFC 8414 and RFC 9728. Access tokens are opaque, hashed at rest, short-lived and bound to the
// MCP resource; they cannot be used on the REST API.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/larueli/biblisann/internal/authz"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

// Lifetimes and limits.
const (
	AccessTTL   = time.Hour
	RefreshTTL  = 30 * 24 * time.Hour
	codeTTL     = 10 * time.Minute
	maxClients  = 2000
	accessPref  = "bb_oat_"
	refreshPref = "bb_ort_"
)

// OAuth errors (RFC 6749 error codes).
var (
	ErrInvalidRequest = errors.New("invalid_request")
	ErrInvalidClient  = errors.New("invalid_client")
	ErrInvalidGrant   = errors.New("invalid_grant")
	ErrInvalidScope   = errors.New("invalid_scope")
	ErrTooManyClients = errors.New("too many registered clients")
)

// Service is the authorization server.
type Service struct {
	pool     *pgxpool.Pool
	q        *dbsqlc.Queries
	base     string
	resource string
	now      func() time.Time
}

// New creates the service; base is the public origin (BASE_URL without trailing slash).
func New(pool *pgxpool.Pool, base string) *Service {
	base = strings.TrimSuffix(base, "/")
	return &Service{pool: pool, q: dbsqlc.New(pool), base: base, resource: base + "/mcp", now: time.Now}
}

// Issuer is the authorization server identifier.
func (s *Service) Issuer() string { return s.base }

// Resource is the protected resource identifier (the MCP endpoint).
func (s *Service) Resource() string { return s.resource }

func randToken(prefix string, n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func hash(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }

var schemeRe = regexp.MustCompile(`^[a-z][a-z0-9+.-]*$`)

var forbiddenSchemes = map[string]bool{"javascript": true, "data": true, "file": true, "vbscript": true, "about": true, "blob": true}

// validRedirect accepts https, loopback http (native clients, RFC 8252) and private-use schemes.
func validRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || len(raw) > 500 || u.User != nil {
		return false
	}
	switch u.Scheme {
	case "https":
		return u.Host != ""
	case "http":
		h := u.Hostname()
		return h == "localhost" || h == "127.0.0.1" || h == "::1"
	}
	return schemeRe.MatchString(u.Scheme) && !forbiddenSchemes[u.Scheme]
}

// RegisterClient implements dynamic client registration for public clients.
func (s *Service) RegisterClient(ctx context.Context, name string, redirectURIs []string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 || len(redirectURIs) == 0 || len(redirectURIs) > 10 {
		return "", ErrInvalidRequest
	}
	for _, r := range redirectURIs {
		if !validRedirect(r) {
			return "", fmt.Errorf("%w: unacceptable redirect_uri", ErrInvalidRequest)
		}
	}
	if n, err := s.q.CountOAuthClients(ctx); err != nil {
		return "", err
	} else if n >= maxClients {
		return "", ErrTooManyClients
	}
	id, err := randToken("bbc_", 18)
	if err != nil {
		return "", err
	}
	return id, s.q.CreateOAuthClient(ctx, dbsqlc.CreateOAuthClientParams{ID: id, Name: name, RedirectUris: redirectURIs})
}

// Request is an authorization request as sent by the client.
type Request struct {
	ClientID            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	State               string
	Scope               string
	Resource            string
}

var challengeRe = regexp.MustCompile(`^[A-Za-z0-9_-]{43,128}$`)

// Validate checks a request against the registered client. The redirect URI must match exactly;
// when it is wrong the user must not be redirected anywhere (callers show an error page).
func (s *Service) Validate(ctx context.Context, r Request) (dbsqlc.OauthClient, []string, error) {
	c, err := s.q.GetOAuthClient(ctx, r.ClientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, nil, ErrInvalidClient
	}
	if err != nil {
		return c, nil, err
	}
	ok := false
	for _, u := range c.RedirectUris {
		if u == r.RedirectURI {
			ok = true
		}
	}
	if !ok {
		return c, nil, ErrInvalidClient
	}
	if r.CodeChallengeMethod != "S256" || !challengeRe.MatchString(r.CodeChallenge) {
		return c, nil, fmt.Errorf("%w: PKCE with S256 is required", ErrInvalidRequest)
	}
	if r.Resource != "" && r.Resource != s.resource {
		return c, nil, fmt.Errorf("%w: unknown resource", ErrInvalidRequest)
	}
	scopes, err := parseScopes(r.Scope)
	return c, scopes, err
}

func parseScopes(s string) ([]string, error) {
	var out []string
	for _, f := range strings.Fields(s) {
		if f != "read" && f != "write" {
			return nil, ErrInvalidScope
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		out = []string{"read"}
	}
	return out, nil
}

func redirectWith(redirect string, params map[string]string) string {
	u, _ := url.Parse(redirect)
	q := u.Query()
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// Approve records the user's consent and returns the URL to send the browser back to. scopes are
// what the user granted (they may be fewer than requested); libs restricts the grant (nil: all).
func (s *Service) Approve(ctx context.Context, userID uuid.UUID, r Request, scopes []string, libs []uuid.UUID) (string, error) {
	if _, _, err := s.Validate(ctx, r); err != nil {
		return "", err
	}
	granted := scopes[:0:0]
	for _, sc := range scopes {
		if sc == "read" || sc == "write" {
			granted = append(granted, sc)
		}
	}
	if len(granted) == 0 {
		return "", ErrInvalidScope
	}
	code, err := randToken("", 32)
	if err != nil {
		return "", err
	}
	if err := s.q.CreateOAuthCode(ctx, dbsqlc.CreateOAuthCodeParams{CodeHash: hash(code), ClientID: r.ClientID, UserID: userID,
		RedirectUri: r.RedirectURI, CodeChallenge: r.CodeChallenge, Scopes: granted, LibraryIds: libs, Resource: s.resource,
		ExpiresAt: s.now().Add(codeTTL)}); err != nil {
		return "", err
	}
	return redirectWith(r.RedirectURI, map[string]string{"code": code, "state": r.State, "iss": s.base}), nil
}

// Deny returns the redirect for a refused request.
func (s *Service) Deny(r Request) string {
	return redirectWith(r.RedirectURI, map[string]string{"error": "access_denied", "state": r.State, "iss": s.base})
}

// TokenResponse is the JSON answer of the token endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

func (s *Service) issue(ctx context.Context, q *dbsqlc.Queries, family uuid.UUID, clientID string, uid uuid.UUID, scopes []string, libs []uuid.UUID) (TokenResponse, error) {
	at, err := randToken(accessPref, 32)
	if err != nil {
		return TokenResponse{}, err
	}
	rt, err := randToken(refreshPref, 32)
	if err != nil {
		return TokenResponse{}, err
	}
	now := s.now()
	if err := q.CreateOAuthAccess(ctx, dbsqlc.CreateOAuthAccessParams{TokenHash: hash(at), ClientID: clientID, UserID: uid,
		Scopes: scopes, LibraryIds: libs, Resource: s.resource, ExpiresAt: now.Add(AccessTTL), FamilyID: family}); err != nil {
		return TokenResponse{}, err
	}
	if err := q.CreateOAuthRefresh(ctx, dbsqlc.CreateOAuthRefreshParams{TokenHash: hash(rt), ClientID: clientID, UserID: uid,
		Scopes: scopes, LibraryIds: libs, Resource: s.resource, ExpiresAt: now.Add(RefreshTTL), FamilyID: family}); err != nil {
		return TokenResponse{}, err
	}
	return TokenResponse{AccessToken: at, TokenType: "Bearer", ExpiresIn: int(AccessTTL.Seconds()), RefreshToken: rt, Scope: strings.Join(scopes, " ")}, nil
}

// ExchangeCode trades an authorization code (with its PKCE verifier) for tokens.
func (s *Service) ExchangeCode(ctx context.Context, clientID, code, redirectURI, verifier, resource string) (TokenResponse, error) {
	row, err := s.q.ConsumeOAuthCode(ctx, hash(code)) // single use, even when the rest fails
	if errors.Is(err, pgx.ErrNoRows) {
		return TokenResponse{}, ErrInvalidGrant
	}
	if err != nil {
		return TokenResponse{}, err
	}
	if row.ClientID != clientID || row.RedirectUri != redirectURI || (resource != "" && resource != row.Resource) {
		return TokenResponse{}, ErrInvalidGrant
	}
	want := base64.RawURLEncoding.EncodeToString(hash(verifier))
	if len(verifier) < 43 || len(verifier) > 128 || subtle.ConstantTimeCompare([]byte(want), []byte(row.CodeChallenge)) != 1 {
		return TokenResponse{}, ErrInvalidGrant
	}
	return s.issue(ctx, s.q, uuid.New(), clientID, row.UserID, row.Scopes, row.LibraryIds) // a new authorization starts a new family
}

// Refresh rotates a refresh token. A refresh token works once. Presenting one that was already used
// means two parties hold copies (a theft, or a client that lost state): the whole family, every
// refresh and access token descended from the same authorization, is revoked, so the thief and the
// owner are both locked out and the user has to authorize again.
func (s *Service) Refresh(ctx context.Context, clientID, refresh string) (TokenResponse, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TokenResponse{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	row, err := q.LockOAuthRefresh(ctx, hash(refresh))
	if errors.Is(err, pgx.ErrNoRows) {
		return TokenResponse{}, ErrInvalidGrant
	}
	if err != nil {
		return TokenResponse{}, err
	}
	if row.UsedAt != nil {
		if err := q.RevokeOAuthFamily(ctx, row.FamilyID); err != nil {
			return TokenResponse{}, err
		}
		if err := q.RevokeOAuthFamilyAccess(ctx, row.FamilyID); err != nil {
			return TokenResponse{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return TokenResponse{}, err
		}
		return TokenResponse{}, ErrInvalidGrant
	}
	if row.ClientID != clientID {
		return TokenResponse{}, ErrInvalidGrant
	}
	u, err := q.GetUser(ctx, row.UserID)
	if err != nil || u.DisabledAt != nil {
		return TokenResponse{}, ErrInvalidGrant
	}
	if err := q.MarkOAuthRefreshUsed(ctx, row.TokenHash); err != nil {
		return TokenResponse{}, err
	}
	tr, err := s.issue(ctx, q, row.FamilyID, clientID, row.UserID, row.Scopes, row.LibraryIds)
	if err != nil {
		return TokenResponse{}, err
	}
	return tr, tx.Commit(ctx)
}

// Resolve authenticates an OAuth access token for the MCP endpoint. It returns (nil, nil) for
// tokens that are not ours.
func (s *Service) Resolve(ctx context.Context, secret string) (*authz.Principal, error) {
	if !strings.HasPrefix(secret, accessPref) || len(secret) > 100 {
		return nil, nil
	}
	t, err := s.q.GetOAuthAccess(ctx, hash(secret))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && t.DisabledAt != nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &authz.Principal{UserID: t.UserID, Email: t.Email, IsAdmin: t.IsAdmin, Scopes: t.Scopes, Via: "token",
		LibraryIDs: t.LibraryIds, Audience: "mcp"}, nil
}

// Purge removes expired access tokens.
func (s *Service) Purge(ctx context.Context) error { return s.q.PurgeOAuth(ctx) }

// Grant is an application the user authorized.
type Grant struct {
	ClientID string
	Name     string
	Scopes   []string
	Since    time.Time
}

// Grants lists the applications with a live refresh token for the user.
func (s *Service) Grants(ctx context.Context, userID uuid.UUID) ([]Grant, error) {
	rows, err := s.q.ListOAuthGrants(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Grant, len(rows))
	for i, r := range rows {
		out[i] = Grant{r.ClientID, r.Name, r.Scopes, r.Since}
	}
	return out, nil
}

// RevokeGrant removes an application's access: its refresh tokens and its live access tokens.
func (s *Service) RevokeGrant(ctx context.Context, userID uuid.UUID, clientID string) error {
	n, err := s.q.RevokeOAuthRefreshForClient(ctx, dbsqlc.RevokeOAuthRefreshForClientParams{UserID: userID, ClientID: clientID})
	if err != nil {
		return err
	}
	if err := s.q.RevokeOAuthAccessForClient(ctx, dbsqlc.RevokeOAuthAccessForClientParams{UserID: userID, ClientID: clientID}); err != nil {
		return err
	}
	if n == 0 {
		return ErrInvalidClient
	}
	return nil
}

// RevokeAll removes every OAuth grant of a user (administrator action, or account deletion).
func (s *Service) RevokeAll(ctx context.Context, userID uuid.UUID) error {
	if err := s.q.RevokeAllOAuthRefreshForUser(ctx, userID); err != nil {
		return err
	}
	return s.q.RevokeAllOAuthAccessForUser(ctx, userID)
}
