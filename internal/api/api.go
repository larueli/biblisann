package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

// Pinger reports whether a dependency (the database) is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps are the collaborators of the HTTP surface.
type Deps struct {
	Version string
	DB      Pinger         // nil in spec-export mode
	Web     http.Handler   // the SPA; nil answers 404
	Extra   func(huma.API) // hook for later slices to register operations
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

	registerVersion(a, d.Version)
	if d.Extra != nil {
		d.Extra(a)
	}

	mux.HandleFunc("/v1/", func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusNotFound, "no such API operation")
	})
	if d.Web != nil {
		mux.Handle("/", d.Web)
	}
	return mux, a
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
