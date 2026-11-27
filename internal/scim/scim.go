// Package scim implements the SCIM 2.0 (RFC 7643, 7644) Users and Groups endpoints so an identity
// provider can provision accounts and group membership. Authentication is a dedicated bearer
// token created by an instance administrator. Users created this way have no password: they sign
// in through OIDC, which links to them by verified email. Deleting a user deactivates the account
// (it is not erased) so ownership of libraries is never lost by accident.
package scim

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/larueli/biblisann/internal/auth"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

const (
	contentType = "application/scim+json"
	schemaUser  = "urn:ietf:params:scim:schemas:core:2.0:User"
	schemaGroup = "urn:ietf:params:scim:schemas:core:2.0:Group"
	schemaList  = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	schemaErr   = "urn:ietf:params:scim:api:messages:2.0:Error"
	schemaPatch = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	maxBody     = 1 << 20
)

// Authenticator validates the bearer token.
type Authenticator interface {
	ValidSCIM(ctx context.Context, secret string) bool
}

// Handler serves /scim/v2.
type Handler struct {
	q    *dbsqlc.Queries
	pool *pgxpool.Pool
	auth Authenticator
	base string // public base URL, for meta.location
}

// New creates the handler.
func New(pool *pgxpool.Pool, a Authenticator, baseURL string) *Handler {
	return &Handler{q: dbsqlc.New(pool), pool: pool, auth: a, base: strings.TrimSuffix(baseURL, "/")}
}

// Routes registers the endpoints on mux under /scim/v2.
func (h *Handler) Routes(mux *http.ServeMux) {
	r := func(pattern string, f http.HandlerFunc) { mux.HandleFunc(pattern, h.guard(f)) }
	r("GET /scim/v2/ServiceProviderConfig", h.serviceConfig)
	r("GET /scim/v2/ResourceTypes", h.resourceTypes)
	r("GET /scim/v2/Users", h.listUsers)
	r("POST /scim/v2/Users", h.createUser)
	r("GET /scim/v2/Users/{id}", h.getUser)
	r("PUT /scim/v2/Users/{id}", h.replaceUser)
	r("PATCH /scim/v2/Users/{id}", h.patchUser)
	r("DELETE /scim/v2/Users/{id}", h.deleteUser)
	r("GET /scim/v2/Groups", h.listGroups)
	r("POST /scim/v2/Groups", h.createGroup)
	r("GET /scim/v2/Groups/{id}", h.getGroup)
	r("PUT /scim/v2/Groups/{id}", h.replaceGroup)
	r("PATCH /scim/v2/Groups/{id}", h.patchGroup)
	r("DELETE /scim/v2/Groups/{id}", h.deleteGroup)
}

func (h *Handler) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == r.Header.Get("Authorization") || !h.auth.ValidSCIM(r.Context(), tok) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeErr(w, http.StatusUnauthorized, "", "authentication required")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, scimType, detail string) {
	body := map[string]any{"schemas": []string{schemaErr}, "status": strconv.Itoa(status), "detail": detail}
	if scimType != "" {
		body["scimType"] = scimType
	}
	writeJSON(w, status, body)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalidSyntax", "invalid JSON")
		return false
	}
	return true
}

func pageParams(r *http.Request) (int64, int32, int32) {
	query := r.URL.Query()
	start, _ := strconv.ParseInt(query.Get("startIndex"), 10, 64)
	if start < 1 {
		start = 1
	}
	offset := start - 1
	if offset > math.MaxInt32 {
		offset = math.MaxInt32
	}
	count, err := strconv.ParseInt(query.Get("count"), 10, 32)
	if err != nil || count < 0 || count > 200 {
		count = 100
	}
	return offset + 1, int32(count), int32(offset)
}

// eqFilter extracts the value of a simple `attr eq "value"` filter; other filters are refused.
func eqFilter(r *http.Request, attr string) (string, bool) {
	f := strings.TrimSpace(r.URL.Query().Get("filter"))
	if f == "" {
		return "", true
	}
	parts := strings.SplitN(f, " ", 3)
	if len(parts) != 3 || !strings.EqualFold(parts[0], attr) || !strings.EqualFold(parts[1], "eq") {
		return "", false
	}
	v := strings.TrimSpace(parts[2])
	if len(v) < 2 || v[0] != '"' || v[len(v)-1] != '"' {
		return "", false
	}
	return v[1 : len(v)-1], true
}

func (h *Handler) serviceConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{
		"schemas":        []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"patch":          map[string]bool{"supported": true},
		"bulk":           map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
		"filter":         map[string]any{"supported": true, "maxResults": 200},
		"changePassword": map[string]bool{"supported": false},
		"sort":           map[string]bool{"supported": false},
		"etag":           map[string]bool{"supported": false},
		"authenticationSchemes": []map[string]string{{"type": "oauthbearertoken", "name": "Bearer token",
			"description": "Token created by an instance administrator"}},
	})
}

func (h *Handler) resourceTypes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"schemas": []string{schemaList}, "totalResults": 2, "Resources": []map[string]any{
		{"id": "User", "name": "User", "endpoint": "/Users", "schema": schemaUser},
		{"id": "Group", "name": "Group", "endpoint": "/Groups", "schema": schemaGroup},
	}})
}

// ---- users

type userResource struct {
	Schemas  []string `json:"schemas"`
	ID       string   `json:"id,omitempty"`
	UserName string   `json:"userName"`
	Name     *struct {
		Formatted string `json:"formatted,omitempty"`
	} `json:"name,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Emails      []struct {
		Value   string `json:"value"`
		Primary bool   `json:"primary,omitempty"`
	} `json:"emails,omitempty"`
	Active *bool `json:"active,omitempty"`
	Meta   *struct {
		ResourceType string `json:"resourceType"`
		Location     string `json:"location,omitempty"`
	} `json:"meta,omitempty"`
}

func (h *Handler) userOut(u dbsqlc.User) userResource {
	active := u.DisabledAt == nil
	r := userResource{Schemas: []string{schemaUser}, ID: u.ID.String(), UserName: u.Email, DisplayName: u.DisplayName, Active: &active}
	r.Name = &struct {
		Formatted string `json:"formatted,omitempty"`
	}{u.DisplayName}
	r.Emails = []struct {
		Value   string `json:"value"`
		Primary bool   `json:"primary,omitempty"`
	}{{u.Email, true}}
	r.Meta = &struct {
		ResourceType string `json:"resourceType"`
		Location     string `json:"location,omitempty"`
	}{"User", h.base + "/scim/v2/Users/" + u.ID.String()}
	return r
}

func (r userResource) email() string {
	if r.UserName != "" {
		return r.UserName
	}
	for _, e := range r.Emails {
		if e.Primary || len(r.Emails) == 1 {
			return e.Value
		}
	}
	return ""
}

func (r userResource) display() string {
	switch {
	case r.DisplayName != "":
		return r.DisplayName
	case r.Name != nil:
		return r.Name.Formatted
	}
	return ""
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	name, ok := eqFilter(r, "userName")
	if !ok {
		writeErr(w, 400, "invalidFilter", `only: userName eq "value"`)
		return
	}
	start, limit, offset := pageParams(r)
	total, _ := h.q.ScimCountUsers(r.Context(), name)
	rows, err := h.q.ScimListUsers(r.Context(), dbsqlc.ScimListUsersParams{Column1: name, Limit: limit, Offset: offset})
	if err != nil {
		writeErr(w, 500, "", "internal error")
		return
	}
	res := make([]userResource, len(rows))
	for i, u := range rows {
		res[i] = h.userOut(u)
	}
	writeJSON(w, 200, map[string]any{"schemas": []string{schemaList}, "totalResults": total, "startIndex": start, "itemsPerPage": len(res), "Resources": res})
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	var in userResource
	if !decode(w, r, &in) {
		return
	}
	email, err := auth.NormalizeEmail(in.email())
	if err != nil {
		writeErr(w, 400, "invalidValue", "userName must be an email address")
		return
	}
	var disabled *time.Time
	if in.Active != nil && !*in.Active {
		now := time.Now()
		disabled = &now
	}
	u, err := h.q.CreateScimUser(r.Context(), dbsqlc.CreateScimUserParams{Email: email, DisplayName: in.display(), DisabledAt: disabled})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		writeErr(w, 409, "uniqueness", "userName already exists")
		return
	}
	if err != nil {
		writeErr(w, 500, "", "internal error")
		return
	}
	writeJSON(w, 201, h.userOut(u))
}

func (h *Handler) user(w http.ResponseWriter, r *http.Request) (dbsqlc.User, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err == nil {
		var u dbsqlc.User
		if u, err = h.q.GetUser(r.Context(), id); err == nil {
			return u, true
		}
	}
	writeErr(w, 404, "", "user not found")
	return dbsqlc.User{}, false
}

// writableUser is user() for operations that change the account. Instance administrators are never
// managed through SCIM: a leaked provisioning token must not be able to rewrite their email address.
func (h *Handler) writableUser(w http.ResponseWriter, r *http.Request) (dbsqlc.User, bool) {
	u, ok := h.user(w, r)
	if ok && u.IsAdmin {
		writeErr(w, 403, "", "instance administrators are not managed through SCIM")
		return dbsqlc.User{}, false
	}
	return u, ok
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) {
	if u, ok := h.user(w, r); ok {
		writeJSON(w, 200, h.userOut(u))
	}
}

func (h *Handler) apply(w http.ResponseWriter, r *http.Request, u dbsqlc.User, email, name string, active *bool) {
	e, err := auth.NormalizeEmail(email)
	if err != nil {
		writeErr(w, 400, "invalidValue", "userName must be an email address")
		return
	}
	disabled := u.DisabledAt
	if active != nil {
		switch {
		case *active:
			disabled = nil
		case disabled == nil:
			now := time.Now()
			disabled = &now
		}
	}
	up, err := h.q.ScimUpdateUser(r.Context(), dbsqlc.ScimUpdateUserParams{ID: u.ID, Email: e, DisplayName: name, DisabledAt: disabled})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		writeErr(w, 409, "uniqueness", "userName already exists")
		return
	}
	if err != nil {
		writeErr(w, 500, "", "internal error")
		return
	}
	if disabled != nil {
		_ = h.q.DeleteUserSessions(r.Context(), u.ID) // a deactivated user is signed out everywhere
	}
	writeJSON(w, 200, h.userOut(up))
}

func (h *Handler) replaceUser(w http.ResponseWriter, r *http.Request) {
	u, ok := h.writableUser(w, r)
	if !ok {
		return
	}
	var in userResource
	if !decode(w, r, &in) {
		return
	}
	email := in.email()
	if email == "" {
		email = u.Email
	}
	h.apply(w, r, u, email, in.display(), in.Active)
}

type patchOp struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

type patchRequest struct {
	Schemas    []string  `json:"schemas"`
	Operations []patchOp `json:"Operations"`
}

func (h *Handler) patchUser(w http.ResponseWriter, r *http.Request) {
	u, ok := h.writableUser(w, r)
	if !ok {
		return
	}
	var req patchRequest
	if !decode(w, r, &req) {
		return
	}
	email, name := u.Email, u.DisplayName
	var active *bool
	for _, op := range req.Operations {
		if !strings.EqualFold(op.Op, "replace") && !strings.EqualFold(op.Op, "add") {
			writeErr(w, 400, "invalidValue", "only add and replace are supported on users")
			return
		}
		set := func(path string, raw json.RawMessage) bool {
			switch strings.ToLower(path) {
			case "active":
				var b bool
				if json.Unmarshal(raw, &b) != nil {
					var s string // some providers send "False"
					_ = json.Unmarshal(raw, &s)
					b = strings.EqualFold(s, "true")
				}
				active = &b
			case "username":
				_ = json.Unmarshal(raw, &email)
			case "displayname", "name.formatted":
				_ = json.Unmarshal(raw, &name)
			default:
				return false
			}
			return true
		}
		if op.Path != "" {
			if !set(op.Path, op.Value) {
				writeErr(w, 400, "invalidPath", "unsupported path "+op.Path)
				return
			}
			continue
		}
		var m map[string]json.RawMessage // no path: value is an object of attributes
		if json.Unmarshal(op.Value, &m) != nil {
			writeErr(w, 400, "invalidSyntax", "invalid value")
			return
		}
		for k, v := range m {
			set(k, v)
		}
	}
	h.apply(w, r, u, email, name, active)
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	u, ok := h.writableUser(w, r)
	if !ok {
		return
	}
	f := false
	h.apply(&discard{ResponseWriter: w}, r, u, u.Email, u.DisplayName, &f)
	w.WriteHeader(http.StatusNoContent)
}

// discard swallows the body of the inner response so DELETE can answer 204.
type discard struct{ http.ResponseWriter }

func (*discard) Write(b []byte) (int, error) { return len(b), nil }
func (*discard) WriteHeader(int)             {}
func (d *discard) Header() http.Header       { return http.Header{} }

// ---- groups

type groupResource struct {
	Schemas     []string `json:"schemas"`
	ID          string   `json:"id,omitempty"`
	DisplayName string   `json:"displayName"`
	ExternalID  string   `json:"externalId,omitempty"`
	Members     []member `json:"members,omitempty"`
	Meta        *struct {
		ResourceType string `json:"resourceType"`
		Location     string `json:"location,omitempty"`
	} `json:"meta,omitempty"`
}

type member struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
}

func (h *Handler) groupOut(ctx context.Context, g dbsqlc.Group) groupResource {
	r := groupResource{Schemas: []string{schemaGroup}, ID: g.ID.String(), DisplayName: g.Name}
	if g.ExternalID != nil {
		r.ExternalID = *g.ExternalID
	}
	ms, _ := h.q.ListGroupMembers(ctx, g.ID)
	for _, m := range ms {
		r.Members = append(r.Members, member{m.ID.String(), m.Email})
	}
	r.Meta = &struct {
		ResourceType string `json:"resourceType"`
		Location     string `json:"location,omitempty"`
	}{"Group", h.base + "/scim/v2/Groups/" + g.ID.String()}
	return r
}

func (h *Handler) listGroups(w http.ResponseWriter, r *http.Request) {
	name, ok := eqFilter(r, "displayName")
	if !ok {
		writeErr(w, 400, "invalidFilter", `only: displayName eq "value"`)
		return
	}
	start, limit, offset := pageParams(r)
	total, _ := h.q.ScimCountGroups(r.Context(), name)
	rows, err := h.q.ScimListGroups(r.Context(), dbsqlc.ScimListGroupsParams{Column1: name, Limit: limit, Offset: offset})
	if err != nil {
		writeErr(w, 500, "", "internal error")
		return
	}
	res := make([]groupResource, len(rows))
	for i, g := range rows {
		res[i] = h.groupOut(r.Context(), g)
	}
	writeJSON(w, 200, map[string]any{"schemas": []string{schemaList}, "totalResults": total, "startIndex": start, "itemsPerPage": len(res), "Resources": res})
}

func (h *Handler) group(w http.ResponseWriter, r *http.Request) (dbsqlc.Group, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err == nil {
		var g dbsqlc.Group
		if g, err = h.q.GetGroup(r.Context(), id); err == nil {
			return g, true
		}
	}
	writeErr(w, 404, "", "group not found")
	return dbsqlc.Group{}, false
}

func (h *Handler) setMembers(ctx context.Context, groupID uuid.UUID, ms []member) error {
	want := map[uuid.UUID]bool{}
	for _, m := range ms {
		id, err := uuid.Parse(m.Value)
		if err != nil {
			return errors.New("invalid member id")
		}
		if _, err := h.q.GetUser(ctx, id); err != nil {
			return errors.New("unknown member")
		}
		want[id] = true
	}
	cur, err := h.q.ListGroupMembers(ctx, groupID)
	if err != nil {
		return err
	}
	for _, c := range cur {
		if !want[c.ID] {
			if err := h.q.RemoveGroupMember(ctx, dbsqlc.RemoveGroupMemberParams{GroupID: groupID, UserID: c.ID}); err != nil {
				return err
			}
		}
	}
	for id := range want {
		if err := h.q.AddGroupMember(ctx, dbsqlc.AddGroupMemberParams{GroupID: groupID, UserID: id}); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) createGroup(w http.ResponseWriter, r *http.Request) {
	var in groupResource
	if !decode(w, r, &in) {
		return
	}
	name := strings.TrimSpace(in.DisplayName)
	if name == "" || len(name) > 200 {
		writeErr(w, 400, "invalidValue", "displayName is required")
		return
	}
	var ext *string
	if in.ExternalID != "" {
		ext = &in.ExternalID
	}
	g, err := h.q.CreateGroup(r.Context(), dbsqlc.CreateGroupParams{Name: name, ExternalID: ext})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		writeErr(w, 409, "uniqueness", "displayName already exists")
		return
	}
	if err != nil {
		writeErr(w, 500, "", "internal error")
		return
	}
	if err := h.setMembers(r.Context(), g.ID, in.Members); err != nil {
		writeErr(w, 400, "invalidValue", err.Error())
		return
	}
	writeJSON(w, 201, h.groupOut(r.Context(), g))
}

func (h *Handler) getGroup(w http.ResponseWriter, r *http.Request) {
	if g, ok := h.group(w, r); ok {
		writeJSON(w, 200, h.groupOut(r.Context(), g))
	}
}

func (h *Handler) replaceGroup(w http.ResponseWriter, r *http.Request) {
	g, ok := h.group(w, r)
	if !ok {
		return
	}
	var in groupResource
	if !decode(w, r, &in) {
		return
	}
	if name := strings.TrimSpace(in.DisplayName); name != "" && name != g.Name {
		var err error
		if g, err = h.q.ScimRenameGroup(r.Context(), dbsqlc.ScimRenameGroupParams{ID: g.ID, Name: name}); err != nil {
			writeErr(w, 409, "uniqueness", "displayName already exists")
			return
		}
	}
	if err := h.setMembers(r.Context(), g.ID, in.Members); err != nil {
		writeErr(w, 400, "invalidValue", err.Error())
		return
	}
	writeJSON(w, 200, h.groupOut(r.Context(), g))
}

func (h *Handler) patchGroup(w http.ResponseWriter, r *http.Request) {
	g, ok := h.group(w, r)
	if !ok {
		return
	}
	var req patchRequest
	if !decode(w, r, &req) {
		return
	}
	ctx := r.Context()
	for _, op := range req.Operations {
		path := strings.ToLower(op.Path)
		verb := strings.ToLower(op.Op)
		switch {
		case path == "members" && (verb == "add" || verb == "replace"):
			var ms []member
			if json.Unmarshal(op.Value, &ms) != nil {
				writeErr(w, 400, "invalidSyntax", "invalid members")
				return
			}
			if verb == "replace" {
				if err := h.setMembers(ctx, g.ID, ms); err != nil {
					writeErr(w, 400, "invalidValue", err.Error())
					return
				}
				continue
			}
			for _, m := range ms {
				id, err := uuid.Parse(m.Value)
				if err != nil {
					writeErr(w, 400, "invalidValue", "invalid member id")
					return
				}
				if _, err := h.q.GetUser(ctx, id); errors.Is(err, pgx.ErrNoRows) {
					writeErr(w, 400, "invalidValue", "unknown member")
					return
				}
				_ = h.q.AddGroupMember(ctx, dbsqlc.AddGroupMemberParams{GroupID: g.ID, UserID: id})
			}
		case strings.HasPrefix(path, "members") && verb == "remove":
			// Either members[value eq "id"] or a value list.
			if i := strings.Index(op.Path, `"`); i >= 0 {
				if j := strings.LastIndex(op.Path, `"`); j > i {
					if id, err := uuid.Parse(op.Path[i+1 : j]); err == nil {
						_ = h.q.RemoveGroupMember(ctx, dbsqlc.RemoveGroupMemberParams{GroupID: g.ID, UserID: id})
					}
				}
				continue
			}
			var ms []member
			_ = json.Unmarshal(op.Value, &ms)
			for _, m := range ms {
				if id, err := uuid.Parse(m.Value); err == nil {
					_ = h.q.RemoveGroupMember(ctx, dbsqlc.RemoveGroupMemberParams{GroupID: g.ID, UserID: id})
				}
			}
		case path == "displayname" && verb == "replace":
			var n string
			_ = json.Unmarshal(op.Value, &n)
			if n = strings.TrimSpace(n); n != "" {
				var err error
				if g, err = h.q.ScimRenameGroup(ctx, dbsqlc.ScimRenameGroupParams{ID: g.ID, Name: n}); err != nil {
					writeErr(w, 409, "uniqueness", "displayName already exists")
					return
				}
			}
		default:
			writeErr(w, 400, "invalidPath", "unsupported operation")
			return
		}
	}
	writeJSON(w, 200, h.groupOut(ctx, g))
}

func (h *Handler) deleteGroup(w http.ResponseWriter, r *http.Request) {
	g, ok := h.group(w, r)
	if !ok {
		return
	}
	if err := h.q.DeleteGroup(r.Context(), g.ID); err != nil {
		writeErr(w, 500, "", "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
