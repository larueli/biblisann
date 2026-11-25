package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/larueli/biblisann/internal/oidc"
)

const oidcCookie = "bb_oidc"
const oidcLinkCookie = "bb_oidc_link"

// registerOIDC adds the single sign-on endpoints when a provider is configured.
func (s *Server) registerOIDC(a huma.API, mux *http.ServeMux) {
	Register(a, huma.Operation{OperationID: "oidc-info", Method: http.MethodGet, Path: "/v1/auth/oidc",
		Summary: "Whether single sign-on is configured, and the button label", Tags: []string{"auth"}}, PermPublic,
		func(context.Context, *struct{}) (*struct {
			Body struct {
				Enabled bool   `json:"enabled"`
				Label   string `json:"label,omitempty"`
			}
		}, error) {
			out := &struct {
				Body struct {
					Enabled bool   `json:"enabled"`
					Label   string `json:"label,omitempty"`
				}
			}{}
			if s.OIDC != nil {
				out.Body.Enabled, out.Body.Label = true, s.OIDC.Label()
			}
			return out, nil
		})
	Register(a, huma.Operation{OperationID: "oidc-link", Method: http.MethodPost, Path: "/v1/auth/oidc/link",
		Summary: "Link the signed-in account to a verified single sign-on identity", Tags: []string{"auth"}}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct {
			SetCookie http.Cookie `header:"Set-Cookie"`
			Body      struct {
				URL string `json:"url"`
			}
		}, error) {
			if s.OIDC == nil {
				return nil, huma.Error404NotFound("not found")
			}
			sess := SessionFrom(ctx)
			if sess == nil {
				return nil, huma.Error403Forbidden("a browser session is required")
			}
			if err := s.requireStepUp(ctx); err != nil {
				return nil, err
			}
			if err := s.throttle(ctx, "oidc-link", sess.UserID.String()); err != nil {
				return nil, err
			}
			url, state, err := s.OIDC.BeginLink(ctx, sess.UserID, sessionTokenFrom(ctx))
			if err != nil {
				return nil, huma.Error502BadGateway("the identity provider is unavailable")
			}
			out := &struct {
				SetCookie http.Cookie `header:"Set-Cookie"`
				Body      struct {
					URL string `json:"url"`
				}
			}{}
			out.SetCookie = http.Cookie{Name: oidcLinkCookie, Value: state, Path: "/v1/auth/oidc", MaxAge: 600, HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode}
			out.Body.URL = url
			return out, nil
		})
	if s.OIDC == nil {
		return
	}
	mux.HandleFunc("GET /v1/auth/oidc/login", func(w http.ResponseWriter, r *http.Request) {
		if s.Limiter != nil {
			if ok, _ := s.Limiter.Allow("oidc-login|" + clientKey(r)); !ok {
				writeProblem(w, http.StatusTooManyRequests, "too many attempts")
				return
			}
		}
		url, state, err := s.OIDC.Begin(r.Context())
		if err != nil {
			s.Log.Error("oidc begin", "error", err)
			writeProblem(w, http.StatusBadGateway, "the identity provider is unavailable")
			return
		}
		// The state is also bound to this browser, so a forged callback link cannot log a victim
		// in as someone else.
		http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: state, Path: "/v1/auth/oidc", MaxAge: 600,
			HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, url, http.StatusFound)
	})
	s.registerOIDCStepUp(mux)
	mux.HandleFunc("GET /v1/auth/oidc/callback", func(w http.ResponseWriter, r *http.Request) {
		if s.finishOIDCLink(w, r) {
			return
		}
		if s.finishOIDCStepUp(w, r) {
			return
		}
		c, err := r.Cookie(oidcCookie)
		state := r.URL.Query().Get("state")
		if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
			writeProblem(w, http.StatusBadRequest, "invalid sign-in request")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: "", Path: "/v1/auth/oidc", MaxAge: -1, HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode})
		if e := r.URL.Query().Get("error"); e != "" {
			http.Redirect(w, r, "/login?sso=denied", http.StatusFound)
			return
		}
		uid, err := s.OIDC.Complete(r.Context(), r.URL.Query().Get("code"), state)
		switch {
		case errors.Is(err, oidc.ErrUnverifiedEmail), errors.Is(err, oidc.ErrNotAllowed), errors.Is(err, oidc.ErrDisabled):
			http.Redirect(w, r, "/login?sso=refused", http.StatusFound)
			return
		case err != nil:
			s.Log.Warn("oidc callback failed", "error", err)
			http.Redirect(w, r, "/login?sso=error", http.StatusFound)
			return
		}
		sess, err := s.Auth.OpenSessionFor(r.Context(), uid, r.UserAgent())
		if err != nil {
			http.Redirect(w, r, "/login?sso=error", http.StatusFound)
			return
		}
		http.SetCookie(w, newSessionCookie(s.secure, sess.Token, sess.ExpiresAt))
		http.Redirect(w, r, "/", http.StatusFound)
	})
}

func (s *Server) finishOIDCLink(w http.ResponseWriter, r *http.Request) bool {
	c, err := r.Cookie(oidcLinkCookie)
	if err != nil {
		return false
	}
	state := r.URL.Query().Get("state")
	if state == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
		return false
	}
	http.SetCookie(w, &http.Cookie{Name: oidcLinkCookie, Value: "", Path: "/v1/auth/oidc", MaxAge: -1, HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode})
	sess, token, ok := s.cookieSession(r)
	if !ok || r.URL.Query().Get("error") != "" {
		http.Redirect(w, r, "/settings?oidc_link=failed", http.StatusFound)
		return true
	}
	if err := s.OIDC.CompleteLink(r.Context(), r.URL.Query().Get("code"), state, sess.UserID, token); err != nil {
		http.Redirect(w, r, "/settings?oidc_link=failed", http.StatusFound)
		return true
	}
	http.Redirect(w, r, "/settings?oidc_link=ok", http.StatusFound)
	return true
}
