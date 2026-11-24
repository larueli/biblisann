package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

// StepUpWindow is how long a fresh authentication unlocks sensitive actions (ADR 0026).
const StepUpWindow = 5 * time.Minute

// ErrStepUpRequired means the action needs a recent re-authentication.
var ErrStepUpRequired = errors.New("recent authentication required")

// StepUpValid reports whether the session proved itself again within StepUpWindow.
func (s *Service) StepUpValid(sess *Session) bool {
	return sess != nil && sess.StepUpUntil != nil && sess.StepUpUntil.After(s.now())
}

// MarkStepUp opens the step-up window on the session behind a cookie token.
func (s *Service) MarkStepUp(ctx context.Context, token string) error {
	return s.markStepUp(ctx, token, StepUpWindow, false)
}

// MarkStepUpOnce unlocks exactly one sensitive action (for two minutes at most). It is the answer
// to a weak proof: an identity provider that does not report when the user signed in.
func (s *Service) MarkStepUpOnce(ctx context.Context, token string) error {
	return s.markStepUp(ctx, token, 2*time.Minute, true)
}

// ConsumeStepUp closes a single-use window. It does nothing for a regular window.
func (s *Service) ConsumeStepUp(ctx context.Context, token string) error {
	if err := s.q.ConsumeSessionStepUp(ctx, hashToken(token)); err != nil {
		return fmt.Errorf("auth: consume step-up: %w", err)
	}
	return nil
}

func (s *Service) markStepUp(ctx context.Context, token string, window time.Duration, once bool) error {
	until := s.now().Add(window)
	if err := s.q.MarkSessionStepUp(ctx, dbsqlc.MarkSessionStepUpParams{TokenHash: hashToken(token), StepupUntil: &until, StepupSingleUse: once}); err != nil {
		return fmt.Errorf("auth: mark step-up: %w", err)
	}
	return nil
}

// StepUpWithPassword re-checks the password (and the authenticator or a recovery code when the
// account has a second factor) and opens the step-up window.
func (s *Service) StepUpWithPassword(ctx context.Context, token string, userID uuid.UUID, password, code string) error {
	u, err := s.q.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("auth: load user: %w", err)
	}
	if u.PasswordHash == nil {
		return ErrInvalidCredentials
	}
	if ok, _ := VerifyPassword(password, *u.PasswordHash); !ok {
		return ErrInvalidCredentials
	}
	if s.hasMFA(ctx, userID) {
		ok, err := s.checkTOTP(ctx, s.q, userID, code)
		if err != nil {
			return err
		}
		if !ok {
			n, err := s.q.UseRecoveryCode(ctx, dbsqlc.UseRecoveryCodeParams{UserID: userID, CodeHash: recoveryHash(code)})
			if err != nil {
				return err
			}
			ok = n == 1
		}
		if !ok {
			return ErrInvalidCode
		}
	}
	return s.MarkStepUp(ctx, token)
}

// AdminSetPassword sets a password on any account (including single sign-on accounts, which then
// can also sign in locally) and ends its sessions.
func (s *Service) AdminSetPassword(ctx context.Context, userID uuid.UUID, password string) error {
	u, err := s.q.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("auth: load user: %w", err)
	}
	if err := CheckPassword(password, u.Email, u.DisplayName); err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	if err := s.q.UpdateUserPassword(ctx, dbsqlc.UpdateUserPasswordParams{ID: userID, PasswordHash: &hash}); err != nil {
		return fmt.Errorf("auth: update password: %w", err)
	}
	return s.q.DeleteUserSessions(ctx, userID)
}

// HasPassword reports whether the account can sign in with a password (single sign-on and
// passkey-only accounts cannot).
func (s *Service) HasPassword(ctx context.Context, userID uuid.UUID) bool {
	u, err := s.q.GetUser(ctx, userID)
	return err == nil && u.PasswordHash != nil
}
