package api

import (
	"context"
	"net/http"
	"net/mail"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

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
	User      userBody  `json:"user"`
	CSRFToken string    `json:"csrf_token" doc:"Send as X-CSRF-Token on every non-GET request made with the session cookie."`
	ExpiresAt time.Time `json:"expires_at"`
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
	ip := httpmw.ClientIP(ctx).String()
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
		Summary: "Sign in with email and password", Tags: []string{"auth"}, Errors: []int{401, 429}}, PermPublic,
		func(ctx context.Context, in *struct{ Body credentials }) (*sessionOutput, error) {
			if err := s.throttle(ctx, "login", lower(in.Body.Email)); err != nil {
				return nil, err
			}
			sess, err := s.Auth.Login(ctx, in.Body.Email, in.Body.Password, "")
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return s.sessionOut(sess.Token, sess.CSRFToken, sess.ExpiresAt, userBody{ID: sess.UserID, Email: sess.Email,
				Name: sess.Name, IsAdmin: sess.IsAdmin, Locale: sess.Locale}), nil
		})

	Register(a, huma.Operation{OperationID: "get-session", Method: http.MethodGet, Path: "/v1/auth/session",
		Summary: "Current user and CSRF token", Tags: []string{"auth"}}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct{ Body sessionBody }, error) {
			sess := SessionFrom(ctx)
			if sess == nil {
				return nil, huma.Error401Unauthorized("a browser session is required")
			}
			return &struct{ Body sessionBody }{sessionBody{User: userBody{ID: sess.UserID, Email: sess.Email, Name: sess.Name,
				IsAdmin: sess.IsAdmin, Locale: sess.Locale}, CSRFToken: sess.CSRFToken, ExpiresAt: sess.ExpiresAt}}, nil
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
			if err := s.throttle(ctx, "password", sess.UserID.String()); err != nil {
				return nil, err
			}
			if err := s.Auth.ChangePassword(ctx, sess.UserID, in.Body.Current, in.Body.New); err != nil {
				return nil, mapError(s.Log, err)
			}
			return nil, nil
		})
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

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
