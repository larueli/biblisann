package api

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/httpmw"
	"github.com/larueli/biblisann/internal/ratelimit"
)

// quotaWindow is the period the per-user request quota is expressed over (the "w" of the policy).
const quotaWindow = time.Minute

// requestCost weighs a request: lookups that call external providers and imports cost more than a
// page view, so a client cannot turn a generous quota into a flood of outbound traffic.
func requestCost(r *http.Request) int {
	p := r.URL.Path
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/v1/isbn/"):
		return 5
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/scan"):
		return 5
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/refresh"):
		return 10
	case r.Method == http.MethodPost && (strings.HasSuffix(p, "/import") || strings.Contains(p, "/import/")):
		return 20
	}
	return 1
}

// quotaHeaders writes the IETF RateLimit header fields (draft-ietf-httpapi-ratelimit-headers):
// RateLimit-Policy names the quota, RateLimit says what is left and when it refills.
func quotaHeaders(h http.Header, d ratelimit.Decision) {
	h.Set("RateLimit-Policy", fmt.Sprintf(`"default";q=%d;w=%d`, d.Limit, int(quotaWindow.Seconds())))
	h.Set("RateLimit", fmt.Sprintf(`"default";r=%d;t=%d`, d.Remaining, ceilSeconds(d.Reset)))
}

func ceilSeconds(d time.Duration) int { return int(math.Ceil(d.Seconds())) }

// QuotaKey identifies who the request quota belongs to (also used by the MCP tools that weigh more).
func QuotaKey(p *authz.Principal) string { return "quota|" + p.UserID.String() }

// quotaMiddleware enforces the per-user request quota on authenticated API calls, MCP calls and
// OPDS feeds. It must sit
// inside the authenticator, which puts the principal in the context. Cover images, probes and the
// static app are not metered.
func quotaMiddleware(m ratelimit.Meter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/opds/") {
			// Feed readers authenticate with the token in the path, inside the handler: meter by client.
			d := m.Take("quota|opds|"+ipKey(httpmw.ClientIP(r.Context())), 1)
			quotaHeaders(w.Header(), d)
			if !d.Allowed {
				w.Header().Set("Retry-After", strconv.Itoa(max(1, ceilSeconds(d.RetryAfter))))
				writeProblem(w, http.StatusTooManyRequests, "request quota exceeded, slow down")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if (!strings.HasPrefix(path, "/v1/") && path != "/mcp") || strings.HasPrefix(path, "/v1/covers/") {
			next.ServeHTTP(w, r)
			return
		}
		p, err := authz.From(r.Context())
		if err != nil {
			next.ServeHTTP(w, r) // anonymous routes have their own throttles
			return
		}
		d := m.Take(QuotaKey(p), requestCost(r))
		quotaHeaders(w.Header(), d)
		if !d.Allowed {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, ceilSeconds(d.RetryAfter))))
			writeProblem(w, http.StatusTooManyRequests, "request quota exceeded, slow down")
			return
		}
		next.ServeHTTP(w, r)
	})
}
