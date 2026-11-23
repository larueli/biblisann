package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/larueli/biblisann/internal/hardcover"
	"github.com/larueli/biblisann/internal/hcsync"
)

type hardcoverBody struct {
	Available  bool       `json:"available" doc:"false when the server has no BIBLISANN_SECRET_KEY to store tokens."`
	Linked     bool       `json:"linked"`
	Mode       string     `json:"mode,omitempty" enum:"pull,push,both"`
	LastSyncAt *time.Time `json:"last_sync_at,omitempty"`
	LastError  string     `json:"last_error,omitempty"`
}

func (s *Server) registerHardcoverSync(a huma.API) {
	status := func(ctx context.Context) (hardcoverBody, error) {
		sess := SessionFrom(ctx)
		if sess == nil {
			return hardcoverBody{}, huma.Error403Forbidden("a browser session is required")
		}
		out := hardcoverBody{Available: s.HCSync != nil && s.HCSync.Available()}
		if s.HCSync == nil {
			return out, nil
		}
		st, err := s.HCSync.Get(ctx, sess.UserID)
		if err != nil {
			return out, mapError(s.Log, err)
		}
		out.Linked, out.Mode, out.LastSyncAt, out.LastError = st.Linked, st.Mode, st.LastSyncAt, st.LastError
		return out, nil
	}
	Register(a, huma.Operation{OperationID: "get-hardcover-link", Method: http.MethodGet, Path: "/v1/me/hardcover",
		Summary: "My Hardcover link and its direction", Tags: []string{"reading"}}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct{ Body hardcoverBody }, error) {
			b, err := status(ctx)
			return &struct{ Body hardcoverBody }{b}, err
		})
	Register(a, huma.Operation{OperationID: "put-hardcover-link", Method: http.MethodPut, Path: "/v1/me/hardcover",
		Summary:     "Link my Hardcover account, or change the sync direction",
		Description: "mode: pull (Hardcover to Biblisann), push (Biblisann to Hardcover) or both. The token is only needed the first time; it is verified, then stored encrypted. Reading status and ratings are synchronized, matched by ISBN.",
		Tags:        []string{"reading"}, Errors: []int{401, 422, 503}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			Body struct {
				Token string `json:"token,omitempty" maxLength:"4096" doc:"Your Hardcover API token."`
				Mode  string `json:"mode" enum:"pull,push,both"`
			}
		}) (*struct{ Body hardcoverBody }, error) {
			sess := SessionFrom(ctx)
			if sess == nil || s.HCSync == nil {
				return nil, huma.Error403Forbidden("a browser session is required")
			}
			if !s.HCSync.Available() {
				return nil, huma.Error503ServiceUnavailable(hcsync.ErrDisabled.Error())
			}
			if err := s.throttle(ctx, "hardcover-link", sess.UserID.String()); err != nil {
				return nil, err
			}
			var err error
			if in.Body.Token != "" {
				err = s.HCSync.Link(ctx, sess.UserID, in.Body.Token, in.Body.Mode)
			} else {
				err = s.HCSync.SetMode(ctx, sess.UserID, in.Body.Mode)
			}
			switch {
			case errors.Is(err, hardcover.ErrUnauthorized):
				return nil, huma.Error401Unauthorized("Hardcover rejected this token")
			case errors.Is(err, hcsync.ErrMode):
				return nil, huma.Error422UnprocessableEntity(err.Error())
			case err != nil:
				s.Log.Warn("hardcover link", "error", err)
				return nil, huma.Error422UnprocessableEntity("link your account first, with a valid token")
			}
			b, err := status(ctx)
			return &struct{ Body hardcoverBody }{b}, err
		})
	Register(a, huma.Operation{OperationID: "delete-hardcover-link", Method: http.MethodDelete, Path: "/v1/me/hardcover",
		Summary: "Unlink Hardcover (forgets the token; local and Hardcover data stay as they are)", Tags: []string{"reading"}, DefaultStatus: http.StatusNoContent}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			sess := SessionFrom(ctx)
			if sess == nil || s.HCSync == nil {
				return nil, huma.Error403Forbidden("a browser session is required")
			}
			return nil, mapError(s.Log, s.HCSync.Unlink(ctx, sess.UserID))
		})
	Register(a, huma.Operation{OperationID: "sync-hardcover-now", Method: http.MethodPost, Path: "/v1/me/hardcover/sync",
		Summary: "Synchronize with Hardcover now (one budgeted pass; large libraries continue in the background)", Tags: []string{"reading"}, Errors: []int{401, 429, 502}}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct {
			Body struct {
				Pulled int  `json:"pulled"`
				Pushed int  `json:"pushed"`
				More   bool `json:"more" doc:"true: more remains and will continue in the background."`
			}
		}, error) {
			sess := SessionFrom(ctx)
			if sess == nil || s.HCSync == nil {
				return nil, huma.Error403Forbidden("a browser session is required")
			}
			if err := s.throttle(ctx, "hardcover-sync", sess.UserID.String()); err != nil {
				return nil, err
			}
			res, err := s.HCSync.SyncUser(ctx, sess.UserID)
			if errors.Is(err, hardcover.ErrUnauthorized) {
				return nil, huma.Error401Unauthorized("Hardcover rejected the stored token: link your account again")
			}
			if err != nil {
				s.Log.Warn("hardcover sync", "error", err)
				return nil, huma.NewError(http.StatusBadGateway, "the synchronization failed; it will be retried")
			}
			out := &struct {
				Body struct {
					Pulled int  `json:"pulled"`
					Pushed int  `json:"pushed"`
					More   bool `json:"more" doc:"true: more remains and will continue in the background."`
				}
			}{}
			out.Body.Pulled, out.Body.Pushed, out.Body.More = res.Pulled, res.Pushed, res.More
			return out, nil
		})
}
