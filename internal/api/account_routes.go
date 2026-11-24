package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/larueli/biblisann/internal/account"
	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/authz"
)

const maxImportBytes = 64 << 20

type importReportBody struct {
	Libraries  int         `json:"libraries"`
	Editions   int         `json:"editions_created"`
	Instances  int         `json:"instances"`
	Places     int         `json:"places"`
	Lists      int         `json:"lists"`
	ListItems  int         `json:"list_items"`
	Loans      int         `json:"loans"`
	Contacts   int         `json:"contacts"`
	Reading    int         `json:"reading_states"`
	Grades     int         `json:"grades"`
	LibraryIDs []uuid.UUID `json:"library_ids" nullable:"false"`
}

func toImportReport(r account.ImportReport) importReportBody {
	ids := r.LibraryIDs
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return importReportBody{r.Libraries, r.Editions, r.Instances, r.Places, r.Lists, r.ListItems, r.Loans, r.Contacts, r.Reading, r.Grades, ids}
}

func requireAdmin(ctx context.Context) (*authz.Principal, error) {
	p, err := authz.From(ctx)
	if err != nil {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	if !p.IsAdmin || p.Via != "session" {
		return nil, huma.Error404NotFound("not found") // never confirm admin endpoints to others
	}
	return p, nil
}

func accountErr(s *Server, err error) error {
	var sole *account.ErrSoleAdmin
	switch {
	case errors.As(err, &sole):
		return huma.Error409Conflict(sole.Error())
	case errors.Is(err, account.ErrLastInstanceAdmin):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, account.ErrBusy):
		return huma.NewError(http.StatusTooManyRequests, "too many imports in progress, try again in a minute")
	case errors.Is(err, account.ErrArchive):
		return huma.Error422UnprocessableEntity("the archive is not a valid, supported Biblisann export")
	}
	return mapError(s.Log, err)
}

func (s *Server) registerAccount(a huma.API) {
	type sessionInfoBody struct {
		ID        uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		LastSeen  time.Time `json:"last_seen"`
		ExpiresAt time.Time `json:"expires_at"`
		UserAgent string    `json:"user_agent,omitempty"`
		Current   bool      `json:"current"`
	}
	Register(a, huma.Operation{OperationID: "list-sessions", Method: http.MethodGet, Path: "/v1/me/sessions",
		Summary: "My signed-in browsers and devices", Tags: []string{"account"}}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct{ Body []sessionInfoBody }, error) {
			sess := SessionFrom(ctx)
			if sess == nil {
				return nil, huma.Error403Forbidden("a browser session is required")
			}
			rows, err := s.Auth.Sessions(ctx, sess.UserID, sessionTokenFrom(ctx))
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := make([]sessionInfoBody, len(rows))
			for i, r := range rows {
				out[i] = sessionInfoBody{r.ID, r.CreatedAt, r.LastSeen, r.ExpiresAt, r.UserAgent, r.Current}
			}
			return &struct{ Body []sessionInfoBody }{out}, nil
		})
	Register(a, huma.Operation{OperationID: "revoke-session", Method: http.MethodDelete, Path: "/v1/me/sessions/{id}",
		Summary: "Sign one session out", Tags: []string{"account"}, DefaultStatus: http.StatusNoContent, Errors: []int{404}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			ID uuid.UUID `path:"id"`
		}) (*struct{}, error) {
			sess := SessionFrom(ctx)
			if sess == nil {
				return nil, huma.Error403Forbidden("a browser session is required")
			}
			if err := s.Auth.RevokeSession(ctx, sess.UserID, in.ID); err != nil {
				if errors.Is(err, auth.ErrSessionNotFound) {
					return nil, huma.Error404NotFound("not found")
				}
				return nil, mapError(s.Log, err)
			}
			return nil, nil
		})
	Register(a, huma.Operation{OperationID: "revoke-other-sessions", Method: http.MethodPost, Path: "/v1/me/sessions/revoke-others",
		Summary: "Sign out everywhere except here", Tags: []string{"account"}}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct {
			Body struct {
				Revoked int64 `json:"revoked"`
			}
		}, error) {
			sess := SessionFrom(ctx)
			if sess == nil {
				return nil, huma.Error403Forbidden("a browser session is required")
			}
			n, err := s.Auth.RevokeOtherSessions(ctx, sess.UserID, sessionTokenFrom(ctx))
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := &struct {
				Body struct {
					Revoked int64 `json:"revoked"`
				}
			}{}
			out.Body.Revoked = n
			return out, nil
		})

	type appBody struct {
		ClientID string    `json:"client_id"`
		Name     string    `json:"name"`
		Scopes   []string  `json:"scopes" nullable:"false"`
		Since    time.Time `json:"since"`
	}
	Register(a, huma.Operation{OperationID: "list-authorized-apps", Method: http.MethodGet, Path: "/v1/me/authorized-apps",
		Summary: "Applications (MCP clients) I authorized through OAuth", Tags: []string{"account"}}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct{ Body []appBody }, error) {
			out := []appBody{}
			if sess := SessionFrom(ctx); sess != nil && s.OAuth != nil {
				rows, err := s.OAuth.Grants(ctx, sess.UserID)
				if err != nil {
					return nil, mapError(s.Log, err)
				}
				for _, g := range rows {
					out = append(out, appBody{g.ClientID, g.Name, g.Scopes, g.Since})
				}
			}
			return &struct{ Body []appBody }{out}, nil
		})
	Register(a, huma.Operation{OperationID: "revoke-authorized-app", Method: http.MethodDelete, Path: "/v1/me/authorized-apps/{client_id}",
		Summary: "Revoke an application's access (refresh and access tokens)", Tags: []string{"account"}, DefaultStatus: http.StatusNoContent, Errors: []int{404}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			ClientID string `path:"client_id" maxLength:"100"`
		}) (*struct{}, error) {
			sess := SessionFrom(ctx)
			if sess == nil || s.OAuth == nil {
				return nil, huma.Error404NotFound("not found")
			}
			if err := s.OAuth.RevokeGrant(ctx, sess.UserID, in.ClientID); err != nil {
				return nil, huma.Error404NotFound("not found")
			}
			return nil, nil
		})

	// ---- import (documented here; exports are plain downloads, see serveExport)
	Register(a, huma.Operation{OperationID: "import-library", Method: http.MethodPost, Path: "/v1/libraries/import",
		Summary: "Create a new library from a Biblisann library export (tar)", Description: "Send the archive as the request body (application/x-tar, at most 64 MiB). The caller becomes the administrator of the new library.",
		Tags: []string{"account"}, Errors: []int{413, 422}, MaxBodyBytes: maxImportBytes}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			RawBody []byte `contentType:"application/x-tar"`
		}) (*struct{ Body importReportBody }, error) {
			rep, err := s.Account.ImportLibrary(ctx, bytes.NewReader(in.RawBody))
			if err != nil {
				return nil, accountErr(s, err)
			}
			return &struct{ Body importReportBody }{toImportReport(rep)}, nil
		})
	Register(a, huma.Operation{OperationID: "import-account", Method: http.MethodPost, Path: "/v1/me/import",
		Summary:     "Restore personal data (lists, reading, grades, contacts, libraries) from an account export (tar)",
		Description: "Libraries in the archive become new libraries. The profile in the archive is never applied.",
		Tags:        []string{"account"}, Errors: []int{413, 422}, MaxBodyBytes: maxImportBytes}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			RawBody []byte `contentType:"application/x-tar"`
		}) (*struct{ Body importReportBody }, error) {
			rep, err := s.Account.ImportAccount(ctx, bytes.NewReader(in.RawBody))
			if err != nil {
				return nil, accountErr(s, err)
			}
			return &struct{ Body importReportBody }{toImportReport(rep)}, nil
		})

	// ---- deletion
	Register(a, huma.Operation{OperationID: "delete-me", Method: http.MethodDelete, Path: "/v1/me",
		Summary:     "Delete my account and the data that belongs only to it",
		Description: "Libraries only you use are deleted. A shared library you alone administer blocks the deletion unless promote_successors is true (the most privileged other member becomes administrator). Export your data first: this cannot be undone.",
		Tags:        []string{"account"}, DefaultStatus: http.StatusNoContent, Errors: []int{401, 409}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			Body struct {
				Email             string `json:"confirm_email" maxLength:"320" doc:"Your email address, typed again."`
				Password          string `json:"password,omitempty" maxLength:"256" doc:"Required when the account has a password."`
				PromoteSuccessors bool   `json:"promote_successors,omitempty"`
			}
		}) (*struct {
			SetCookie http.Cookie `header:"Set-Cookie"`
		}, error) {
			sess := SessionFrom(ctx)
			if sess == nil {
				return nil, huma.Error403Forbidden("a browser session is required")
			}
			if err := s.requireStepUp(ctx); err != nil {
				return nil, err
			}
			if err := s.throttle(ctx, "delete-account", sess.UserID.String()); err != nil {
				return nil, err
			}
			if err := s.Auth.ConfirmIdentity(ctx, sess.UserID, in.Body.Email, in.Body.Password); err != nil {
				return nil, mapError(s.Log, err)
			}
			if err := s.Account.Delete(ctx, sess.UserID, in.Body.PromoteSuccessors); err != nil {
				return nil, accountErr(s, err)
			}
			return &struct {
				SetCookie http.Cookie `header:"Set-Cookie"`
			}{SetCookie: *clearSessionCookie(s.secure)}, nil
		})

	// ---- administration
	type userAdminBody struct {
		ID        uuid.UUID `json:"id"`
		Email     string    `json:"email"`
		Name      string    `json:"name"`
		IsAdmin   bool      `json:"is_admin"`
		Disabled  bool      `json:"disabled"`
		CreatedAt time.Time `json:"created_at"`
	}
	Register(a, huma.Operation{OperationID: "admin-list-users", Method: http.MethodGet, Path: "/v1/admin/users",
		Summary: "List accounts (instance administrators)", Tags: []string{"admin"}}, PermInstanceAdmin,
		func(ctx context.Context, in *struct {
			Limit  int `query:"limit" minimum:"1" maximum:"200" default:"50"`
			Offset int `query:"offset" minimum:"0"`
		}) (*struct{ Body []userAdminBody }, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			rows, err := s.Auth.ListUsers(ctx, in.Limit, in.Offset)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := make([]userAdminBody, len(rows))
			for i, u := range rows {
				out[i] = userAdminBody{u.ID, u.Email, u.DisplayName, u.IsAdmin, u.DisabledAt != nil, u.CreatedAt}
			}
			return &struct{ Body []userAdminBody }{out}, nil
		})
	Register(a, huma.Operation{OperationID: "admin-set-password", Method: http.MethodPut, Path: "/v1/admin/users/{id}/password",
		Summary:     "Set a password on an account (also single sign-on accounts) and sign it out everywhere",
		Description: "Users who sign in with single sign-on cannot set a password themselves; an administrator can, which also lets them sign in locally. Needs a recent re-authentication.",
		Tags:        []string{"admin"}, DefaultStatus: http.StatusNoContent, Errors: []int{403, 404, 422}}, PermInstanceAdmin,
		func(ctx context.Context, in *struct {
			ID   uuid.UUID `path:"id"`
			Body struct {
				Password string `json:"password" maxLength:"256"`
			}
		}) (*struct{}, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			if err := s.requireStepUp(ctx); err != nil {
				return nil, err
			}
			if err := s.throttle(ctx, "admin-password", in.ID.String()); err != nil {
				return nil, err
			}
			return nil, mapError(s.Log, s.Auth.AdminSetPassword(ctx, in.ID, in.Body.Password))
		})
	Register(a, huma.Operation{OperationID: "admin-set-disabled", Method: http.MethodPut, Path: "/v1/admin/users/{id}/disabled",
		Summary: "Disable or re-enable an account (disabling signs it out everywhere)", Tags: []string{"admin"}, DefaultStatus: http.StatusNoContent}, PermInstanceAdmin,
		func(ctx context.Context, in *struct {
			ID   uuid.UUID `path:"id"`
			Body struct {
				Disabled bool `json:"disabled"`
			}
		}) (*struct{}, error) {
			p, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			if p.UserID == in.ID && in.Body.Disabled {
				return nil, huma.Error409Conflict("you cannot disable your own account")
			}
			return nil, mapError(s.Log, s.Auth.SetDisabled(ctx, in.ID, in.Body.Disabled))
		})
	Register(a, huma.Operation{OperationID: "admin-revoke-sessions", Method: http.MethodPost, Path: "/v1/admin/users/{id}/revoke-sessions",
		Summary: "Sign a user out everywhere and revoke their OAuth grants and personal access tokens", Tags: []string{"admin"}, DefaultStatus: http.StatusNoContent}, PermInstanceAdmin,
		func(ctx context.Context, in *struct {
			ID uuid.UUID `path:"id"`
		}) (*struct{}, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			if err := s.Auth.RevokeAllSessions(ctx, in.ID); err != nil {
				return nil, mapError(s.Log, err)
			}
			if s.OAuth != nil {
				if err := s.OAuth.RevokeAll(ctx, in.ID); err != nil {
					return nil, mapError(s.Log, err)
				}
			}
			return nil, mapError(s.Log, s.TokenSvc.RevokeAllPATs(ctx, in.ID))
		})
	Register(a, huma.Operation{OperationID: "admin-delete-user", Method: http.MethodDelete, Path: "/v1/admin/users/{id}",
		Summary: "Delete an account (instance administrators)", Tags: []string{"admin"}, DefaultStatus: http.StatusNoContent, Errors: []int{404, 409}}, PermInstanceAdmin,
		func(ctx context.Context, in *struct {
			ID                uuid.UUID `path:"id"`
			PromoteSuccessors bool      `query:"promote_successors"`
		}) (*struct{}, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			return nil, accountErr(s, s.Account.Delete(ctx, in.ID, in.PromoteSuccessors))
		})
}

// serveExport streams an export archive: GET /v1/libraries/{id}/export and GET /v1/me/export.
// Downloads are plain handlers (not Huma operations) because they stream; they are documented
// in docs/user/organise.md.
func (s *Server) serveExport(w http.ResponseWriter, r *http.Request) {
	if s.Account == nil {
		writeProblem(w, http.StatusNotFound, "not found")
		return
	}
	p, err := authz.From(r.Context())
	if err != nil {
		writeProblem(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if !p.HasScope("read") && !p.HasScope("write") {
		writeProblem(w, http.StatusForbidden, "forbidden")
		return
	}
	name := "biblisann-account-" + time.Now().UTC().Format("20060102")
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Cache-Control", "no-store")
	var run func() error
	if idStr := r.PathValue("id"); idStr != "" {
		id, perr := uuid.Parse(idStr)
		if perr != nil {
			writeProblem(w, http.StatusNotFound, "not found")
			return
		}
		name = "biblisann-library-" + time.Now().UTC().Format("20060102")
		// The archive is built before the first byte is sent when access is refused.
		if _, gerr := s.Library.GetLibrary(r.Context(), id); gerr != nil {
			status := http.StatusNotFound
			if errors.Is(gerr, authz.ErrForbidden) {
				status = http.StatusForbidden
			}
			w.Header().Del("Content-Type")
			writeProblem(w, status, "not found")
			return
		}
		run = func() error { return s.Account.ExportLibrary(r.Context(), id, w) }
	} else {
		run = func() error { return s.Account.ExportAccount(r.Context(), w) }
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.tar"`)
	if err := run(); err != nil {
		s.Log.Error("export failed", "error", err) // headers are sent: the client sees a truncated archive
	}
}
