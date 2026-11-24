package library

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

	"github.com/larueli/biblisann/internal/authz"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

// InvitationLife is how long an invitation link works.
const InvitationLife = 7 * 24 * time.Hour

// ErrNoAccount means nobody is registered with the email address. Only instance administrators
// ever see it: for everyone else sharing with an unknown address silently does nothing, exactly
// like sharing with a known one answers, so addresses cannot be probed.
var ErrNoAccount = errors.New("no account uses this email address")

// ErrBadInvitation means the invitation link is unknown, used or expired.
var ErrBadInvitation = errors.New("invalid or expired invitation")

func invitationHash(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// Invite records an invitation for an address without an account and returns the secret token.
// Only instance administrators who administer the library may do it, and the grant may not make
// the invitee a library administrator.
func (s *Service) Invite(ctx context.Context, libraryID uuid.UUID, email string, g Grant) (string, error) {
	p, err := s.RequirePerm(ctx, nil, libraryID, authz.PermAdmin)
	if err != nil {
		return "", err
	}
	if !p.IsAdmin {
		return "", ErrForbidden
	}
	if err := s.checkGrant(ctx, libraryID, g); err != nil {
		return "", err
	}
	email = strings.TrimSpace(email)
	if email == "" || len(email) > 320 || !strings.Contains(email, "@") {
		return "", ErrInvalid
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("library: invitation token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	_, err = s.q.CreateInvitation(ctx, dbsqlc.CreateInvitationParams{LibraryID: libraryID, Email: email, Role: g.Role,
		ProfileID: g.ProfileID, InvitedBy: p.UserID, TokenHash: invitationHash(token), ExpiresAt: time.Now().Add(InvitationLife)})
	if err != nil {
		return "", fmt.Errorf("library: create invitation: %w", err)
	}
	return token, nil
}

// InvitationInfo is what the invitation page shows before signing up.
type InvitationInfo struct {
	Email       string
	LibraryName string
}

// PeekInvitation describes a valid invitation without using it.
func (s *Service) PeekInvitation(ctx context.Context, token string) (InvitationInfo, error) {
	if len(token) > 128 {
		return InvitationInfo{}, ErrBadInvitation
	}
	row, err := s.q.PeekInvitation(ctx, invitationHash(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return InvitationInfo{}, ErrBadInvitation
	}
	if err != nil {
		return InvitationInfo{}, err
	}
	return InvitationInfo{Email: row.Email, LibraryName: row.LibraryName}, nil
}

// AcceptInvitation uses the token for userID: the link proves the mailbox, so it works for any
// account the holder signs in with (they may change their email later). Existing access is never
// downgraded.
func (s *Service) AcceptInvitation(ctx context.Context, token string, userID uuid.UUID) (uuid.UUID, error) {
	if len(token) > 128 {
		return uuid.Nil, ErrBadInvitation
	}
	inv, err := s.q.ConsumeInvitation(ctx, invitationHash(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrBadInvitation
	}
	if err != nil {
		return uuid.Nil, err
	}
	err = s.q.GrantIfNoAccess(ctx, dbsqlc.GrantIfNoAccessParams{LibraryID: inv.LibraryID, UserID: userID, Role: inv.Role, ProfileID: inv.ProfileID})
	return inv.LibraryID, err
}
