package hooks

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// Error helpers that keep custom-route failures on the existing GolfTrack
// error contract.
//
// The Django API returns {"error": "<message>"} with 400/401/403/404/409 (see
// config/api.py), and the Next.js routes before it did the same. PocketBase's
// own errors are shaped {"data": {}, "message": ..., "status": ...}, so
// responses written by custom routes have to be built explicitly to match.
//
// Reconciling PocketBase's *built-in* validation errors with this shape is a
// Phase 5 (API parity) decision, not something these helpers do.

// writeError writes {"error": message} with the given status.
func writeError(e *core.RequestEvent, status int, message string) error {
	return e.JSON(status, map[string]string{"error": message})
}

// ValidationError — 400, invalid input (Django ValidationError).
func ValidationError(e *core.RequestEvent, message string) error {
	return writeError(e, http.StatusBadRequest, message)
}

// UnauthorizedError — 401, no authenticated user (Django UnauthorizedError).
func UnauthorizedError(e *core.RequestEvent, message string) error {
	if message == "" {
		message = "Unauthorized"
	}
	return writeError(e, http.StatusUnauthorized, message)
}

// ForbiddenError — 403, authenticated but not permitted (Django ForbiddenError).
func ForbiddenError(e *core.RequestEvent, message string) error {
	if message == "" {
		message = "Forbidden"
	}
	return writeError(e, http.StatusForbidden, message)
}

// NotFoundError — 404, record missing or not visible to the caller (Django
// NotFoundError).
func NotFoundError(e *core.RequestEvent, message string) error {
	if message == "" {
		message = "Not found"
	}
	return writeError(e, http.StatusNotFound, message)
}

// ConflictError — 409, request conflicts with current state (Django
// ConflictError).
func ConflictError(e *core.RequestEvent, message string) error {
	return writeError(e, http.StatusConflict, message)
}
