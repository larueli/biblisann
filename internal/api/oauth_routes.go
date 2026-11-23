package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/oauth"
)

type oauthRequest struct {
	ClientID            string `json:"client_id" maxLength:"100"`
	RedirectURI         string `json:"redirect_uri" maxLength:"500"`
	CodeChallenge       string `json:"code_challenge" maxLength:"128"`
	CodeChallengeMethod string `json:"code_challenge_method" maxLength:"10"`
	State               string `json:"state,omitempty" maxLength:"500"`
	Scope               string `json:"scope,omitempty" maxLength:"100"`
	Resource            string `json:"resource,omitempty" maxLength:"500"`
}

func (r oauthRequest) toOAuth() oauth.Request {
	return oauth.Request{ClientID: r.ClientID, RedirectURI: r.RedirectURI, CodeChallenge: r.CodeChallenge,
		CodeChallengeMethod: r.CodeChallengeMethod, State: r.State, Scope: r.Scope, Resource: r.Resource}
}

func oauthProblem(err error) error {
	switch {
	case errors.Is(err, oauth.ErrInvalidClient):
		return huma.Error422UnprocessableEntity("unknown client or redirect address; do not continue")
	case errors.Is(err, oauth.ErrInvalidScope), errors.Is(err, oauth.ErrInvalidRequest):
		return huma.Error422UnprocessableEntity(err.Error())
	}
	return huma.Error500InternalServerError("internal error")
}

// registerOAuth adds the consent endpoints used by the web app's authorization screen.
func (s *Server) registerOAuth(a huma.API) {
	type infoBody struct {
		ClientName string   `json:"client_name"`
		Scopes     []string `json:"requested_scopes" nullable:"false"`
	}
	Register(a, huma.Operation{OperationID: "oauth-request-info", Method: http.MethodPost, Path: "/v1/oauth/request",
		Summary: "Validate an authorization request and describe the client (for the consent screen)",
		Tags:    []string{"oauth"}, Errors: []int{422}}, PermAuthenticated,
		func(ctx context.Context, in *struct{ Body oauthRequest }) (*struct{ Body infoBody }, error) {
			if SessionFrom(ctx) == nil || s.OAuth == nil {
				return nil, huma.Error403Forbidden("a browser session is required")
			}
			c, scopes, err := s.OAuth.Validate(ctx, in.Body.toOAuth())
			if err != nil {
				return nil, oauthProblem(err)
			}
			return &struct{ Body infoBody }{infoBody{c.Name, scopes}}, nil
		})
	Register(a, huma.Operation{OperationID: "oauth-consent", Method: http.MethodPost, Path: "/v1/oauth/consent",
		Summary: "Approve or deny an authorization request; returns where to send the browser",
		Tags:    []string{"oauth"}, Errors: []int{422}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			Body struct {
				Request    oauthRequest `json:"request"`
				Approve    bool         `json:"approve"`
				Scopes     []string     `json:"scopes,omitempty" doc:"What the user grants: read and/or write."`
				LibraryIDs []uuid.UUID  `json:"library_ids,omitempty" doc:"Restrict the grant to these libraries."`
			}
		}) (*struct {
			Body struct {
				RedirectTo string `json:"redirect_to"`
			}
		}, error) {
			if SessionFrom(ctx) == nil || s.OAuth == nil {
				return nil, huma.Error403Forbidden("a browser session is required")
			}
			p, _ := authz.From(ctx)
			req := in.Body.Request.toOAuth()
			out := &struct {
				Body struct {
					RedirectTo string `json:"redirect_to"`
				}
			}{}
			if _, _, err := s.OAuth.Validate(ctx, req); err != nil {
				return nil, oauthProblem(err)
			}
			if !in.Body.Approve {
				out.Body.RedirectTo = s.OAuth.Deny(req)
				return out, nil
			}
			// The user may only restrict, never widen: a granted library must be accessible to them.
			for _, l := range in.Body.LibraryIDs {
				if _, err := s.Library.GetLibrary(ctx, l); err != nil {
					return nil, mapError(s.Log, err)
				}
			}
			url, err := s.OAuth.Approve(ctx, p.UserID, req, in.Body.Scopes, in.Body.LibraryIDs)
			if err != nil {
				return nil, oauthProblem(err)
			}
			out.Body.RedirectTo = url
			return out, nil
		})
}
