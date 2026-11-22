package api

import (
	"context"
	"net/http"

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
