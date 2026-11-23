package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/larueli/biblisann/internal/authz"
)

// Permission names what a caller needs to invoke an operation. Authorization itself is
// enforced in the service layer (deny by default); the declaration here is documentation and a
// guard against forgetting to think about it.
type Permission string

// Permissions. The library roles are checked per library by the service layer.
const (
	// PermPublic marks operations that need no authentication.
	PermPublic Permission = "public"
	// PermAuthenticated needs a signed-in user and no particular library.
	PermAuthenticated Permission = "authenticated"
	// PermLibraryRead needs the read role (or better) in the library concerned.
	PermLibraryRead Permission = "library:read"
	// PermLibraryEdit needs the edit role (or better) in the library concerned.
	PermLibraryEdit Permission = "library:edit"
	// PermLibraryAdmin needs the admin role in the library concerned.
	PermLibraryAdmin Permission = "library:admin"
	// PermInstanceAdmin needs an instance administrator account.
	PermInstanceAdmin Permission = "instance:admin"
)

// Register adds an operation and records its required permission. It panics at startup when
// the permission is empty, so a forgotten declaration can never ship.
func Register[I, O any](a huma.API, op huma.Operation, perm Permission, h func(context.Context, *I) (*O, error)) {
	if perm == "" {
		panic(fmt.Sprintf("api: operation %q declares no permission", op.OperationID))
	}
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Extensions["x-permission"] = string(perm)
	if perm != PermPublic {
		// Deny by default: the operation needs a principal even before the service layer runs.
		op.Errors = append(op.Errors, http.StatusUnauthorized)
		inner := h
		h = func(ctx context.Context, in *I) (*O, error) {
			if _, err := authz.From(ctx); err != nil {
				return nil, huma.Error401Unauthorized("authentication required")
			}
			return inner(ctx, in)
		}
	}
	huma.Register(a, op, h)
}
