package api

import (
	"net/http"

	"github.com/larueli/biblisann/internal/authz"
)

// guardImports rejects unauthorized requests and limits concurrent buffered bodies before Huma
// reads them. The semaphore is shared by both archive import endpoints.
func guardImports(next http.Handler) http.Handler {
	active := make(chan struct{}, 2)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || (r.URL.Path != "/v1/libraries/import" && r.URL.Path != "/v1/me/import") {
			next.ServeHTTP(w, r)
			return
		}
		p, err := authz.From(r.Context())
		if err != nil {
			writeProblem(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if !p.HasScope("write") {
			writeProblem(w, http.StatusForbidden, "forbidden")
			return
		}
		select {
		case active <- struct{}{}:
			defer func() { <-active }()
		default:
			writeProblem(w, http.StatusServiceUnavailable, "too many imports")
			return
		}
		next.ServeHTTP(w, r)
	})
}
