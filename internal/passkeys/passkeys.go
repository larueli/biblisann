// Package passkeys implements passwordless sign-in with WebAuthn (passkeys): discoverable
// credentials with mandatory user verification, so a passkey is a complete multi-factor login.
// It can be turned off by configuration (for example when sign-in is delegated to OIDC).
package passkeys

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

// Errors.
var (
	ErrBadCeremony = errors.New("invalid or expired passkey request")
	ErrUnknown     = errors.New("unknown passkey")
	ErrDisabled    = errors.New("this account is disabled")
	ceremonyLife   = 5 * time.Minute
)

// Service runs WebAuthn ceremonies.
type Service struct {
	wa *webauthn.WebAuthn
	q  *dbsqlc.Queries
}

// New creates the service for the public origin (BASE_URL).
func New(pool *pgxpool.Pool, baseURL *url.URL, rpName string) (*Service, error) {
	wa, err := webauthn.New(&webauthn.Config{
		RPDisplayName: rpName, RPID: baseURL.Hostname(), RPOrigins: []string{baseURL.Scheme + "://" + baseURL.Host},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey: protocol.ResidentKeyRequirementRequired, UserVerification: protocol.VerificationRequired},
	})
	if err != nil {
		return nil, fmt.Errorf("passkeys: %w", err)
	}
	return &Service{wa: wa, q: dbsqlc.New(pool)}, nil
}

type user struct {
	u     dbsqlc.User
	creds []webauthn.Credential
}

func (w *user) WebAuthnID() []byte                         { b, _ := w.u.ID.MarshalBinary(); return b }
func (w *user) WebAuthnName() string                       { return w.u.Email }
func (w *user) WebAuthnDisplayName() string                { return firstNonEmpty(w.u.DisplayName, w.u.Email) }
func (w *user) WebAuthnCredentials() []webauthn.Credential { return w.creds }

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (s *Service) load(ctx context.Context, id uuid.UUID) (*user, error) {
	u, err := s.q.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListCredentials(ctx, id)
	if err != nil {
		return nil, err
	}
	w := &user{u: u}
	for _, r := range rows {
		var c webauthn.Credential
		if json.Unmarshal(r.Credential, &c) == nil {
			w.creds = append(w.creds, c)
		}
	}
	return w, nil
}

func (s *Service) saveSession(ctx context.Context, uid *uuid.UUID, kind string, sd *webauthn.SessionData) (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(b)
	data, _ := json.Marshal(sd)
	if err := s.q.CreateWebAuthnSession(ctx, dbsqlc.CreateWebAuthnSessionParams{ID: id, UserID: uid, Kind: kind, Data: data,
		ExpiresAt: time.Now().Add(ceremonyLife)}); err != nil {
		return "", err
	}
	_ = s.q.PurgeWebAuthnSessions(ctx)
	return id, nil
}

func (s *Service) takeSession(ctx context.Context, id, kind string) (*webauthn.SessionData, *uuid.UUID, error) {
	row, err := s.q.ConsumeWebAuthnSession(ctx, dbsqlc.ConsumeWebAuthnSessionParams{ID: id, Kind: kind})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrBadCeremony
	}
	if err != nil {
		return nil, nil, err
	}
	var sd webauthn.SessionData
	if err := json.Unmarshal(row.Data, &sd); err != nil {
		return nil, nil, ErrBadCeremony
	}
	return &sd, row.UserID, nil
}

// BeginRegistration starts adding a passkey to the signed-in user's account.
func (s *Service) BeginRegistration(ctx context.Context, userID uuid.UUID) (sessionID string, options json.RawMessage, err error) {
	u, err := s.load(ctx, userID)
	if err != nil {
		return "", nil, err
	}
	creation, sd, err := s.wa.BeginRegistration(u, webauthn.WithExclusions(webauthn.Credentials(u.creds).CredentialDescriptors()))
	if err != nil {
		return "", nil, fmt.Errorf("passkeys: begin registration: %w", err)
	}
	id, err := s.saveSession(ctx, &userID, "register", sd)
	if err != nil {
		return "", nil, err
	}
	options, _ = json.Marshal(creation)
	return id, options, nil
}

// FinishRegistration verifies the authenticator's answer and stores the credential.
func (s *Service) FinishRegistration(ctx context.Context, userID uuid.UUID, sessionID, name string, response []byte) error {
	sd, owner, err := s.takeSession(ctx, sessionID, "register")
	if err != nil {
		return err
	}
	if owner == nil || *owner != userID {
		return ErrBadCeremony
	}
	u, err := s.load(ctx, userID)
	if err != nil {
		return err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return ErrBadCeremony
	}
	cred, err := s.wa.CreateCredential(u, *sd, parsed)
	if err != nil {
		return ErrBadCeremony
	}
	if name == "" || len(name) > 100 {
		name = "Passkey"
	}
	data, _ := json.Marshal(cred)
	return s.q.AddCredential(ctx, dbsqlc.AddCredentialParams{ID: cred.ID, UserID: userID, Name: name, Credential: data})
}

// BeginLogin starts a discoverable (username-less) sign-in.
func (s *Service) BeginLogin(ctx context.Context) (sessionID string, options json.RawMessage, err error) {
	assertion, sd, err := s.wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return "", nil, fmt.Errorf("passkeys: begin login: %w", err)
	}
	id, err := s.saveSession(ctx, nil, "login", sd)
	if err != nil {
		return "", nil, err
	}
	options, _ = json.Marshal(assertion)
	return id, options, nil
}

// FinishLogin verifies the assertion and returns the user to sign in.
func (s *Service) FinishLogin(ctx context.Context, sessionID string, response []byte) (uuid.UUID, error) {
	sd, _, err := s.takeSession(ctx, sessionID, "login")
	if err != nil {
		return uuid.Nil, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return uuid.Nil, ErrBadCeremony
	}
	var found *user
	handler := func(_, userHandle []byte) (webauthn.User, error) {
		id, err := uuid.FromBytes(userHandle)
		if err != nil {
			return nil, ErrUnknown
		}
		u, err := s.load(ctx, id)
		if err != nil {
			return nil, ErrUnknown
		}
		found = u
		return u, nil
	}
	cred, err := s.wa.ValidateDiscoverableLogin(handler, *sd, parsed)
	if err != nil || found == nil {
		return uuid.Nil, ErrBadCeremony
	}
	if found.u.DisabledAt != nil {
		return uuid.Nil, ErrDisabled
	}
	// Store the new signature counter: a clone detected later shows as a counter regression.
	data, _ := json.Marshal(cred)
	_ = s.q.UpdateCredential(ctx, dbsqlc.UpdateCredentialParams{ID: cred.ID, Credential: data})
	return found.u.ID, nil
}

// Passkey is a registered credential as shown to its owner.
type Passkey struct {
	ID        string
	Name      string
	CreatedAt time.Time
	LastUsed  *time.Time
}

// List returns the user's passkeys.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Passkey, error) {
	rows, err := s.q.ListCredentials(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Passkey, len(rows))
	for i, r := range rows {
		out[i] = Passkey{base64.RawURLEncoding.EncodeToString(r.ID), r.Name, r.CreatedAt, r.LastUsed}
	}
	return out, nil
}

// Delete removes one of the user's passkeys by its base64url ID.
func (s *Service) Delete(ctx context.Context, userID uuid.UUID, id string) error {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return ErrUnknown
	}
	n, err := s.q.DeleteCredential(ctx, dbsqlc.DeleteCredentialParams{ID: raw, UserID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrUnknown
	}
	return nil
}
