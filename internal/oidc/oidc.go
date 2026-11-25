// Package oidc signs users in through an external OpenID Connect provider (authorization code
// flow with PKCE and nonce). Discovery and token requests use the SSRF-safe HTTP client.
//
// Account rules: an existing identity (issuer + subject) signs in directly. Otherwise the ID
// token must carry a verified email from the configured issuer. A new account is created when
// auto-creation is on. Only a newly provisioned SCIM account with no credentials can be linked
// by email on first sign-in. Other existing accounts require explicit linking. Groups from the token
// are mirrored into externally managed groups, and a configured admin group grants administration.
package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
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
	ErrStaleAuth       = errors.New("the identity provider did not authenticate the user again")
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
	return s.begin(ctx, purposeLogin, nil, "")
}

// BeginStepUp returns the URL that makes the identity provider authenticate the user again
// (prompt=login, max_age=0) for a sensitive action.
func (s *Service) BeginStepUp(ctx context.Context) (authURL, state string, err error) {
	return s.begin(ctx, purposeStepUp, nil, "", oauth2.SetAuthURLParam("prompt", "login"), oauth2.SetAuthURLParam("max_age", "0"))
}

// BeginLink starts an explicit link bound to the currently authenticated account.
func (s *Service) BeginLink(ctx context.Context, userID uuid.UUID, sessionToken string) (authURL, state string, err error) {
	if sessionToken == "" {
		return "", "", ErrBadFlow
	}
	return s.begin(ctx, purposeLink, &userID, sessionToken, oauth2.SetAuthURLParam("prompt", "login"))
}

const (
	purposeLogin  = "login"
	purposeStepUp = "step_up"
	purposeLink   = "link"
)

func (s *Service) begin(ctx context.Context, purpose string, linkUserID *uuid.UUID, sessionToken string, extra ...oauth2.AuthCodeOption) (authURL, state string, err error) {
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
	var sessionHash []byte
	if sessionToken != "" {
		sessionHash = hash(sessionToken)
	}
	if err := s.q.CreateOIDCFlow(ctx, dbsqlc.CreateOIDCFlowParams{StateHash: hash(state), Nonce: nonce, Verifier: verifier,
		Purpose: purpose, ExpiresAt: time.Now().Add(flowLife), LinkUserID: linkUserID, LinkSessionHash: sessionHash}); err != nil {
		return "", "", err
	}
	_ = s.q.PurgeOIDCFlows(ctx)
	opts := append([]oauth2.AuthCodeOption{oauth2.S256ChallengeOption(verifier), gooidc.Nonce(nonce)}, extra...)
	return s.oauth(p).AuthCodeURL(state, opts...), state, nil
}

type claims struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Locale        string `json:"locale"`
	AuthTime      int64  `json:"auth_time"`
}

// Complete finishes a sign-in flow and returns the local user ID.
func (s *Service) Complete(ctx context.Context, code, state string) (uuid.UUID, error) {
	uid, _, err := s.complete(ctx, code, state, purposeLogin, nil, "")
	return uid, err
}

// CompleteStepUp finishes a re-authentication flow and returns the local user ID. strong is true
// when the provider reported an authentication time inside the step-up window. Providers that do
// not send auth_time give a weaker proof (the ID token was just issued after prompt=login): the
// caller then unlocks a single action instead of a window.
func (s *Service) CompleteStepUp(ctx context.Context, code, state string) (uid uuid.UUID, strong bool, err error) {
	uid, strong, err = s.complete(ctx, code, state, purposeStepUp, nil, "")
	return uid, strong, err
}

// CompleteLink attaches the verified provider identity to the same account that initiated linking.
func (s *Service) CompleteLink(ctx context.Context, code, state string, userID uuid.UUID, sessionToken string) error {
	_, _, err := s.complete(ctx, code, state, purposeLink, &userID, sessionToken)
	return err
}

func (s *Service) complete(ctx context.Context, code, state, purpose string, linkUserID *uuid.UUID, sessionToken string) (_ uuid.UUID, strong bool, _ error) {
	flow, err := s.q.ConsumeOIDCFlow(ctx, hash(state))
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, ErrBadFlow
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	if flow.Purpose != purpose {
		return uuid.Nil, false, ErrBadFlow
	}
	if purpose == purposeLink && (flow.LinkUserID == nil || linkUserID == nil || *flow.LinkUserID != *linkUserID || sessionToken == "" || subtle.ConstantTimeCompare(flow.LinkSessionHash, hash(sessionToken)) != 1) {
		return uuid.Nil, false, ErrBadFlow
	}
	p, err := s.discover(ctx)
	if err != nil {
		return uuid.Nil, false, err
	}
	tok, err := s.oauth(p).Exchange(s.ctx(ctx), code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("oidc: token exchange failed: %w", err)
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return uuid.Nil, false, errors.New("oidc: no id_token in the response")
	}
	idt, err := p.Verifier(&gooidc.Config{ClientID: s.cfg.ClientID}).Verify(s.ctx(ctx), raw)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("oidc: invalid id_token: %w", err)
	}
	if idt.Nonce != flow.Nonce {
		return uuid.Nil, false, ErrBadFlow
	}
	var c claims
	if err := idt.Claims(&c); err != nil {
		return uuid.Nil, false, err
	}
	if purpose == purposeLink {
		if err := s.linkIdentity(ctx, *linkUserID, sessionToken, idt.Issuer, c); err != nil {
			return uuid.Nil, false, err
		}
		return *linkUserID, false, nil
	}
	if purpose == purposeStepUp {
		// With max_age=0 the provider must report when the user authenticated. No claim, or an old
		// one, means it did not really ask again.
		at := time.Unix(c.AuthTime, 0)
		strong = c.AuthTime != 0
		if !strong {
			at = idt.IssuedAt
		}
		if at.IsZero() || time.Since(at) > auth.StepUpWindow || time.Until(at) > time.Minute {
			return uuid.Nil, false, ErrStaleAuth
		}
	}
	var all map[string]any
	_ = idt.Claims(&all)
	groups := stringList(all[s.cfg.GroupsClaim])

	uid, err := s.resolveUser(ctx, idt.Issuer, c)
	if err != nil {
		return uuid.Nil, false, err
	}
	if purpose == purposeLogin {
		if err := s.syncGroups(ctx, uid, groups); err != nil {
			return uuid.Nil, false, err
		}
	}
	return uid, strong, nil
}

func (s *Service) linkIdentity(ctx context.Context, userID uuid.UUID, sessionToken, issuer string, c claims) error {
	if c.Subject == "" || !c.EmailVerified {
		return ErrUnverifiedEmail
	}
	email, err := auth.NormalizeEmail(c.Email)
	if err != nil {
		return ErrUnverifiedEmail
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("oidc: begin link: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if _, err := q.LockOAuthUser(ctx, userID); err != nil {
		return fmt.Errorf("oidc: lock linking account: %w", err)
	}
	u, err := q.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if u.DisabledAt != nil {
		return ErrDisabled
	}
	sessionUser, err := q.LockSession(ctx, hash(sessionToken))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBadFlow
	}
	if err != nil {
		return fmt.Errorf("oidc: lock linking session: %w", err)
	}
	if sessionUser != userID {
		return ErrBadFlow
	}
	if !strings.EqualFold(u.Email, email) {
		return ErrNotAllowed
	}
	if err := q.CreateIdentity(ctx, dbsqlc.CreateIdentityParams{Issuer: issuer, Subject: c.Subject, UserID: userID}); err != nil {
		return err
	}
	// CreateIdentity ignores conflicts; verify that a concurrent link did not belong to another user.
	uid, err := q.GetIdentity(ctx, dbsqlc.GetIdentityParams{Issuer: issuer, Subject: c.Subject})
	if err != nil {
		return err
	}
	if uid != userID {
		return ErrNotAllowed
	}
	if _, err := q.VerifyUserEmail(ctx, dbsqlc.VerifyUserEmailParams{ID: userID, Email: email}); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
	// A verified provider email does not prove control of an existing local account. The sole
	// exception is a freshly SCIM-provisioned account with no credentials or active sessions.
	if !c.EmailVerified || strings.TrimSpace(c.Email) == "" {
		return uuid.Nil, ErrUnverifiedEmail
	}
	email, err := auth.NormalizeEmail(c.Email)
	if err != nil {
		return uuid.Nil, ErrUnverifiedEmail
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("oidc: begin identity resolution: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	u, err := q.GetUserByEmail(ctx, email)
	switch {
	case err == nil:
		if _, err := q.ClaimScimBootstrap(ctx, dbsqlc.ClaimScimBootstrapParams{UserID: u.ID, Email: email}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return uuid.Nil, ErrNotAllowed
			}
			return uuid.Nil, fmt.Errorf("oidc: claim SCIM account: %w", err)
		}
	case errors.Is(err, pgx.ErrNoRows):
		if !s.cfg.AutoCreate {
			return uuid.Nil, ErrNotAllowed
		}
		u, err = q.CreateExternalUser(ctx, dbsqlc.CreateExternalUserParams{Email: email, DisplayName: c.Name, IsAdmin: false})
		if err != nil {
			return uuid.Nil, err
		}
		if _, err := q.VerifyUserEmail(ctx, dbsqlc.VerifyUserEmailParams{ID: u.ID, Email: email}); err != nil {
			return uuid.Nil, err
		}
	default:
		return uuid.Nil, err
	}
	if err := q.CreateIdentity(ctx, dbsqlc.CreateIdentityParams{Issuer: issuer, Subject: c.Subject, UserID: u.ID}); err != nil {
		return uuid.Nil, err
	}
	linked, err := q.GetIdentity(ctx, dbsqlc.GetIdentityParams{Issuer: issuer, Subject: c.Subject})
	if err != nil {
		return uuid.Nil, err
	}
	if linked != u.ID {
		return uuid.Nil, ErrNotAllowed
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("oidc: commit identity resolution: %w", err)
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
