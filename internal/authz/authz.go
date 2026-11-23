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
