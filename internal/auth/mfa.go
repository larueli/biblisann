package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

// Errors of the second factor and reset flows.
var (
	ErrMFADisabled   = errors.New("two-factor authentication is not available on this server")
	ErrInvalidCode   = errors.New("invalid code")
	ErrTooManyTries  = errors.New("too many attempts")
	ErrInvalidToken  = errors.New("invalid or expired token")
	ErrNoMailer      = errors.New("password reset by email is not available on this server")
	mfaChallengeLife = 5 * time.Minute
	resetLife        = time.Hour
	maxMFAAttempts   = 5
)

// MFARequired is returned by Login when the password was right but a second factor is needed.
type MFARequired struct{ Challenge string }

func (*MFARequired) Error() string { return "second factor required" }

// Mailer sends a plain-text email.
type Mailer interface {
	SendMail(ctx context.Context, to, subject, body string) error
}

// EnableMFA turns on TOTP with the 32-byte key used to seal secrets at rest.
func (s *Service) EnableMFA(key []byte) error {
	if len(key) != 32 {
		return errors.New("auth: BIBLISANN_SECRET_KEY must decode to 32 bytes")
	}
	s.secretKey = key
	return nil
}

// SetMailer enables password reset by email.
func (s *Service) SetMailer(m Mailer) { s.mailer = m }

// MFAAvailable reports whether TOTP can be used.
func (s *Service) MFAAvailable() bool { return s.secretKey != nil }

// ResetAvailable reports whether password reset by email can be used.
func (s *Service) ResetAvailable() bool { return s.mailer != nil }

func (s *Service) hasMFA(ctx context.Context, userID uuid.UUID) bool {
	t, err := s.q.GetTOTP(ctx, userID)
	return err == nil && t.ConfirmedAt != nil
}

// IsMFAEnabled reports whether the user has a confirmed second factor.
func (s *Service) IsMFAEnabled(ctx context.Context, userID uuid.UUID) bool {
	return s.hasMFA(ctx, userID)
}

// EnrollTOTP starts enrollment: it returns the base32 secret and the otpauth URI. The factor is
// not active until ConfirmTOTP succeeds.
func (s *Service) EnrollTOTP(ctx context.Context, userID uuid.UUID, issuer string) (secret, uri string, err error) {
	if s.secretKey == nil {
		return "", "", ErrMFADisabled
	}
	u, err := s.q.GetUser(ctx, userID)
	if err != nil {
		return "", "", err
	}
	raw, err := newTOTPSecret()
	if err != nil {
		return "", "", err
	}
	sealed, err := seal(s.secretKey, raw)
	if err != nil {
		return "", "", err
	}
	if err := s.q.UpsertTOTP(ctx, dbsqlc.UpsertTOTPParams{UserID: userID, SecretEnc: sealed}); err != nil {
		return "", "", fmt.Errorf("auth: store totp: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), totpURI(issuer, u.Email, raw), nil
}

func (s *Service) checkTOTP(ctx context.Context, q *dbsqlc.Queries, userID uuid.UUID, code string) (bool, error) {
	t, err := q.GetTOTP(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	secret, err := open(s.secretKey, t.SecretEnc)
	if err != nil {
		return false, fmt.Errorf("auth: open totp secret: %w", err)
	}
	step := matchTOTP(secret, code, s.now())
	if step == 0 {
		return false, nil
	}
	// A code works once: the step must advance.
	n, err := q.SetTOTPStep(ctx, dbsqlc.SetTOTPStepParams{UserID: userID, LastStep: step})
	return n == 1, err
}

// ConfirmTOTP activates the factor with a first valid code and returns fresh recovery codes.
func (s *Service) ConfirmTOTP(ctx context.Context, userID uuid.UUID, code string) ([]string, error) {
	if s.secretKey == nil {
		return nil, ErrMFADisabled
	}
	ok, err := s.checkTOTP(ctx, s.q, userID, code)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrInvalidCode
	}
	if err := s.q.ConfirmTOTP(ctx, userID); err != nil {
		return nil, err
	}
	return s.newRecoveryCodes(ctx, userID)
}

func (s *Service) newRecoveryCodes(ctx context.Context, userID uuid.UUID) ([]string, error) {
	if err := s.q.DeleteRecoveryCodes(ctx, userID); err != nil {
		return nil, err
	}
	codes := make([]string, 10)
	for i := range codes {
		b := make([]byte, 7)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		c := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))[:10]
		codes[i] = c[:5] + "-" + c[5:]
		if err := s.q.AddRecoveryCode(ctx, dbsqlc.AddRecoveryCodeParams{UserID: userID, CodeHash: recoveryHash(codes[i])}); err != nil {
			return nil, err
		}
	}
	return codes, nil
}

func recoveryHash(code string) []byte {
	h := sha256.Sum256([]byte(strings.ToLower(strings.ReplaceAll(strings.TrimSpace(code), " ", ""))))
	return h[:]
}

// DisableTOTP removes the second factor after re-checking the password.
func (s *Service) DisableTOTP(ctx context.Context, userID uuid.UUID, password string) error {
	u, err := s.q.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if u.PasswordHash == nil {
		return ErrInvalidCredentials
	}
	if ok, _ := VerifyPassword(password, *u.PasswordHash); !ok {
		return ErrInvalidCredentials
	}
	if err := s.q.DeleteTOTP(ctx, userID); err != nil {
		return err
	}
	return s.q.DeleteRecoveryCodes(ctx, userID)
}

func (s *Service) startChallenge(ctx context.Context, userID uuid.UUID) (*MFARequired, error) {
	tok, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	if err := s.q.CreateMFAChallenge(ctx, dbsqlc.CreateMFAChallengeParams{TokenHash: hashToken(tok), UserID: userID,
		ExpiresAt: s.now().Add(mfaChallengeLife)}); err != nil {
		return nil, err
	}
	return &MFARequired{Challenge: tok}, nil
}

// VerifyMFA completes a login: code is a TOTP code or a recovery code. After five wrong tries the
// challenge is void.
func (s *Service) VerifyMFA(ctx context.Context, challenge, code, userAgent string) (*Session, error) {
	h := hashToken(challenge)
	ch, err := s.q.GetMFAChallenge(ctx, h)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	if n, err := s.q.BumpMFAAttempts(ctx, h); err != nil || int(n) > maxMFAAttempts {
		_ = s.q.DeleteMFAChallenge(ctx, h)
		return nil, ErrTooManyTries
	}
	ok, err := s.checkTOTP(ctx, s.q, ch.UserID, code)
	if err != nil {
		return nil, err
	}
	if !ok {
		n, err := s.q.UseRecoveryCode(ctx, dbsqlc.UseRecoveryCodeParams{UserID: ch.UserID, CodeHash: recoveryHash(code)})
		if err != nil {
			return nil, err
		}
		ok = n == 1
	}
	if !ok {
		return nil, ErrInvalidCode
	}
	_ = s.q.DeleteMFAChallenge(ctx, h)
	return s.OpenSessionFor(ctx, ch.UserID, userAgent)
}

// RequestReset emails a one-hour reset link if the address belongs to an account. It always
// returns nil for unknown addresses, so it cannot be used to discover accounts.
func (s *Service) RequestReset(ctx context.Context, email, baseURL string) error {
	if s.mailer == nil {
		return ErrNoMailer
	}
	u, err := s.q.GetUserByEmail(ctx, strings.TrimSpace(email))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && u.DisabledAt != nil) {
		return nil
	}
	if err != nil {
		return err
	}
	tok, err := randomToken(32)
	if err != nil {
		return err
	}
	if err := s.q.CreatePasswordReset(ctx, dbsqlc.CreatePasswordResetParams{TokenHash: hashToken(tok), UserID: u.ID,
		ExpiresAt: s.now().Add(resetLife)}); err != nil {
		return err
	}
	body := fmt.Sprintf("A password reset was requested for your Biblisann account.\n\n%s/reset-password?token=%s\n\nThe link works once for one hour. If you did not ask for it, ignore this message.\n", baseURL, tok)
	return s.mailer.SendMail(ctx, u.Email, "Reset your Biblisann password", body)
}

// Reset sets a new password with a reset token, ends every session, and removes the second
// factor only if the user also loses access to it (it stays: a reset must not downgrade security).
func (s *Service) Reset(ctx context.Context, token, newPassword string) error {
	if len(token) > 128 {
		return ErrInvalidToken
	}
	// Check the policy before spending the token.
	probe := strings.TrimSpace(newPassword)
	if err := CheckPassword(probe, "", ""); err != nil {
		return err
	}
	uid, err := s.q.UsePasswordReset(ctx, hashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidToken
	}
	if err != nil {
		return err
	}
	u, err := s.q.GetUser(ctx, uid)
	if err != nil {
		return err
	}
	if err := CheckPassword(newPassword, u.Email, u.DisplayName); err != nil {
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.q.UpdateUserPassword(ctx, dbsqlc.UpdateUserPasswordParams{ID: uid, PasswordHash: &hash}); err != nil {
		return err
	}
	return s.q.DeleteUserSessions(ctx, uid)
}
