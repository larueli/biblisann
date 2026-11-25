package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/catalog"
	"github.com/larueli/biblisann/internal/importer"
	"github.com/larueli/biblisann/internal/library"
	"github.com/larueli/biblisann/internal/lists"
	"github.com/larueli/biblisann/internal/loans"
	"github.com/larueli/biblisann/internal/notify"
	"github.com/larueli/biblisann/internal/reading"
	"github.com/larueli/biblisann/internal/tokens"
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
	case errors.Is(err, library.ErrConflict), errors.Is(err, library.ErrLastAdmin), errors.Is(err, loans.ErrAlreadyLent),
		errors.Is(err, loans.ErrAlreadyReturn):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, notify.ErrTooMany):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, tokens.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, library.ErrIdempotencyConflict):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, library.ErrInvalid), errors.Is(err, reading.ErrInvalid), errors.Is(err, lists.ErrInvalid),
		errors.Is(err, loans.ErrInvalid), errors.Is(err, importer.ErrFormat), errors.Is(err, tokens.ErrInvalid), errors.Is(err, catalog.ErrInvalidISBN), errors.Is(err, catalog.ErrInvalidEdition),
		errors.Is(err, auth.ErrInvalidEmail):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.As(err, &weak):
		return huma.Error422UnprocessableEntity(weak.Error())
	case errors.Is(err, catalog.ErrSyncExcluded), errors.Is(err, catalog.ErrInUse):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, catalog.ErrProvidersUnavailable):
		return huma.NewError(http.StatusBadGateway, err.Error())
	case errors.Is(err, auth.ErrNoPassword):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, auth.ErrInvalidCode), errors.Is(err, auth.ErrInvalidToken):
		return huma.Error401Unauthorized("invalid or expired code")
	case errors.Is(err, auth.ErrTooManyTries):
		return huma.NewError(http.StatusTooManyRequests, "too many attempts")
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
