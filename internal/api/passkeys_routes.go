package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/larueli/biblisann/internal/passkeys"
)

type ceremony struct {
	SessionID string          `json:"session_id"`
	Options   json.RawMessage `json:"options" doc:"WebAuthn options to pass to navigator.credentials (as JSON)."`
}

func (s *Server) passkeyErr(err error) error {
	switch {
	case errors.Is(err, passkeys.ErrBadCeremony), errors.Is(err, passkeys.ErrUnknown):
		return huma.Error401Unauthorized("passkey verification failed")
	case errors.Is(err, passkeys.ErrDisabled):
		return huma.Error401Unauthorized("this account is disabled")
	}
	s.Log.Error("passkeys", "error", err)
	return huma.Error500InternalServerError("internal error")
}

func (s *Server) registerPasskeys(a huma.API) {
	off := func() error { return huma.Error404NotFound("passkeys are disabled on this server") }

	Register(a, huma.Operation{OperationID: "passkey-login-begin", Method: http.MethodPost, Path: "/v1/auth/passkey/begin",
		Summary: "Start a username-less passkey sign-in", Tags: []string{"auth"}, Errors: []int{404, 429}}, PermPublic,
		func(ctx context.Context, _ *struct{}) (*struct{ Body ceremony }, error) {
			if s.Passkeys == nil {
				return nil, off()
			}
			if err := s.throttle(ctx, "passkey", ""); err != nil {
				return nil, err
			}
			id, opts, err := s.Passkeys.BeginLogin(ctx)
			if err != nil {
				return nil, s.passkeyErr(err)
			}
			return &struct{ Body ceremony }{ceremony{id, opts}}, nil
		})
	Register(a, huma.Operation{OperationID: "passkey-login-finish", Method: http.MethodPost, Path: "/v1/auth/passkey/finish",
		Summary: "Finish a passkey sign-in with the authenticator's answer", Tags: []string{"auth"}, Errors: []int{401, 404, 429}}, PermPublic,
		func(ctx context.Context, in *struct {
			Body struct {
				SessionID  string          `json:"session_id" maxLength:"64"`
				Credential json.RawMessage `json:"credential"`
			}
		}) (*loginOutput, error) {
			if s.Passkeys == nil {
				return nil, off()
			}
			if err := s.throttle(ctx, "passkey", ""); err != nil {
				return nil, err
			}
			uid, err := s.Passkeys.FinishLogin(ctx, in.Body.SessionID, in.Body.Credential)
			if err != nil {
				return nil, s.passkeyErr(err)
			}
			sess, err := s.Auth.OpenSessionFor(ctx, uid, "")
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return s.loginOK(sess), nil
		})
	Register(a, huma.Operation{OperationID: "passkey-register-begin", Method: http.MethodPost, Path: "/v1/me/passkeys/begin",
		Summary: "Start adding a passkey to my account", Tags: []string{"auth"}, Errors: []int{403, 404}}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct{ Body ceremony }, error) {
			if s.Passkeys == nil {
				return nil, off()
			}
			sess := SessionFrom(ctx)
			if sess == nil {
				return nil, huma.Error403Forbidden("a browser session is required")
			}
			id, opts, err := s.Passkeys.BeginRegistration(ctx, sess.UserID)
			if err != nil {
				return nil, s.passkeyErr(err)
			}
			return &struct{ Body ceremony }{ceremony{id, opts}}, nil
		})
	Register(a, huma.Operation{OperationID: "passkey-register-finish", Method: http.MethodPost, Path: "/v1/me/passkeys/finish",
		Summary: "Finish adding a passkey", Tags: []string{"auth"}, DefaultStatus: http.StatusNoContent, Errors: []int{401, 403, 404}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			Body struct {
				SessionID  string          `json:"session_id" maxLength:"64"`
				Name       string          `json:"name,omitempty" maxLength:"100"`
				Credential json.RawMessage `json:"credential"`
			}
		}) (*struct{}, error) {
			if s.Passkeys == nil {
				return nil, off()
			}
			sess := SessionFrom(ctx)
			if sess == nil {
				return nil, huma.Error403Forbidden("a browser session is required")
			}
			if err := s.Passkeys.FinishRegistration(ctx, sess.UserID, in.Body.SessionID, in.Body.Name, in.Body.Credential); err != nil {
				return nil, s.passkeyErr(err)
			}
			return nil, nil
		})

	type passkeyBody struct {
		ID        string     `json:"id"`
		Name      string     `json:"name"`
		CreatedAt time.Time  `json:"created_at"`
		LastUsed  *time.Time `json:"last_used,omitempty"`
	}
	Register(a, huma.Operation{OperationID: "list-passkeys", Method: http.MethodGet, Path: "/v1/me/passkeys",
		Summary: "My passkeys", Tags: []string{"auth"}}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct{ Body []passkeyBody }, error) {
			out := []passkeyBody{}
			sess := SessionFrom(ctx)
			if s.Passkeys != nil && sess != nil {
				rows, err := s.Passkeys.List(ctx, sess.UserID)
				if err != nil {
					return nil, s.passkeyErr(err)
				}
				for _, r := range rows {
					out = append(out, passkeyBody{r.ID, r.Name, r.CreatedAt, r.LastUsed})
				}
			}
			return &struct{ Body []passkeyBody }{out}, nil
		})
	Register(a, huma.Operation{OperationID: "delete-passkey", Method: http.MethodDelete, Path: "/v1/me/passkeys/{id}",
		Summary: "Remove a passkey", Tags: []string{"auth"}, DefaultStatus: http.StatusNoContent, Errors: []int{404}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			ID string `path:"id" maxLength:"200"`
		}) (*struct{}, error) {
			sess := SessionFrom(ctx)
			if s.Passkeys == nil || sess == nil {
				return nil, off()
			}
			if err := s.Passkeys.Delete(ctx, sess.UserID, in.ID); err != nil {
				if errors.Is(err, passkeys.ErrUnknown) {
					return nil, huma.Error404NotFound("not found")
				}
				return nil, s.passkeyErr(err)
			}
			return nil, nil
		})
}
