package api

import (
	"errors"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/catalog"
	"github.com/larueli/biblisann/internal/library"
)

// mapError turns service errors into RFC 9457 problems. Unknown errors are logged and answered
// with a generic 500: internals never reach the client.
func mapError(log *slog.Logger, err error) error {
	var weak auth.ErrWeakPassword
	switch {
	case err == nil:
		return nil
	case errors.Is(err, authz.ErrUnauthenticated):
		return huma.Error401Unauthorized("authentication required")
	case errors.Is(err, authz.ErrNotFound), errors.Is(err, catalog.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, authz.ErrForbidden):
		return huma.Error403Forbidden("forbidden")
	case errors.Is(err, library.ErrIdempotencyConflict):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, library.ErrInvalid), errors.Is(err, catalog.ErrInvalidISBN), errors.Is(err, catalog.ErrInvalidEdition),
		errors.Is(err, auth.ErrInvalidEmail):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.As(err, &weak):
		return huma.Error422UnprocessableEntity(weak.Error())
	case errors.Is(err, auth.ErrInvalidCredentials):
		return huma.Error401Unauthorized("invalid email or password")
	case errors.Is(err, auth.ErrRegistrationClosed):
		return huma.Error403Forbidden("registration is closed")
	case errors.Is(err, auth.ErrEmailTaken):
		return huma.Error409Conflict("email already registered")
	}
	log.Error("unhandled error", "error", err)
	return huma.Error500InternalServerError("internal error")
}
