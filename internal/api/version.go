package api

import (
	"context"
	"net/http"
	"net/url"
	"regexp"

	"github.com/danielgtaylor/huma/v2"
)

type versionOutput struct {
	Body struct {
		Version string `json:"version" example:"0.1.0" doc:"Release version of the running server."`
	}
}

func registerVersion(a huma.API, version string) {
	Register(a, huma.Operation{
		OperationID: "get-version",
		Method:      http.MethodGet,
		Path:        "/v1/version",
		Summary:     "Get the server version",
		Tags:        []string{"system"},
	}, PermPublic, func(context.Context, *struct{}) (*versionOutput, error) {
		out := &versionOutput{}
		out.Body.Version = version
		return out, nil
	})
}

type aboutOutput struct {
	Body struct {
		Version    string `json:"version" example:"0.4.0"`
		License    string `json:"license" example:"AGPL-3.0-or-later"`
		SourceURL  string `json:"source_url" doc:"Where the exact source of this running version can be obtained (AGPL-3.0 section 13)."`
		DocsURL    string `json:"docs_url"`
		APISpecURL string `json:"openapi_url"`
	}
}

var releaseRe = regexp.MustCompile(`^\d+\.\d+\.\d+`)

// sourceFor links to the tag of a release build when the source host is GitHub; otherwise to the
// configured URL as is.
func sourceFor(base, version string) string {
	if u, err := url.Parse(base); err == nil && u.Host == "github.com" && releaseRe.MatchString(version) {
		return base + "/tree/v" + version
	}
	return base
}

func registerAbout(a huma.API, version, sourceURL string) {
	Register(a, huma.Operation{
		OperationID: "get-about",
		Method:      http.MethodGet,
		Path:        "/v1/about",
		Summary:     "Version, license and where to get the source",
		Tags:        []string{"system"},
	}, PermPublic, func(context.Context, *struct{}) (*aboutOutput, error) {
		out := &aboutOutput{}
		out.Body.Version, out.Body.License = version, "AGPL-3.0-or-later"
		out.Body.SourceURL = sourceFor(sourceURL, version)
		out.Body.DocsURL, out.Body.APISpecURL = "/docs/", "/openapi.json"
		return out, nil
	})
}
