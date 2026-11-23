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
	UserID    uuid.UUID
	Email     string
	Name      string
	IsAdmin   bool
	Locale    string
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
	if s.secretKey != nil && s.hasMFA(ctx, u.ID) {
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
	return &Session{CSRFToken: row.CsrfToken, ExpiresAt: row.ExpiresAt, UserID: row.UserID, Email: row.Email,
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
