package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/oidc"
)

// stepUpRequired is the problem detail clients match on to open the re-authentication dialog.
const stepUpRequired = "step_up_required"

// requireStepUp fails with 403 step_up_required unless the browser session re-authenticated within
// auth.StepUpWindow. Sensitive actions call it first (ADR 0026).
func (s *Server) requireStepUp(ctx context.Context) error {
	sess := SessionFrom(ctx)
	if sess == nil {
		return huma.Error403Forbidden("a browser session is required")
	}
	if !s.Auth.StepUpValid(sess) {
		return huma.Error403Forbidden(stepUpRequired)
	}
	return nil
}

type stepUpBody struct {
	Until time.Time `json:"until" doc:"The step-up window closes at this time (5 minutes after the last re-authentication)."`
}

func (s *Server) registerStepUp(a huma.API) {
	Register(a, huma.Operation{OperationID: "step-up", Method: http.MethodPost, Path: "/v1/auth/step-up",
		Summary: "Re-authenticate with the password (and a code when two-factor is on)",
		Description: "Opens a 5-minute window for sensitive actions (password, two-factor and passkey changes, new tokens, " +
			"account deletion). Accounts without a password use a passkey or the identity provider instead.",
		Tags: []string{"auth"}, Errors: []int{401, 403, 429}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			Body struct {
				Password string `json:"password" maxLength:"256"`
				Code     string `json:"code,omitempty" maxLength:"32" doc:"Authenticator or recovery code, when two-factor is on."`
			}
		}) (*struct{ Body stepUpBody }, error) {
			sess := SessionFrom(ctx)
			if sess == nil {
				return nil, huma.Error403Forbidden("a browser session is required")
			}
			if err := s.throttle(ctx, "step-up", sess.UserID.String()); err != nil {
				return nil, err
			}
			if err := s.Auth.StepUpWithPassword(ctx, sessionTokenFrom(ctx), sess.UserID, in.Body.Password, in.Body.Code); err != nil {
				return nil, mapError(s.Log, err)
			}
			return &struct{ Body stepUpBody }{stepUpBody{Until: time.Now().Add(auth.StepUpWindow)}}, nil
		})

	Register(a, huma.Operation{OperationID: "step-up-passkey-begin", Method: http.MethodPost, Path: "/v1/auth/step-up/passkey/begin",
		Summary: "Start a re-authentication with a passkey", Tags: []string{"auth"}, Errors: []int{403, 404, 429}}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct{ Body ceremony }, error) {
			if s.Passkeys == nil {
				return nil, huma.Error404NotFound("passkeys are disabled on this server")
			}
			sess := SessionFrom(ctx)
			if sess == nil {
				return nil, huma.Error403Forbidden("a browser session is required")
			}
			if err := s.throttle(ctx, "step-up", sess.UserID.String()); err != nil {
				return nil, err
			}
			id, opts, err := s.Passkeys.BeginLogin(ctx)
			if err != nil {
				return nil, s.passkeyErr(err)
			}
			return &struct{ Body ceremony }{ceremony{id, opts}}, nil
		})

	Register(a, huma.Operation{OperationID: "step-up-passkey-finish", Method: http.MethodPost, Path: "/v1/auth/step-up/passkey/finish",
		Summary: "Finish a re-authentication with a passkey of this account", Tags: []string{"auth"}, Errors: []int{401, 403, 404, 429}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			Body struct {
				SessionID  string          `json:"session_id" maxLength:"64"`
				Credential json.RawMessage `json:"credential"`
			}
		}) (*struct{ Body stepUpBody }, error) {
			if s.Passkeys == nil {
				return nil, huma.Error404NotFound("passkeys are disabled on this server")
			}
			sess := SessionFrom(ctx)
			if sess == nil {
				return nil, huma.Error403Forbidden("a browser session is required")
			}
			if err := s.throttle(ctx, "step-up", sess.UserID.String()); err != nil {
				return nil, err
			}
			uid, err := s.Passkeys.FinishLogin(ctx, in.Body.SessionID, in.Body.Credential)
			if err != nil {
				return nil, s.passkeyErr(err)
			}
			if uid != sess.UserID {
				return nil, huma.Error401Unauthorized("passkey verification failed")
			}
			if err := s.Auth.MarkStepUp(ctx, sessionTokenFrom(ctx)); err != nil {
				return nil, mapError(s.Log, err)
			}
			return &struct{ Body stepUpBody }{stepUpBody{Until: time.Now().Add(auth.StepUpWindow)}}, nil
		})
}

const oidcStepUpCookie = "bb_oidc_stepup"

// registerOIDCStepUp adds the browser redirect that asks the identity provider to authenticate the
// signed-in user again. The callback lives in registerOIDC (same redirect URI as sign-in).
func (s *Server) registerOIDCStepUp(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/auth/oidc/step-up", func(w http.ResponseWriter, r *http.Request) {
		sess, _, ok := s.cookieSession(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		if s.Limiter != nil {
			if ok, _ := s.Limiter.Allow("oidc-stepup|" + sess.UserID.String()); !ok {
				writeProblem(w, http.StatusTooManyRequests, "too many attempts")
				return
			}
		}
		url, state, err := s.OIDC.BeginStepUp(r.Context())
		if err != nil {
			s.Log.Error("oidc step-up begin", "error", err)
			writeProblem(w, http.StatusBadGateway, "the identity provider is unavailable")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: oidcStepUpCookie, Value: state, Path: "/v1/auth/oidc", MaxAge: 600,
			HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, url, http.StatusFound)
	})
}

// finishOIDCStepUp handles the callback of a step-up flow. It reports whether the request was one.
func (s *Server) finishOIDCStepUp(w http.ResponseWriter, r *http.Request) bool {
	c, err := r.Cookie(oidcStepUpCookie)
	if err != nil {
		return false
	}
	http.SetCookie(w, &http.Cookie{Name: oidcStepUpCookie, Value: "", Path: "/v1/auth/oidc", MaxAge: -1, HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode})
	state := r.URL.Query().Get("state")
	if state == "" || c.Value != state {
		return false
	}
	sess, token, ok := s.cookieSession(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusFound)
		return true
	}
	if r.URL.Query().Get("error") != "" {
		http.Redirect(w, r, "/settings?stepup=denied", http.StatusFound)
		return true
	}
	uid, err := s.OIDC.CompleteStepUp(r.Context(), r.URL.Query().Get("code"), state)
	if err != nil || uid != sess.UserID {
		if err != nil && !errors.Is(err, oidc.ErrBadFlow) {
			s.Log.Warn("oidc step-up failed", "error", err)
		}
		http.Redirect(w, r, "/settings?stepup=failed", http.StatusFound)
		return true
	}
	if err := s.Auth.MarkStepUp(r.Context(), token); err != nil {
		http.Redirect(w, r, "/settings?stepup=failed", http.StatusFound)
		return true
	}
	http.Redirect(w, r, "/settings?stepup=ok", http.StatusFound)
	return true
}

// cookieSession resolves the browser session behind a plain HTTP request (the redirect routes are
// outside the huma pipeline).
func (s *Server) cookieSession(r *http.Request) (*auth.Session, string, bool) {
	c, err := r.Cookie(cookieName(s.secure))
	if err != nil {
		return nil, "", false
	}
	sess, err := s.Auth.Lookup(r.Context(), c.Value)
	if err != nil {
		return nil, "", false
	}
	return sess, c.Value, true
}
