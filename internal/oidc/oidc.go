// Package oidc signs users in through an external OpenID Connect provider (authorization code
// flow with PKCE and nonce). Discovery and token requests use the SSRF-safe HTTP client.
//
// Account rules: an existing identity (issuer + subject) signs in directly. Otherwise the ID
// token must carry a verified email from the configured issuer; the account with that email is
// then linked, or created when auto-creation is on. Groups from the token are mirrored into
// externally managed groups, and a configured admin group grants instance administration.
package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"

	"github.com/larueli/biblisann/internal/auth"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

// Config configures the provider.
type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string // BASE_URL + /v1/auth/oidc/callback
	Scopes       []string
	GroupsClaim  string
	AdminGroup   string
	AutoCreate   bool
	Label        string
}

// Errors.
var (
	ErrUnverifiedEmail = errors.New("the identity provider did not verify the email address")
	ErrNotAllowed      = errors.New("no account for this identity and automatic creation is off")
	ErrBadFlow         = errors.New("invalid or expired sign-in request")
	ErrDisabled        = errors.New("this account is disabled")
)

const flowLife = 10 * time.Minute

// Service is the OIDC login service.
type Service struct {
	cfg  Config
	pool *pgxpool.Pool
	q    *dbsqlc.Queries
	auth *auth.Service
	http *http.Client //nolint:forbidigo // the SSRF-safe client from safehttp.Client.Std

	mu       sync.Mutex
	provider *gooidc.Provider
}

// New creates the service. client carries the discovery and token traffic (use safehttp's Std).
func New(cfg Config, pool *pgxpool.Pool, a *auth.Service, client *http.Client) *Service { //nolint:forbidigo // the SSRF-safe client from safehttp.Client.Std
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{gooidc.ScopeOpenID, "email", "profile", "groups"}
	}
	if cfg.GroupsClaim == "" {
		cfg.GroupsClaim = "groups"
	}
	return &Service{cfg: cfg, pool: pool, q: dbsqlc.New(pool), auth: a, http: client}
}

// Label is the text of the sign-in button.
func (s *Service) Label() string {
	if s.cfg.Label != "" {
		return s.cfg.Label
	}
	return "Single sign-on"
}

func (s *Service) ctx(ctx context.Context) context.Context {
	return gooidc.ClientContext(ctx, s.http)
}

// discover resolves the provider lazily, so the server starts even when the IdP is down.
func (s *Service) discover(ctx context.Context) (*gooidc.Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.provider != nil {
		return s.provider, nil
	}
	p, err := gooidc.NewProvider(s.ctx(ctx), s.cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc: discovery: %w", err)
	}
	s.provider = p
	return p, nil
}

func (s *Service) oauth(p *gooidc.Provider) *oauth2.Config {
	return &oauth2.Config{ClientID: s.cfg.ClientID, ClientSecret: s.cfg.ClientSecret, Endpoint: p.Endpoint(),
		RedirectURL: s.cfg.RedirectURL, Scopes: s.cfg.Scopes}
}

func randString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hash(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }

// Begin returns the URL to send the browser to.
func (s *Service) Begin(ctx context.Context) (authURL, state string, err error) {
	p, err := s.discover(ctx)
	if err != nil {
		return "", "", err
	}
	state, err = randString(24)
	if err != nil {
		return "", "", err
	}
	nonce, err := randString(24)
	if err != nil {
		return "", "", err
	}
	verifier := oauth2.GenerateVerifier()
	if err := s.q.CreateOIDCFlow(ctx, dbsqlc.CreateOIDCFlowParams{StateHash: hash(state), Nonce: nonce, Verifier: verifier,
		ExpiresAt: time.Now().Add(flowLife)}); err != nil {
		return "", "", err
	}
	_ = s.q.PurgeOIDCFlows(ctx)
	return s.oauth(p).AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), gooidc.Nonce(nonce)), state, nil
}

type claims struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Locale        string `json:"locale"`
}

// Complete finishes the flow and returns the local user ID.
func (s *Service) Complete(ctx context.Context, code, state string) (uuid.UUID, error) {
	flow, err := s.q.ConsumeOIDCFlow(ctx, hash(state))
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrBadFlow
	}
	if err != nil {
		return uuid.Nil, err
	}
	p, err := s.discover(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	tok, err := s.oauth(p).Exchange(s.ctx(ctx), code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		return uuid.Nil, fmt.Errorf("oidc: token exchange failed: %w", err)
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return uuid.Nil, errors.New("oidc: no id_token in the response")
	}
	idt, err := p.Verifier(&gooidc.Config{ClientID: s.cfg.ClientID}).Verify(s.ctx(ctx), raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("oidc: invalid id_token: %w", err)
	}
	if idt.Nonce != flow.Nonce {
		return uuid.Nil, ErrBadFlow
	}
	var c claims
	if err := idt.Claims(&c); err != nil {
		return uuid.Nil, err
	}
	var all map[string]any
	_ = idt.Claims(&all)
	groups := stringList(all[s.cfg.GroupsClaim])

	uid, err := s.resolveUser(ctx, idt.Issuer, c)
	if err != nil {
		return uuid.Nil, err
	}
	if err := s.syncGroups(ctx, uid, groups); err != nil {
		return uuid.Nil, err
	}
	return uid, nil
}

func (s *Service) resolveUser(ctx context.Context, issuer string, c claims) (uuid.UUID, error) {
	if c.Subject == "" {
		return uuid.Nil, errors.New("oidc: token without subject")
	}
	if uid, err := s.q.GetIdentity(ctx, dbsqlc.GetIdentityParams{Issuer: issuer, Subject: c.Subject}); err == nil {
		u, err := s.q.GetUser(ctx, uid)
		if err != nil {
			return uuid.Nil, err
		}
		if u.DisabledAt != nil {
			return uuid.Nil, ErrDisabled
		}
		return uid, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}
	// First sign-in with this identity: only a verified email may attach it to an account.
	if !c.EmailVerified || strings.TrimSpace(c.Email) == "" {
		return uuid.Nil, ErrUnverifiedEmail
	}
	email, err := auth.NormalizeEmail(c.Email)
	if err != nil {
		return uuid.Nil, ErrUnverifiedEmail
	}
	u, err := s.q.GetUserByEmail(ctx, email)
	switch {
	case err == nil:
		if u.DisabledAt != nil {
			return uuid.Nil, ErrDisabled
		}
	case errors.Is(err, pgx.ErrNoRows):
		if !s.cfg.AutoCreate {
			return uuid.Nil, ErrNotAllowed
		}
		u, err = s.q.CreateExternalUser(ctx, dbsqlc.CreateExternalUserParams{Email: email, DisplayName: c.Name, IsAdmin: false})
		if err != nil {
			return uuid.Nil, err
		}
	default:
		return uuid.Nil, err
	}
	if err := s.q.CreateIdentity(ctx, dbsqlc.CreateIdentityParams{Issuer: issuer, Subject: c.Subject, UserID: u.ID}); err != nil {
		return uuid.Nil, err
	}
	return u.ID, nil
}

// syncGroups mirrors the token's groups: it adds missing memberships, removes memberships of
// externally managed groups the user left, and applies the admin group.
func (s *Service) syncGroups(ctx context.Context, uid uuid.UUID, names []string) error {
	want := map[string]bool{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || len(n) > 200 {
			continue
		}
		g, err := s.q.UpsertExternalGroup(ctx, dbsqlc.UpsertExternalGroupParams{Name: n, ExternalID: ptr("oidc:" + n)})
		if errors.Is(err, pgx.ErrNoRows) {
			continue // an administrator's own group with that name: identity provider claims never join it
		}
		if err != nil {
			return err
		}
		want[g.ID.String()] = true
		if err := s.q.AddGroupMember(ctx, dbsqlc.AddGroupMemberParams{GroupID: g.ID, UserID: uid}); err != nil {
			return err
		}
	}
	current, err := s.q.ListExternalGroupsOfUser(ctx, uid)
	if err != nil {
		return err
	}
	for _, g := range current {
		if !want[g.ID.String()] {
			if err := s.q.RemoveGroupMember(ctx, dbsqlc.RemoveGroupMemberParams{GroupID: g.ID, UserID: uid}); err != nil {
				return err
			}
		}
	}
	if s.cfg.AdminGroup != "" {
		isAdmin := false
		for _, n := range names {
			if strings.EqualFold(strings.TrimSpace(n), s.cfg.AdminGroup) {
				isAdmin = true
			}
		}
		return s.q.SetUserAdmin(ctx, dbsqlc.SetUserAdminParams{ID: uid, IsAdmin: isAdmin})
	}
	return nil
}

func ptr(s string) *string { return &s }

func stringList(v any) []string {
	var out []string
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
	case string:
		out = strings.Fields(strings.ReplaceAll(x, ",", " "))
	}
	return out
}
