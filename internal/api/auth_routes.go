package api

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"net/netip"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/httpmw"
)

type userBody struct {
	ID      uuid.UUID `json:"id"`
	Email   string    `json:"email"`
	Name    string    `json:"name"`
	IsAdmin bool      `json:"is_admin"`
	Locale  string    `json:"locale"`
}

type sessionBody struct {
	User        userBody   `json:"user"`
	CSRFToken   string     `json:"csrf_token" doc:"Send as X-CSRF-Token on every non-GET request made with the session cookie."`
	ExpiresAt   time.Time  `json:"expires_at"`
	StepUpUntil *time.Time `json:"step_up_until,omitempty" doc:"Set while a recent re-authentication unlocks sensitive actions."`
}

type loginBody struct {
	User        *userBody  `json:"user,omitempty"`
	CSRFToken   string     `json:"csrf_token,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	MFARequired bool       `json:"mfa_required"`
	MFAToken    string     `json:"mfa_token,omitempty" doc:"Present when mfa_required: send it to /v1/auth/mfa with a code."`
}

type loginOutput struct {
	SetCookie *http.Cookie `header:"Set-Cookie"`
	Body      loginBody
}

type sessionOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      sessionBody
}

type credentials struct {
	Email    string `json:"email" maxLength:"320" doc:"Account email address."`
	Password string `json:"password" maxLength:"256" doc:"Password (10 characters minimum)."`
	Name     string `json:"name,omitempty" maxLength:"200" doc:"Display name (registration only)."`
}

func (s *Server) allow(ctx context.Context, kind, key string, l interface {
	Allow(string) (bool, time.Duration)
}) error {
	if l == nil {
		return nil
	}
	if ok, wait := l.Allow(kind + "|" + key); !ok {
		return huma.NewError(http.StatusTooManyRequests, "too many attempts, retry in "+wait.Round(time.Second).String())
	}
	return nil
}

func (s *Server) throttle(ctx context.Context, kind, email string) error {
	if s.Limiter == nil {
		return nil
	}
	ip := ipKey(httpmw.ClientIP(ctx))
	if err := s.allow(ctx, kind+"-ip", ip, s.Limiter); err != nil {
		return err
	}
	if email != "" {
		return s.allow(ctx, kind+"-account", email, s.Limiter)
	}
	return nil
}

func (s *Server) registerAuth(a huma.API) {
	Register(a, huma.Operation{OperationID: "register", Method: http.MethodPost, Path: "/v1/auth/register",
		Summary: "Create an account (first account becomes the administrator)", Tags: []string{"auth"},
		DefaultStatus: http.StatusCreated, Errors: []int{403, 409, 422, 429}}, PermPublic,
		func(ctx context.Context, in *struct{ Body credentials }) (*sessionOutput, error) {
			if err := s.throttle(ctx, "register", ""); err != nil {
				return nil, err
			}
			if _, err := mail.ParseAddress(in.Body.Email); err != nil {
				return nil, huma.Error422UnprocessableEntity("invalid email address")
			}
			u, err := s.Auth.Register(ctx, in.Body.Email, in.Body.Password, in.Body.Name)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return s.open(ctx, u.ID)
		})

	Register(a, huma.Operation{OperationID: "login", Method: http.MethodPost, Path: "/v1/auth/login",
		Summary:     "Sign in with email and password",
		Description: "When the account has a second factor the answer is 200 with mfa_required=true and an mfa_token, and no cookie: finish with /v1/auth/mfa.",
		Tags:        []string{"auth"}, Errors: []int{401, 429}}, PermPublic,
		func(ctx context.Context, in *struct{ Body credentials }) (*loginOutput, error) {
			if err := s.throttle(ctx, "login", lower(in.Body.Email)); err != nil {
				return nil, err
			}
			sess, err := s.Auth.Login(ctx, in.Body.Email, in.Body.Password, "")
			var mfa *auth.MFARequired
			if errors.As(err, &mfa) {
				return &loginOutput{Body: loginBody{MFARequired: true, MFAToken: mfa.Challenge}}, nil
			}
			if errors.Is(err, auth.ErrMFADisabled) {
				return nil, huma.Error503ServiceUnavailable("two-factor authentication is on for this account but the server cannot check it; contact the administrator")
			}
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return s.loginOK(sess), nil
		})

	Register(a, huma.Operation{OperationID: "complete-mfa", Method: http.MethodPost, Path: "/v1/auth/mfa",
		Summary: "Finish a sign-in with an authenticator code or a recovery code", Tags: []string{"auth"}, Errors: []int{401, 429}}, PermPublic,
		func(ctx context.Context, in *struct {
			Body struct {
				MFAToken string `json:"mfa_token" maxLength:"128"`
				Code     string `json:"code" maxLength:"32"`
			}
		}) (*loginOutput, error) {
			if err := s.throttle(ctx, "mfa", ""); err != nil {
				return nil, err
			}
			sess, err := s.Auth.VerifyMFA(ctx, in.Body.MFAToken, in.Body.Code, "")
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return s.loginOK(sess), nil
		})

	Register(a, huma.Operation{OperationID: "request-password-reset", Method: http.MethodPost, Path: "/v1/auth/password-reset",
		Summary: "Email a password reset link (always answers 204, whether or not the address exists)", Tags: []string{"auth"},
		DefaultStatus: http.StatusNoContent, Errors: []int{429, 503}}, PermPublic,
		func(ctx context.Context, in *struct {
			Body struct {
				Email string `json:"email" maxLength:"320"`
			}
		}) (*struct{}, error) {
			if !s.Auth.ResetAvailable() {
				return nil, huma.Error503ServiceUnavailable("password reset is not available on this server")
			}
			if err := s.throttle(ctx, "reset", lower(in.Body.Email)); err != nil {
				return nil, err
			}
			if err := s.Auth.RequestReset(ctx, in.Body.Email, s.BaseURL.String()); err != nil {
				s.Log.Warn("password reset mail failed", "error", err) // never tell the caller
			}
			return nil, nil
		})
	Register(a, huma.Operation{OperationID: "confirm-password-reset", Method: http.MethodPost, Path: "/v1/auth/password-reset/confirm",
		Summary: "Set a new password with the token from the reset email", Tags: []string{"auth"},
		DefaultStatus: http.StatusNoContent, Errors: []int{401, 422, 429}}, PermPublic,
		func(ctx context.Context, in *struct {
			Body struct {
				Token    string `json:"token" maxLength:"128"`
				Password string `json:"new_password" maxLength:"256"`
			}
		}) (*struct{}, error) {
			if err := s.throttle(ctx, "reset-confirm", ""); err != nil {
				return nil, err
			}
			return nil, mapError(s.Log, s.Auth.Reset(ctx, in.Body.Token, in.Body.Password))
		})

	type securityBody struct {
		MFAAvailable      bool `json:"mfa_available"`
		MFAEnabled        bool `json:"mfa_enabled"`
		ResetAvailable    bool `json:"reset_available"`
		PasskeysAvailable bool `json:"passkeys_available"`
		HasPassword       bool `json:"has_password" doc:"The account can re-authenticate with a password."`
		SSOAvailable      bool `json:"sso_available" doc:"Single sign-on is configured: it can re-authenticate."`
	}
	Register(a, huma.Operation{OperationID: "get-security", Method: http.MethodGet, Path: "/v1/me/security",
		Summary: "Which sign-in protections exist on this server and which I use", Tags: []string{"auth"}}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct{ Body securityBody }, error) {
			p, err := authz.From(ctx)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return &struct{ Body securityBody }{securityBody{s.Auth.MFAAvailable(), s.Auth.IsMFAEnabled(ctx, p.UserID), s.Auth.ResetAvailable(),
				s.Passkeys != nil, s.Auth.HasPassword(ctx, p.UserID), s.OIDC != nil}}, nil
		})
	Register(a, huma.Operation{OperationID: "enroll-totp", Method: http.MethodPost, Path: "/v1/me/mfa/totp",
		Summary: "Start authenticator-app enrollment", Tags: []string{"auth"}, Errors: []int{503}}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct {
			Body struct {
				Secret string `json:"secret" doc:"Base32 secret for manual entry."`
				URI    string `json:"uri" doc:"otpauth:// URI to render as a QR code."`
			}
		}, error) {
			if err := s.requireStepUp(ctx); err != nil {
				return nil, err
			}
			p, _ := authz.From(ctx)
			secret, uri, err := s.Auth.EnrollTOTP(ctx, p.UserID, "Biblisann")
			if errors.Is(err, auth.ErrMFADisabled) {
				return nil, huma.Error503ServiceUnavailable(err.Error())
			}
			if errors.Is(err, auth.ErrMFAActive) {
				return nil, huma.Error409Conflict(err.Error())
			}
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := &struct {
				Body struct {
					Secret string `json:"secret" doc:"Base32 secret for manual entry."`
					URI    string `json:"uri" doc:"otpauth:// URI to render as a QR code."`
				}
			}{}
			out.Body.Secret, out.Body.URI = secret, uri
			return out, nil
		})
	Register(a, huma.Operation{OperationID: "confirm-totp", Method: http.MethodPost, Path: "/v1/me/mfa/totp/confirm",
		Summary: "Confirm enrollment with a first code; returns one-time recovery codes", Tags: []string{"auth"}, Errors: []int{422}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			Body struct {
				Code string `json:"code" maxLength:"16"`
			}
		}) (*struct {
			Body struct {
				RecoveryCodes []string `json:"recovery_codes" doc:"Shown once. Each works once."`
			}
		}, error) {
			if SessionFrom(ctx) == nil {
				return nil, huma.Error403Forbidden("a browser session is required")
			}
			p, _ := authz.From(ctx)
			if err := s.throttle(ctx, "totp-confirm", p.UserID.String()); err != nil {
				return nil, err
			}
			codes, err := s.Auth.ConfirmTOTP(ctx, p.UserID, in.Body.Code)
			if errors.Is(err, auth.ErrInvalidCode) {
				return nil, huma.Error422UnprocessableEntity("invalid code")
			}
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := &struct {
				Body struct {
					RecoveryCodes []string `json:"recovery_codes" doc:"Shown once. Each works once."`
				}
			}{}
			out.Body.RecoveryCodes = codes
			return out, nil
		})
	Register(a, huma.Operation{OperationID: "disable-totp", Method: http.MethodDelete, Path: "/v1/me/mfa/totp",
		Summary: "Turn the authenticator off (password required)", Tags: []string{"auth"}, DefaultStatus: http.StatusNoContent, Errors: []int{401}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			Body struct {
				Password string `json:"password" maxLength:"256"`
			}
		}) (*struct{}, error) {
			if err := s.requireStepUp(ctx); err != nil {
				return nil, err
			}
			p, _ := authz.From(ctx)
			if err := s.throttle(ctx, "totp-disable", p.UserID.String()); err != nil {
				return nil, err
			}
			return nil, mapError(s.Log, s.Auth.DisableTOTP(ctx, p.UserID, in.Body.Password))
		})
	Register(a, huma.Operation{OperationID: "get-session", Method: http.MethodGet, Path: "/v1/auth/session",
		Summary: "Current user and CSRF token", Tags: []string{"auth"}}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct{ Body sessionBody }, error) {
			sess := SessionFrom(ctx)
			if sess == nil {
				return nil, huma.Error401Unauthorized("a browser session is required")
			}
			return &struct{ Body sessionBody }{sessionBody{User: userBody{ID: sess.UserID, Email: sess.Email, Name: sess.Name,
				IsAdmin: sess.IsAdmin, Locale: sess.Locale}, CSRFToken: sess.CSRFToken, ExpiresAt: sess.ExpiresAt,
				StepUpUntil: s.stepUpUntil(sess)}}, nil
		})

	Register(a, huma.Operation{OperationID: "logout", Method: http.MethodPost, Path: "/v1/auth/logout",
		Summary: "Sign out", Tags: []string{"auth"}, DefaultStatus: http.StatusNoContent}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct {
			SetCookie http.Cookie `header:"Set-Cookie"`
		}, error) {
			if tok := sessionTokenFrom(ctx); tok != "" {
				_ = s.Auth.Logout(ctx, tok)
			}
			return &struct {
				SetCookie http.Cookie `header:"Set-Cookie"`
			}{SetCookie: *clearSessionCookie(s.secure)}, nil
		})

	Register(a, huma.Operation{OperationID: "change-password", Method: http.MethodPost, Path: "/v1/auth/password",
		Summary: "Change the password (ends all sessions)", Tags: []string{"auth"}, DefaultStatus: http.StatusNoContent,
		Errors: []int{401, 422}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			Body struct {
				Current string `json:"current" maxLength:"256"`
				New     string `json:"new" maxLength:"256"`
			}
		}) (*struct{}, error) {
			sess := SessionFrom(ctx)
			if sess == nil {
				return nil, huma.Error401Unauthorized("a browser session is required")
			}
			if err := s.requireStepUp(ctx); err != nil {
				return nil, err
			}
			if err := s.throttle(ctx, "password", sess.UserID.String()); err != nil {
				return nil, err
			}
			if err := s.Auth.ChangePassword(ctx, sess.UserID, in.Body.Current, in.Body.New); err != nil {
				return nil, mapError(s.Log, err)
			}
			return nil, nil
		})
}

func (s *Server) loginOK(sess *auth.Session) *loginOutput {
	u := userBody{ID: sess.UserID, Email: sess.Email, Name: sess.Name, IsAdmin: sess.IsAdmin, Locale: sess.Locale}
	exp := sess.ExpiresAt
	return &loginOutput{SetCookie: newSessionCookie(s.secure, sess.Token, exp), Body: loginBody{User: &u, CSRFToken: sess.CSRFToken, ExpiresAt: &exp}}
}

// lower is the throttle key of an email address. It trims and lowercases exactly like the account
// lookup does, so every spelling of one address shares one bucket.
func lower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func (s *Server) sessionOut(token, csrf string, exp time.Time, u userBody) *sessionOutput {
	return &sessionOutput{SetCookie: *newSessionCookie(s.secure, token, exp),
		Body: sessionBody{User: u, CSRFToken: csrf, ExpiresAt: exp}}
}

func (s *Server) open(ctx context.Context, userID uuid.UUID) (*sessionOutput, error) {
	sess, err := s.Auth.OpenSessionFor(ctx, userID, "")
	if err != nil {
		return nil, mapError(s.Log, err)
	}
	return s.sessionOut(sess.Token, sess.CSRFToken, sess.ExpiresAt, userBody{ID: sess.UserID, Email: sess.Email,
		Name: sess.Name, IsAdmin: sess.IsAdmin, Locale: sess.Locale}), nil
}

func clientKey(r *http.Request) string { return ipKey(httpmw.ClientIP(r.Context())) }

// ipKey is the rate-limit key of a client: an IPv6 client is identified by its /64, because a single
// subscriber usually controls that whole prefix and could otherwise rotate addresses freely.
func ipKey(a netip.Addr) string {
	a = a.Unmap()
	if a.Is6() {
		if p, err := a.Prefix(64); err == nil {
			return p.String()
		}
	}
	return a.String()
}

// stepUpUntil is the end of the step-up window, or nil when it is closed.
func (s *Server) stepUpUntil(sess *auth.Session) *time.Time {
	if s.Auth.StepUpValid(sess) {
		return sess.StepUpUntil
	}
	return nil
}
