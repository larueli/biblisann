package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

// Errors surfaced to handlers.
var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrRegistrationClosed = errors.New("registration is closed")
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidEmail       = errors.New("invalid email address")
	ErrSessionNotFound    = errors.New("session not found")
)

// SessionLifetime is the absolute lifetime of a login.
const SessionLifetime = 30 * 24 * time.Hour

// RegistrationMode controls who may create an account through the public endpoint.
type RegistrationMode string

// Registration modes. FirstUser lets only the very first account register (it becomes admin).
const (
	RegistrationFirstUser RegistrationMode = "first-user"
	RegistrationOpen      RegistrationMode = "open"
	RegistrationClosed    RegistrationMode = "closed"
)

// Service is the local-account service.
type Service struct {
	pool *pgxpool.Pool
	q    *dbsqlc.Queries
	mode RegistrationMode
	// dummyHash is verified when the account does not exist, so response time does not reveal
	// which emails are registered.
	dummyHash string
	now       func() time.Time
	secretKey []byte // enables TOTP
	mailer    Mailer // enables password reset
}

// NewService creates the service.
func NewService(pool *pgxpool.Pool, mode RegistrationMode) (*Service, error) {
	dummy, err := HashPassword("dummy password for timing equalization")
	if err != nil {
		return nil, err
	}
	return &Service{pool: pool, q: dbsqlc.New(pool), mode: mode, dummyHash: dummy, now: time.Now}, nil
}

// Session is an authenticated browser session.
type Session struct {
	Token     string // the cookie value, only known at creation
	CSRFToken string
	ExpiresAt time.Time
	// StepUpUntil is when the last re-authentication stops counting (nil: never done).
	StepUpUntil *time.Time
	UserID      uuid.UUID
	Email       string
	Name        string
	IsAdmin     bool
	Locale      string
}

// NormalizeEmail validates and canonicalizes an address (trimmed, bare address, max 320 bytes).
func NormalizeEmail(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 320 {
		return "", ErrInvalidEmail
	}
	a, err := mail.ParseAddress(raw)
	if err != nil || a.Address != raw || !strings.Contains(a.Address, "@") {
		return "", ErrInvalidEmail
	}
	return a.Address, nil
}

// Register creates the first or an additional account according to the registration mode and
// returns the user. Admin-created accounts use CreateUser directly.
func (s *Service) Register(ctx context.Context, email, password, name string) (*dbsqlc.User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return nil, err
	}
	if err := CheckPassword(password, email, name); err != nil {
		return nil, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialize registrations so two concurrent "first users" cannot both become admin.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('biblisann.register'))"); err != nil {
		return nil, fmt.Errorf("auth: lock: %w", err)
	}
	q := s.q.WithTx(tx)
	n, err := q.CountUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: count users: %w", err)
	}
	switch {
	case s.mode == RegistrationClosed, s.mode == RegistrationFirstUser && n > 0:
		return nil, ErrRegistrationClosed
	}
	u, err := s.createUser(ctx, q, email, name, hash, n == 0)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("auth: commit: %w", err)
	}
	return u, nil
}

// CreateUser creates an account on behalf of an administrator.
func (s *Service) CreateUser(ctx context.Context, email, password, name string, admin bool) (*dbsqlc.User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return nil, err
	}
	if err := CheckPassword(password, email, name); err != nil {
		return nil, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	return s.createUser(ctx, s.q, email, name, hash, admin)
}

func (s *Service) createUser(ctx context.Context, q *dbsqlc.Queries, email, name, hash string, admin bool) (*dbsqlc.User, error) {
	u, err := q.CreateUser(ctx, dbsqlc.CreateUserParams{
		Email: email, DisplayName: name, PasswordHash: &hash, IsAdmin: admin, Locale: "",
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, ErrEmailTaken
	}
	if err != nil {
		return nil, fmt.Errorf("auth: create user: %w", err)
	}
	return &u, nil
}

// Login verifies credentials and opens a session.
func (s *Service) Login(ctx context.Context, email, password, userAgent string) (*Session, error) {
	u, err := s.q.GetUserByEmail(ctx, strings.TrimSpace(email))
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = VerifyPassword(password, s.dummyHash)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("auth: lookup: %w", err)
	}
	hash := s.dummyHash
	if u.PasswordHash != nil {
		hash = *u.PasswordHash
	}
	ok, verr := VerifyPassword(password, hash)
	if verr != nil || !ok || u.PasswordHash == nil || u.DisabledAt != nil {
		return nil, ErrInvalidCredentials
	}
	if s.hasMFA(ctx, u.ID) {
		if s.secretKey == nil {
			return nil, ErrMFADisabled // fail closed: never let a password alone open an account that has a second factor
		}
		ch, err := s.startChallenge(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		return nil, ch
	}
	return s.openSession(ctx, u, userAgent)
}

// OpenSessionFor opens a session for an already authenticated user (OIDC, passkey, 2FA).
func (s *Service) OpenSessionFor(ctx context.Context, userID uuid.UUID, userAgent string) (*Session, error) {
	u, err := s.q.GetUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("auth: load user: %w", err)
	}
	if u.DisabledAt != nil {
		return nil, ErrInvalidCredentials
	}
	return s.openSession(ctx, u, userAgent)
}

func (s *Service) openSession(ctx context.Context, u dbsqlc.User, userAgent string) (*Session, error) {
	token, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	csrf, err := randomToken(24)
	if err != nil {
		return nil, err
	}
	exp := s.now().Add(SessionLifetime)
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	err = s.q.CreateSession(ctx, dbsqlc.CreateSessionParams{
		TokenHash: hashToken(token), UserID: u.ID, CsrfToken: csrf, ExpiresAt: exp, UserAgent: userAgent,
	})
	if err != nil {
		return nil, fmt.Errorf("auth: create session: %w", err)
	}
	return &Session{Token: token, CSRFToken: csrf, ExpiresAt: exp, UserID: u.ID, Email: u.Email,
		Name: u.DisplayName, IsAdmin: u.IsAdmin, Locale: u.Locale}, nil
}

// Lookup resolves a cookie token. The returned Session has an empty Token.
func (s *Service) Lookup(ctx context.Context, token string) (*Session, error) {
	if token == "" || len(token) > 128 {
		return nil, ErrSessionNotFound
	}
	h := hashToken(token)
	row, err := s.q.GetSession(ctx, h)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("auth: session lookup: %w", err)
	}
	if row.DisabledAt != nil {
		return nil, ErrSessionNotFound
	}
	_ = s.q.TouchSession(ctx, h) // best effort
	return &Session{CSRFToken: row.CsrfToken, ExpiresAt: row.ExpiresAt, StepUpUntil: row.StepupUntil, UserID: row.UserID, Email: row.Email,
		Name: row.DisplayName, IsAdmin: row.IsAdmin, Locale: row.Locale}, nil
}

// Logout deletes the session behind a cookie token.
func (s *Service) Logout(ctx context.Context, token string) error {
	if err := s.q.DeleteSession(ctx, hashToken(token)); err != nil {
		return fmt.Errorf("auth: logout: %w", err)
	}
	return nil
}

// ChangePassword verifies the current password, sets a new one and ends every session.
func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, current, next string) error {
	u, err := s.q.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("auth: load user: %w", err)
	}
	if u.PasswordHash == nil {
		return ErrInvalidCredentials
	}
	if ok, _ := VerifyPassword(current, *u.PasswordHash); !ok {
		return ErrInvalidCredentials
	}
	if err := CheckPassword(next, u.Email, u.DisplayName); err != nil {
		return err
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	if err := s.q.UpdateUserPassword(ctx, dbsqlc.UpdateUserPasswordParams{ID: userID, PasswordHash: &hash}); err != nil {
		return fmt.Errorf("auth: update password: %w", err)
	}
	return s.q.DeleteUserSessions(ctx, userID)
}

// PurgeExpired deletes expired sessions and returns how many were removed.
func (s *Service) PurgeExpired(ctx context.Context) (int64, error) {
	return s.q.DeleteExpiredSessions(ctx)
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// SessionInfo describes a login for the "my sessions" screen. The token itself is never exposed.
type SessionInfo struct {
	ID        uuid.UUID
	CreatedAt time.Time
	LastSeen  time.Time
	ExpiresAt time.Time
	UserAgent string
	Current   bool
}

// Sessions lists the user's live sessions; currentToken marks the caller's own.
func (s *Service) Sessions(ctx context.Context, userID uuid.UUID, currentToken string) ([]SessionInfo, error) {
	rows, err := s.q.ListUserSessions(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("auth: list sessions: %w", err)
	}
	cur := hashToken(currentToken)
	out := make([]SessionInfo, len(rows))
	for i, r := range rows {
		out[i] = SessionInfo{r.ID, r.CreatedAt, r.LastSeen, r.ExpiresAt, r.UserAgent, currentToken != "" && string(r.TokenHash) == string(cur)}
	}
	return out, nil
}

// RevokeSession signs one of the user's sessions out. It reports ErrSessionNotFound for IDs that
// are not theirs.
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID uuid.UUID) error {
	n, err := s.q.DeleteSessionByID(ctx, dbsqlc.DeleteSessionByIDParams{ID: sessionID, UserID: userID})
	if err != nil {
		return fmt.Errorf("auth: revoke session: %w", err)
	}
	if n == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// RevokeOtherSessions signs the user out everywhere except the current session.
func (s *Service) RevokeOtherSessions(ctx context.Context, userID uuid.UUID, currentToken string) (int64, error) {
	return s.q.DeleteOtherSessions(ctx, dbsqlc.DeleteOtherSessionsParams{UserID: userID, TokenHash: hashToken(currentToken)})
}

// RevokeAllSessions signs a user out everywhere (administrator action).
func (s *Service) RevokeAllSessions(ctx context.Context, userID uuid.UUID) error {
	return s.q.DeleteUserSessions(ctx, userID)
}

// SetDisabled disables or re-enables an account. Disabling also ends its sessions.
func (s *Service) SetDisabled(ctx context.Context, userID uuid.UUID, disabled bool) error {
	var at *time.Time
	if disabled {
		now := s.now()
		at = &now
	}
	if err := s.q.SetUserDisabled(ctx, dbsqlc.SetUserDisabledParams{ID: userID, DisabledAt: at}); err != nil {
		return err
	}
	if disabled {
		return s.q.DeleteUserSessions(ctx, userID)
	}
	return nil
}

// ConfirmIdentity re-checks the caller before a destructive action: the typed email must be theirs
// and, for accounts with a password, the password must match.
func (s *Service) ConfirmIdentity(ctx context.Context, userID uuid.UUID, email, password string) error {
	u, err := s.q.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(email), u.Email) {
		return ErrInvalidCredentials
	}
	if u.PasswordHash != nil {
		if ok, _ := VerifyPassword(password, *u.PasswordHash); !ok {
			return ErrInvalidCredentials
		}
	}
	return nil
}

// ListUsers returns accounts for the administration screen.
func (s *Service) ListUsers(ctx context.Context, limit, offset int) ([]dbsqlc.ListUsersAdminRow, error) {
	return s.q.ListUsersAdmin(ctx, dbsqlc.ListUsersAdminParams{Limit: int32(limit), Offset: int32(offset)})
}
