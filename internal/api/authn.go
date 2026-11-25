package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/authz"
)

type (
	sessionKey      struct{}
	sessionTokenKey struct{}
)

func sessionTokenFrom(ctx context.Context) string {
	t, _ := ctx.Value(sessionTokenKey{}).(string)
	return t
}

// SessionFrom returns the browser session behind the request, if any.
func SessionFrom(ctx context.Context) *auth.Session {
	s, _ := ctx.Value(sessionKey{}).(*auth.Session)
	return s
}

// cookieName: the __Host- prefix pins the cookie to the exact host over HTTPS; it requires
// Secure, so it is only used when the public URL is https.
func cookieName(secure bool) string {
	if secure {
		return "__Host-bb_session"
	}
	return "bb_session"
}

func newSessionCookie(secure bool, token string, exp time.Time) *http.Cookie {
	return &http.Cookie{Name: cookieName(secure), Value: token, Path: "/", Expires: exp, HttpOnly: true,
		Secure: secure, SameSite: http.SameSiteLaxMode}
}

func clearSessionCookie(secure bool) *http.Cookie {
	return &http.Cookie{Name: cookieName(secure), Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: secure, SameSite: http.SameSiteLaxMode}
}

// Authenticator turns cookies into principals and enforces CSRF protection.
type Authenticator struct {
	Auth    *auth.Service
	BaseURL *url.URL
	Secure  bool
	// Tokens resolves "Authorization: Bearer" credentials (personal access tokens, OAuth).
	Tokens TokenResolver
}

// TokenResolver validates a bearer token and returns its principal, or nil when it is not one
// of ours.
type TokenResolver interface {
	Resolve(ctx context.Context, token string) (*authz.Principal, error)
}

func problemHTTP(w http.ResponseWriter, status int, detail string) {
	writeProblem(w, status, detail)
}

// Middleware authenticates the request and rejects cross-site state-changing requests.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// These endpoints validate their own dedicated bearer tokens. Do not send them
		// through the application-token resolver or cookie CSRF checks.
		if r.URL.Path == "/metrics" || strings.HasPrefix(r.URL.Path, "/scim/v2/") {
			next.ServeHTTP(w, r)
			return
		}
		// These OAuth endpoints do not use browser cookies: public clients authenticate with
		// authorization codes or refresh tokens and may call them from another origin.
		if (r.URL.Path == "/oauth/register" || r.URL.Path == "/oauth/token") &&
			(r.Method == http.MethodPost || r.Method == http.MethodOptions) {
			next.ServeHTTP(w, r)
			return
		}
		unsafe := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
		bearer := ""
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			bearer = strings.TrimSpace(h[7:])
		}

		if bearer != "" && a.Tokens != nil {
			// Bearer tokens are not sent automatically by browsers: no CSRF check needed.
			p, err := a.Tokens.Resolve(r.Context(), bearer)
			if err != nil || p == nil {
				problemHTTP(w, http.StatusUnauthorized, "invalid token")
				return
			}
			// OAuth tokens are bound to the MCP resource: the REST API refuses them.
			if p.Audience == "mcp" && !strings.HasPrefix(r.URL.Path, "/mcp") {
				problemHTTP(w, http.StatusForbidden, "this token is only valid for the MCP endpoint")
				return
			}
			next.ServeHTTP(w, r.WithContext(authz.With(r.Context(), p)))
			return
		}

		if unsafe && !a.sameOrigin(r) {
			problemHTTP(w, http.StatusForbidden, "cross-origin request refused")
			return
		}
		c, err := r.Cookie(cookieName(a.Secure))
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		sess, err := a.Auth.Lookup(r.Context(), c.Value)
		if err != nil {
			next.ServeHTTP(w, r) // expired or unknown cookie: anonymous
			return
		}
		got := r.Header.Get("X-CSRF-Token")
		if unsafe || got != "" {
			if subtle.ConstantTimeCompare([]byte(got), []byte(sess.CSRFToken)) != 1 {
				problemHTTP(w, http.StatusForbidden, "missing or invalid CSRF token")
				return
			}
		}
		ctx := authz.With(r.Context(), &authz.Principal{UserID: sess.UserID, Email: sess.Email, IsAdmin: sess.IsAdmin, Via: "session"})
		ctx = context.WithValue(ctx, sessionKey{}, sess)
		ctx = context.WithValue(ctx, sessionTokenKey{}, c.Value)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// sameOrigin accepts a request whose Origin (or, failing that, Sec-Fetch-Site) shows it comes
// from our own origin. Requests without either header (curl, scripts) cannot be CSRF: browsers
// always send one of them on cross-site writes.
func (a *Authenticator) sameOrigin(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		return err == nil && u.Scheme == a.BaseURL.Scheme && u.Host == a.BaseURL.Host
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
		return true
	}
	return false
}

// MultiResolver tries several resolvers in turn; the first that recognizes the token wins.
type MultiResolver []TokenResolver

// Resolve implements TokenResolver.
func (m MultiResolver) Resolve(ctx context.Context, token string) (*authz.Principal, error) {
	for _, r := range m {
		if p, err := r.Resolve(ctx, token); err != nil || p != nil {
			return p, err
		}
	}
	return nil, nil
}
