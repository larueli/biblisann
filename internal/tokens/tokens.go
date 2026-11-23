// Package tokens manages personal access tokens (scoped, optionally limited to some libraries)
// and the calendar feed tokens. Only a SHA-256 hash is stored; the secret is shown once.
package tokens

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/larueli/biblisann/internal/authz"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

// Token kinds and prefixes (prefixes let secret scanners recognise leaked tokens).
const (
	KindPAT    = "pat"
	KindFeed   = "feed"
	KindSCIM   = "scim"
	prefixPAT  = "bb_pat_"
	prefixFeed = "bb_feed_"
	prefixSCIM = "bb_scim_"
)

// Errors.
var (
	ErrInvalid  = errors.New("invalid token request")
	ErrNotFound = errors.New("token not found")
)

// Service manages tokens.
type Service struct {
	pool *pgxpool.Pool
	q    *dbsqlc.Queries
}

// NewService creates the service.
func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool, q: dbsqlc.New(pool)} }

// Created is returned once, with the secret.
type Created struct {
	Token string
	Meta  dbsqlc.CreateTokenRow
}

// Create issues a token for the calling user. Scopes are "read" and/or "write"; a feed token has
// no scopes and only opens the calendar feed.
func (s *Service) Create(ctx context.Context, kind, name string, scopes []string, libraries []uuid.UUID, expires *time.Time) (Created, error) {
	p, err := authz.From(ctx)
	if err != nil {
		return Created{}, err
	}
	if p.Via == "token" {
		return Created{}, authz.ErrForbidden // tokens cannot mint tokens
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 || (kind != KindPAT && kind != KindFeed && kind != KindSCIM) {
		return Created{}, ErrInvalid
	}
	if kind == KindPAT {
		if len(scopes) == 0 {
			return Created{}, ErrInvalid
		}
		for _, sc := range scopes {
			if sc != "read" && sc != "write" {
				return Created{}, ErrInvalid
			}
		}
	} else {
		scopes = []string{}
		if kind == KindSCIM && !p.IsAdmin {
			return Created{}, authz.ErrForbidden // provisioning tokens are an administrator tool
		}
	}
	if expires != nil && !expires.After(time.Now()) {
		return Created{}, ErrInvalid
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Created{}, fmt.Errorf("tokens: random: %w", err)
	}
	prefix := prefixPAT
	switch kind {
	case KindFeed:
		prefix = prefixFeed
	case KindSCIM:
		prefix = prefixSCIM
	}
	secret := prefix + base64.RawURLEncoding.EncodeToString(raw)
	meta, err := s.q.CreateToken(ctx, dbsqlc.CreateTokenParams{UserID: p.UserID, Name: name, Kind: kind,
		TokenHash: hash(secret), Scopes: scopes, LibraryIds: libraries, ExpiresAt: expires})
	if err != nil {
		return Created{}, fmt.Errorf("tokens: create: %w", err)
	}
	return Created{Token: secret, Meta: meta}, nil
}

func hash(secret string) []byte {
	h := sha256.Sum256([]byte(secret))
	return h[:]
}

// Resolve implements the API's token resolver for "Authorization: Bearer" requests. Feed tokens
// never authenticate API calls.
func (s *Service) Resolve(ctx context.Context, secret string) (*authz.Principal, error) {
	if !strings.HasPrefix(secret, prefixPAT) || len(secret) > 100 {
		return nil, nil
	}
	t, err := s.q.GetTokenByHash(ctx, hash(secret))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (t.DisabledAt != nil || t.Kind != KindPAT)) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("tokens: resolve: %w", err)
	}
	_ = s.q.TouchToken(ctx, t.ID)
	return &authz.Principal{UserID: t.UserID, Email: t.Email, IsAdmin: t.IsAdmin, Scopes: t.Scopes,
		Via: "token", LibraryIDs: t.LibraryIds}, nil
}

// FeedUser resolves a feed token to its user.
func (s *Service) FeedUser(ctx context.Context, secret string) (uuid.UUID, error) {
	if !strings.HasPrefix(secret, prefixFeed) || len(secret) > 100 {
		return uuid.Nil, ErrNotFound
	}
	t, err := s.q.GetTokenByHash(ctx, hash(secret))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (t.DisabledAt != nil || t.Kind != KindFeed)) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	_ = s.q.TouchToken(ctx, t.ID)
	return t.UserID, nil
}

// List returns the caller's tokens (never the secrets).
func (s *Service) List(ctx context.Context) ([]dbsqlc.ListTokensRow, error) {
	p, err := authz.From(ctx)
	if err != nil {
		return nil, err
	}
	return s.q.ListTokens(ctx, p.UserID)
}

// Revoke deletes one of the caller's tokens.
func (s *Service) Revoke(ctx context.Context, id uuid.UUID) error {
	p, err := authz.From(ctx)
	if err != nil {
		return err
	}
	n, err := s.q.DeleteToken(ctx, dbsqlc.DeleteTokenParams{ID: id, UserID: p.UserID})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ValidSCIM reports whether secret is a live SCIM provisioning token.
func (s *Service) ValidSCIM(ctx context.Context, secret string) bool {
	if !strings.HasPrefix(secret, prefixSCIM) || len(secret) > 100 {
		return false
	}
	t, err := s.q.GetTokenByHash(ctx, hash(secret))
	if err != nil || t.Kind != KindSCIM || t.DisabledAt != nil || !t.IsAdmin {
		return false
	}
	_ = s.q.TouchToken(ctx, t.ID)
	return true
}

// RevokeAllPATs deletes every personal access token of a user (administrator action).
func (s *Service) RevokeAllPATs(ctx context.Context, userID uuid.UUID) error {
	return s.q.DeleteUserTokens(ctx, userID)
}
