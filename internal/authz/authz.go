// Package authz holds the authenticated principal and the role checks used by the service
// layer. Everything is denied unless a rule allows it.
package authz

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Errors returned by checks. Not-found and forbidden are distinct for the caller to map, but a
// non-member must get ErrNotFound so library IDs cannot be probed.
var (
	ErrUnauthenticated = errors.New("authentication required")
	ErrForbidden       = errors.New("forbidden")
	ErrNotFound        = errors.New("not found")
)

// Role is a library role, ordered by privilege.
type Role int

// Library roles.
const (
	RoleNone Role = iota
	RoleRead
	RoleEdit
	RoleAdmin
)

// ParseRole maps the database value to a Role.
func ParseRole(s string) Role {
	switch s {
	case "read":
		return RoleRead
	case "edit":
		return RoleEdit
	case "admin":
		return RoleAdmin
	}
	return RoleNone
}

// String returns the database value.
func (r Role) String() string {
	return [...]string{"none", "read", "edit", "admin"}[r]
}

// Principal is the authenticated caller.
type Principal struct {
	UserID  uuid.UUID
	Email   string
	IsAdmin bool
	// Scopes limits a token-based caller. nil means a full interactive session.
	Scopes []string
	// Via is "session" or "token".
	Via string
	// LibraryIDs restricts a token to some libraries. nil means every accessible library.
	LibraryIDs []uuid.UUID
}

// AllowsLibrary reports whether the principal may touch the library at all (token restriction).
func (p *Principal) AllowsLibrary(id uuid.UUID) bool {
	if p.LibraryIDs == nil {
		return true
	}
	for _, l := range p.LibraryIDs {
		if l == id {
			return true
		}
	}
	return false
}

type ctxKey struct{}

// With stores p in ctx.
func With(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// From returns the principal or ErrUnauthenticated.
func From(ctx context.Context) (*Principal, error) {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	if p == nil {
		return nil, ErrUnauthenticated
	}
	return p, nil
}

// HasScope reports whether the principal may use scope. Sessions are unrestricted.
func (p *Principal) HasScope(scope string) bool {
	if p.Scopes == nil {
		return true
	}
	for _, s := range p.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// Allows reports whether have is enough for need.
func Allows(have, need Role) bool { return have != RoleNone && have >= need }

// Permissions a library grant can contain. The built-in roles are fixed sets; custom profiles
// are arbitrary subsets defined per library. PermAdmin implies every other permission.
const (
	PermRead  = "library.read"
	PermWrite = "instance.write"
	PermPlace = "place.write"
	PermLoan  = "loan.write"
	PermList  = "list.write"
	PermShare = "library.share"
	PermAdmin = "library.admin"
)

// AllPermissions lists the permissions a custom profile may hold.
var AllPermissions = []string{PermRead, PermWrite, PermPlace, PermLoan, PermList, PermShare, PermAdmin}

// RolePermissions returns the permissions of a built-in role.
func RolePermissions(role string) []string {
	switch role {
	case "read":
		return []string{PermRead}
	case "edit":
		return []string{PermRead, PermWrite, PermPlace, PermLoan, PermList}
	case "admin":
		return []string{PermAdmin}
	}
	return nil
}

// HasPermission reports whether a set of permissions grants need.
func HasPermission(have []string, need string) bool {
	for _, p := range have {
		if p == need || p == PermAdmin {
			return true
		}
	}
	return false
}

// ValidPermission reports whether p is a known permission.
func ValidPermission(p string) bool {
	for _, k := range AllPermissions {
		if k == p {
			return true
		}
	}
	return false
}
