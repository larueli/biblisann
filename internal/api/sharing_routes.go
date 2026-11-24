package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/larueli/biblisann/internal/library"
)

type grantBody struct {
	Role      string     `json:"role" enum:"read,edit,admin,custom"`
	ProfileID *uuid.UUID `json:"profile_id,omitempty" doc:"Required (and only allowed) when role is custom."`
}

type profileBody struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Permissions []string  `json:"permissions" nullable:"false"`
}

func (s *Server) registerSharing(a huma.API) {
	type libPath struct {
		ID uuid.UUID `path:"id"`
	}
	type memberBody struct {
		UserID    uuid.UUID  `json:"user_id"`
		Email     string     `json:"email"`
		Name      string     `json:"name"`
		Role      string     `json:"role"`
		ProfileID *uuid.UUID `json:"profile_id,omitempty"`
	}
	type groupShareBody struct {
		GroupID   uuid.UUID  `json:"group_id"`
		Name      string     `json:"name"`
		Role      string     `json:"role"`
		ProfileID *uuid.UUID `json:"profile_id,omitempty"`
	}
	type sharingBody struct {
		Users  []memberBody     `json:"users" nullable:"false"`
		Groups []groupShareBody `json:"groups" nullable:"false"`
	}
	Register(a, huma.Operation{OperationID: "list-sharing", Method: http.MethodGet, Path: "/v1/libraries/{id}/sharing",
		Summary: "Who has access to this library (users and groups)", Tags: []string{"sharing"}}, PermLibraryAdmin,
		func(ctx context.Context, in *libPath) (*struct{ Body sharingBody }, error) {
			sh, err := s.Library.ListSharing(ctx, in.ID)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := sharingBody{Users: []memberBody{}, Groups: []groupShareBody{}}
			for _, u := range sh.Users {
				out.Users = append(out.Users, memberBody{u.UserID, u.Email, u.DisplayName, u.Role, u.ProfileID})
			}
			for _, g := range sh.Groups {
				out.Groups = append(out.Groups, groupShareBody{g.GroupID, g.Name, g.Role, g.ProfileID})
			}
			return &struct{ Body sharingBody }{out}, nil
		})
	type shareResult struct {
		Status        string `json:"status" enum:"ok,invited" doc:"ok: nothing more to do (or nothing happened, for an address that cannot be shared with). invited: an invitation was created."`
		InvitationURL string `json:"invitation_url,omitempty" doc:"Only for instance administrators: the link to send when no mail server is configured."`
	}
	Register(a, huma.Operation{OperationID: "share-with-user", Method: http.MethodPut, Path: "/v1/libraries/{id}/sharing/users",
		Summary: "Give a registered user (by email) access, or change their role",
		Description: "Sharing needs library administration. For an address without an account, only instance administrators get an answer " +
			"(409 no_account) and may repeat the request with invite=true to create an invitation (a link valid 7 days, emailed when SMTP is configured). " +
			"For everyone else the call succeeds without doing anything, exactly as for a known address.",
		Tags: []string{"sharing"}, Errors: []int{409, 422}}, PermLibraryAdmin,
		func(ctx context.Context, in *struct {
			ID   uuid.UUID `path:"id"`
			Body struct {
				Email  string    `json:"email" maxLength:"320"`
				Grant  grantBody `json:"grant"`
				Invite bool      `json:"invite,omitempty" doc:"Confirm that an invitation must be created when the address has no account."`
			}
		}) (*struct{ Body shareResult }, error) {
			g := library.Grant{Role: in.Body.Grant.Role, ProfileID: in.Body.Grant.ProfileID}
			err := s.Library.ShareWithUser(ctx, in.ID, in.Body.Email, g)
			if errors.Is(err, library.ErrNoAccount) {
				if !in.Body.Invite {
					return nil, huma.Error409Conflict("no_account")
				}
				if err := s.requireStepUp(ctx); err != nil {
					return nil, err
				}
				if err := s.throttle(ctx, "invite", in.ID.String()); err != nil {
					return nil, err
				}
				token, err := s.Library.Invite(ctx, in.ID, in.Body.Email, g)
				if err != nil {
					return nil, mapError(s.Log, err)
				}
				link := s.BaseURL.String() + "/invite?token=" + token
				out := shareResult{Status: "invited", InvitationURL: link}
				if s.Mailer != nil {
					body := "You were invited to a Biblisann library.\n\n" + link + "\n\nThe link works once for 7 days. If you did not expect this, ignore this message.\n"
					if err := s.Mailer.SendMail(ctx, in.Body.Email, "Invitation to Biblisann", body); err != nil {
						s.Log.Warn("invitation mail failed", "error", err)
					}
				}
				return &struct{ Body shareResult }{out}, nil
			}
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return &struct{ Body shareResult }{shareResult{Status: "ok"}}, nil
		})
	Register(a, huma.Operation{OperationID: "peek-invitation", Method: http.MethodGet, Path: "/v1/invitations/{token}",
		Summary: "What an invitation link is for (before signing up)", Tags: []string{"sharing"}, Errors: []int{404, 429}}, PermPublic,
		func(ctx context.Context, in *struct {
			Token string `path:"token" maxLength:"128"`
		}) (*struct {
			Body struct {
				Email       string `json:"email"`
				LibraryName string `json:"library_name"`
			}
		}, error) {
			if err := s.throttle(ctx, "invitation", ""); err != nil {
				return nil, err
			}
			info, err := s.Library.PeekInvitation(ctx, in.Token)
			if err != nil {
				return nil, huma.Error404NotFound("not found")
			}
			out := &struct {
				Body struct {
					Email       string `json:"email"`
					LibraryName string `json:"library_name"`
				}
			}{}
			out.Body.Email, out.Body.LibraryName = info.Email, info.LibraryName
			return out, nil
		})
	Register(a, huma.Operation{OperationID: "accept-invitation", Method: http.MethodPost, Path: "/v1/invitations/accept",
		Summary: "Use an invitation link with my account", Tags: []string{"sharing"}, Errors: []int{404, 429}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			Body struct {
				Token string `json:"token" maxLength:"128"`
			}
		}) (*struct {
			Body struct {
				LibraryID uuid.UUID `json:"library_id"`
			}
		}, error) {
			sess := SessionFrom(ctx)
			if sess == nil {
				return nil, huma.Error403Forbidden("a browser session is required")
			}
			if err := s.throttle(ctx, "invitation", sess.UserID.String()); err != nil {
				return nil, err
			}
			id, err := s.Library.AcceptInvitation(ctx, in.Body.Token, sess.UserID)
			if err != nil {
				return nil, huma.Error404NotFound("not found")
			}
			out := &struct {
				Body struct {
					LibraryID uuid.UUID `json:"library_id"`
				}
			}{}
			out.Body.LibraryID = id
			return out, nil
		})
	Register(a, huma.Operation{OperationID: "unshare-user", Method: http.MethodDelete, Path: "/v1/libraries/{id}/sharing/users/{user_id}",
		Summary: "Remove a user's access", Tags: []string{"sharing"}, DefaultStatus: http.StatusNoContent, Errors: []int{404, 409}}, PermLibraryAdmin,
		func(ctx context.Context, in *struct {
			ID     uuid.UUID `path:"id"`
			UserID uuid.UUID `path:"user_id"`
		}) (*struct{}, error) {
			return nil, mapError(s.Log, s.Library.UnshareUser(ctx, in.ID, in.UserID))
		})
	Register(a, huma.Operation{OperationID: "share-with-group", Method: http.MethodPut, Path: "/v1/libraries/{id}/sharing/groups/{group_id}",
		Summary: "Give a group access, or change its role", Tags: []string{"sharing"}, DefaultStatus: http.StatusNoContent, Errors: []int{422}}, PermLibraryAdmin,
		func(ctx context.Context, in *struct {
			ID      uuid.UUID `path:"id"`
			GroupID uuid.UUID `path:"group_id"`
			Body    grantBody
		}) (*struct{}, error) {
			return nil, mapError(s.Log, s.Library.ShareWithGroup(ctx, in.ID, in.GroupID, library.Grant{Role: in.Body.Role, ProfileID: in.Body.ProfileID}))
		})
	Register(a, huma.Operation{OperationID: "unshare-group", Method: http.MethodDelete, Path: "/v1/libraries/{id}/sharing/groups/{group_id}",
		Summary: "Remove a group's access", Tags: []string{"sharing"}, DefaultStatus: http.StatusNoContent, Errors: []int{404}}, PermLibraryAdmin,
		func(ctx context.Context, in *struct {
			ID      uuid.UUID `path:"id"`
			GroupID uuid.UUID `path:"group_id"`
		}) (*struct{}, error) {
			return nil, mapError(s.Log, s.Library.UnshareGroup(ctx, in.ID, in.GroupID))
		})

	Register(a, huma.Operation{OperationID: "list-profiles", Method: http.MethodGet, Path: "/v1/libraries/{id}/profiles",
		Summary: "Custom permission profiles of a library", Tags: []string{"sharing"}}, PermLibraryAdmin,
		func(ctx context.Context, in *libPath) (*struct{ Body []profileBody }, error) {
			rows, err := s.Library.ListProfiles(ctx, in.ID)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := make([]profileBody, len(rows))
			for i, p := range rows {
				out[i] = profileBody{p.ID, p.Name, p.Permissions}
			}
			return &struct{ Body []profileBody }{out}, nil
		})
	Register(a, huma.Operation{OperationID: "create-profile", Method: http.MethodPost, Path: "/v1/libraries/{id}/profiles",
		Summary: "Define a custom permission profile", Tags: []string{"sharing"}, DefaultStatus: http.StatusCreated, Errors: []int{409, 422}}, PermLibraryAdmin,
		func(ctx context.Context, in *struct {
			ID   uuid.UUID `path:"id"`
			Body struct {
				Name        string   `json:"name" minLength:"1" maxLength:"100"`
				Permissions []string `json:"permissions" minItems:"1" doc:"Any of library.read, instance.write, place.write, loan.write, list.write, library.share, library.admin."`
			}
		}) (*struct{ Body profileBody }, error) {
			p, err := s.Library.CreateProfile(ctx, in.ID, in.Body.Name, in.Body.Permissions)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return &struct{ Body profileBody }{profileBody{p.ID, p.Name, p.Permissions}}, nil
		})
	Register(a, huma.Operation{OperationID: "delete-profile", Method: http.MethodDelete, Path: "/v1/profiles/{id}",
		Summary: "Delete a profile (grants using it are removed)", Tags: []string{"sharing"}, DefaultStatus: http.StatusNoContent}, PermLibraryAdmin,
		func(ctx context.Context, in *libPath) (*struct{}, error) {
			return nil, mapError(s.Log, s.Library.DeleteProfile(ctx, in.ID))
		})

	type groupBody struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	}
	Register(a, huma.Operation{OperationID: "list-groups", Method: http.MethodGet, Path: "/v1/groups",
		Summary: "User groups", Tags: []string{"sharing"}}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct{ Body []groupBody }, error) {
			rows, err := s.Library.ListGroups(ctx)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := make([]groupBody, len(rows))
			for i, g := range rows {
				out[i] = groupBody{g.ID, g.Name}
			}
			return &struct{ Body []groupBody }{out}, nil
		})
	Register(a, huma.Operation{OperationID: "create-group", Method: http.MethodPost, Path: "/v1/groups",
		Summary: "Create a user group (instance administrators)", Tags: []string{"sharing"}, DefaultStatus: http.StatusCreated, Errors: []int{409}}, PermInstanceAdmin,
		func(ctx context.Context, in *struct {
			Body struct {
				Name string `json:"name" minLength:"1" maxLength:"200"`
			}
		}) (*struct{ Body groupBody }, error) {
			g, err := s.Library.CreateGroup(ctx, in.Body.Name)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return &struct{ Body groupBody }{groupBody{g.ID, g.Name}}, nil
		})
	Register(a, huma.Operation{OperationID: "delete-group", Method: http.MethodDelete, Path: "/v1/groups/{id}",
		Summary: "Delete a user group (instance administrators)", Tags: []string{"sharing"}, DefaultStatus: http.StatusNoContent}, PermInstanceAdmin,
		func(ctx context.Context, in *libPath) (*struct{}, error) {
			return nil, mapError(s.Log, s.Library.DeleteGroup(ctx, in.ID))
		})
	type groupMemberBody struct {
		UserID uuid.UUID `json:"user_id"`
		Email  string    `json:"email"`
		Name   string    `json:"name"`
	}
	Register(a, huma.Operation{OperationID: "list-group-members", Method: http.MethodGet, Path: "/v1/groups/{id}/members",
		Summary: "Members of a user group (instance administrators)", Tags: []string{"sharing"}}, PermInstanceAdmin,
		func(ctx context.Context, in *libPath) (*struct{ Body []groupMemberBody }, error) {
			rows, err := s.Library.GroupMembers(ctx, in.ID)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := make([]groupMemberBody, len(rows))
			for i, r := range rows {
				out[i] = groupMemberBody{r.ID, r.Email, r.DisplayName}
			}
			return &struct{ Body []groupMemberBody }{out}, nil
		})
	Register(a, huma.Operation{OperationID: "set-group-member", Method: http.MethodPut, Path: "/v1/groups/{id}/members",
		Summary: "Add or remove a user (by email) in a group (instance administrators)", Tags: []string{"sharing"}, DefaultStatus: http.StatusNoContent}, PermInstanceAdmin,
		func(ctx context.Context, in *struct {
			ID   uuid.UUID `path:"id"`
			Body struct {
				Email  string `json:"email" maxLength:"320"`
				Member bool   `json:"member"`
			}
		}) (*struct{}, error) {
			return nil, mapError(s.Log, s.Library.SetGroupMember(ctx, in.ID, in.Body.Email, in.Body.Member))
		})
}
