package oauth

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// Limiter throttles the unauthenticated endpoints.
type Limiter interface {
	Allow(key string) (bool, time.Duration)
}

func cors(w http.ResponseWriter) {
	// These endpoints carry no cookies, so any origin may call them (browser-based MCP clients).
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, MCP-Protocol-Version")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	cors(w)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func oauthErr(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

// Routes registers the discovery, registration and token endpoints. clientKey identifies the
// caller for rate limiting.
func (s *Service) Routes(mux *http.ServeMux, lim Limiter, clientKey func(*http.Request) string) {
	preflight := func(w http.ResponseWriter, _ *http.Request) {
		cors(w)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
	}
	for _, p := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-authorization-server", "/oauth/register", "/oauth/token"} {
		mux.HandleFunc("OPTIONS "+p, preflight)
	}
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]any{"resource": s.resource, "authorization_servers": []string{s.base},
			"scopes_supported": []string{"read", "write"}, "bearer_methods_supported": []string{"header"},
			"resource_name": "Biblisann MCP"})
	})
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]any{"issuer": s.base, "authorization_endpoint": s.base + "/oauth/authorize",
			"token_endpoint": s.base + "/oauth/token", "registration_endpoint": s.base + "/oauth/register",
			"response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"},
			"code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"},
			"scopes_supported": []string{"read", "write"}, "authorization_response_iss_parameter_supported": true})
	})
	mux.HandleFunc("POST /oauth/register", func(w http.ResponseWriter, r *http.Request) {
		if ok, _ := lim.Allow("oauth-register|" + clientKey(r)); !ok {
			oauthErr(w, 429, "temporarily_unavailable", "too many requests")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		var in struct {
			ClientName   string   `json:"client_name"`
			RedirectURIs []string `json:"redirect_uris"`
			AuthMethod   string   `json:"token_endpoint_auth_method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || (in.AuthMethod != "" && in.AuthMethod != "none") {
			oauthErr(w, 400, "invalid_client_metadata", "send client_name and redirect_uris; only public clients (token_endpoint_auth_method none) are supported")
			return
		}
		id, err := s.RegisterClient(r.Context(), in.ClientName, in.RedirectURIs)
		if errors.Is(err, ErrInvalidRequest) {
			oauthErr(w, 400, "invalid_redirect_uri", "invalid client metadata")
			return
		}
		if err != nil {
			oauthErr(w, 503, "temporarily_unavailable", "registration unavailable")
			return
		}
		writeJSON(w, 201, map[string]any{"client_id": id, "client_name": in.ClientName, "redirect_uris": in.RedirectURIs,
			"token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}})
	})
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if ok, _ := lim.Allow("oauth-token|" + clientKey(r)); !ok {
			oauthErr(w, 429, "temporarily_unavailable", "too many requests")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		if err := r.ParseForm(); err != nil {
			oauthErr(w, 400, "invalid_request", "invalid form")
			return
		}
		var (
			tr  TokenResponse
			err error
		)
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			tr, err = s.ExchangeCode(r.Context(), r.PostForm.Get("client_id"), r.PostForm.Get("code"), r.PostForm.Get("redirect_uri"),
				r.PostForm.Get("code_verifier"), r.PostForm.Get("resource"))
		case "refresh_token":
			tr, err = s.Refresh(r.Context(), r.PostForm.Get("client_id"), r.PostForm.Get("refresh_token"))
		default:
			oauthErr(w, 400, "unsupported_grant_type", "use authorization_code or refresh_token")
			return
		}
		switch {
		case errors.Is(err, ErrInvalidGrant):
			oauthErr(w, 400, "invalid_grant", "the code or token is invalid, expired or already used")
		case err != nil:
			oauthErr(w, 500, "server_error", "internal error")
		default:
			writeJSON(w, 200, tr)
		}
	})
}
