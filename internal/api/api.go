package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/google/uuid"

	"github.com/larueli/biblisann/internal/account"
	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/catalog"
	"github.com/larueli/biblisann/internal/importer"
	"github.com/larueli/biblisann/internal/library"
	"github.com/larueli/biblisann/internal/lists"
	"github.com/larueli/biblisann/internal/loans"
	"github.com/larueli/biblisann/internal/notify"
	"github.com/larueli/biblisann/internal/oauth"
	"github.com/larueli/biblisann/internal/oidc"
	"github.com/larueli/biblisann/internal/opds"
	"github.com/larueli/biblisann/internal/passkeys"
	"github.com/larueli/biblisann/internal/ratelimit"
	"github.com/larueli/biblisann/internal/reading"
	"github.com/larueli/biblisann/internal/scan"
	"github.com/larueli/biblisann/internal/scim"
	"github.com/larueli/biblisann/internal/tokens"
)

// Pinger reports whether a dependency (the database) is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps are the collaborators of the HTTP surface. Services may be nil in spec-export mode.
type Deps struct {
	Version string
	DB      Pinger       // nil in spec-export mode
	Web     http.Handler // the SPA; nil answers 404

	Log       *slog.Logger
	BaseURL   *url.URL
	Auth      *auth.Service
	Library   *library.Service
	Catalog   *catalog.Service
	Scan      *scan.Service
	Reading   *reading.Service
	Lists     *lists.Service
	Loans     *loans.Service
	TokenSvc  *tokens.Service
	Notify    *notify.Dispatcher
	Channels  *notify.Channels
	Limiter   ratelimit.Limiter // login and registration throttling
	Tokens    TokenResolver
	Importer  *importer.Importer
	OIDC      *oidc.Service  // nil: single sign-on is off
	OAuth     *oauth.Service // nil: the MCP authorization server is off
	Hardcover interface {
		Refresh(ctx context.Context, workID uuid.UUID) error
	} // nil: no Hardcover token configured
	Account  *account.Service                             // nil: exports, imports and deletion are off
	Passkeys *passkeys.Service                            // nil: passkeys are off
	OPDS     *opds.Handler                                // nil: no OPDS catalog
	SCIM     *scim.Handler                                // nil: SCIM provisioning is off
	Backup   func(ctx context.Context, w io.Writer) error // nil: backups are not served over HTTP
	MCP      http.Handler                                 // the MCP endpoint, mounted at /mcp (nil: disabled)
	Extra    func(huma.API, *Server)                      // hook for later slices to register operations
}

// Server holds what route handlers share.
type Server struct {
	Deps
	secure bool
}

// New returns the root handler (without middleware) and the Huma API for introspection.
func New(d Deps) (http.Handler, huma.API) {
	mux := http.NewServeMux()
	a := humago.New(mux, config(d.Version))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if d.DB != nil {
			if err := d.DB.Ping(r.Context()); err != nil {
				writeProblem(w, http.StatusServiceUnavailable, "database unavailable")
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.BaseURL == nil {
		d.BaseURL = &url.URL{Scheme: "http", Host: "localhost:8080"}
	}
	srv := &Server{Deps: d, secure: d.BaseURL.Scheme == "https"}
	registerVersion(a, d.Version)
	srv.registerAuth(a)
	srv.registerLibraries(a)
	srv.registerCatalog(a)
	srv.registerSync(a)
	srv.registerReading(a)
	srv.registerLists(a)
	srv.registerLoans(a)
	srv.registerSharing(a)
	srv.registerTokens(a)
	srv.registerExtras(a)
	srv.registerAccount(a)
	mux.HandleFunc("GET /v1/libraries/{id}/export", srv.serveExport)
	mux.HandleFunc("GET /v1/me/export", srv.serveExport)
	srv.registerOIDC(a, mux)
	srv.registerPasskeys(a)
	srv.registerOAuth(a) // operations are always described; handlers answer 404 when the feature is off
	if d.OAuth != nil {
		limiter := d.Limiter
		if limiter == nil {
			limiter = ratelimit.NewMemory(1000, time.Second)
		}
		d.OAuth.Routes(mux, limiter, clientKey)
	}
	mux.HandleFunc("/v1/admin/backup", srv.serveBackup)
	mux.HandleFunc("GET /v1/feeds/loans.ics", srv.serveLoansFeed)
	if d.Extra != nil {
		d.Extra(a, srv)
	}
	mux.HandleFunc("GET /v1/covers/{id}", srv.serveCover)
	if d.OPDS != nil {
		d.OPDS.Routes(mux)
	}
	if d.SCIM != nil {
		d.SCIM.Routes(mux)
	}
	if d.MCP != nil {
		mux.Handle("/mcp", d.MCP)
	}

	mux.HandleFunc("/v1/", func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusNotFound, "no such API operation")
	})
	if d.Web != nil {
		mux.Handle("/", d.Web)
	}
	var h http.Handler = mux
	if d.Auth != nil {
		h = (&Authenticator{Auth: d.Auth, BaseURL: d.BaseURL, Secure: srv.secure, Tokens: d.Tokens}).Middleware(h)
	}
	return noStoreAPI(h), a
}

// noStoreAPI keeps API responses out of shared caches: they are per-user.
func noStoreAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") && !strings.HasPrefix(r.URL.Path, "/v1/covers/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func config(version string) huma.Config {
	c := huma.DefaultConfig("Biblisann API", version)
	c.Info.Description = "REST API of Biblisann, a self-hosted physical book library manager."
	c.Info.License = &huma.License{Name: "AGPL-3.0-or-later", Identifier: "AGPL-3.0-or-later"}
	c.OpenAPIPath = "/openapi" // serves /openapi.json and /openapi.yaml, unauthenticated
	c.DocsPath = ""            // no bundled docs UI (third-party scripts) for now
	c.SchemasPath = ""
	c.CreateHooks = nil // no "$schema" link headers
	return c
}

// OpenAPIJSON renders the OpenAPI document without starting a server (used by make generate).
func OpenAPIJSON(version string) ([]byte, error) {
	_, a := New(Deps{Version: version})
	return json.MarshalIndent(a.OpenAPI(), "", "  ")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeProblem writes an RFC 9457 problem details document.
func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": detail,
	})
}
