package api

import (
	"context"
	"fmt"

	"github.com/danielgtaylor/huma/v2"
)

// Permission names what a caller needs to invoke an operation. Authorization itself is
// enforced in the service layer (deny by default); the declaration here is documentation and a
// guard against forgetting to think about it.
type Permission string

// PermPublic marks operations that need no authentication.
const PermPublic Permission = "public"

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
	huma.Register(a, op, h)
}
