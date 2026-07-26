// Package apierr keeps GolfTrack's HTTP error contract in one place.
//
// The Django API returns {"error": "<message>"} with 400/401/403/404/409 (see
// config/api.py), and the Next.js routes before it did the same. PocketBase's
// own errors are shaped {"data": {}, "message": ..., "status": ...}, so
// responses written by custom routes have to be built explicitly to match.
//
// Domain code raises these as ordinary Go errors — Conflict("Round is already
// completed") is the direct translation of Django's ConflictError — and the
// route wrapper turns whatever comes back into a response. That split is what
// lets a domain function be called from inside a transaction, where there is no
// RequestEvent to write to yet.
//
// Reconciling PocketBase's *built-in* validation errors with this shape is a
// Phase 5 (API parity, #126) decision, not something this package does.
package apierr

import (
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// Error is a failure with a status code from the GolfTrack contract.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

// Validation — 400, invalid input (Django ValidationError).
func Validation(message string) *Error {
	return &Error{Status: http.StatusBadRequest, Message: message}
}

// Unauthorized — 401, no authenticated user (Django UnauthorizedError).
func Unauthorized(message string) *Error {
	return &Error{Status: http.StatusUnauthorized, Message: orDefault(message, "Unauthorized")}
}

// Forbidden — 403, authenticated but not permitted (Django ForbiddenError).
func Forbidden(message string) *Error {
	return &Error{Status: http.StatusForbidden, Message: orDefault(message, "Forbidden")}
}

// NotFound — 404, record missing or not visible to the caller (Django
// NotFoundError).
func NotFound(message string) *Error {
	return &Error{Status: http.StatusNotFound, Message: orDefault(message, "Not found")}
}

// Conflict — 409, request conflicts with current state (Django ConflictError).
func Conflict(message string) *Error {
	return &Error{Status: http.StatusConflict, Message: message}
}

func orDefault(message, fallback string) string {
	if message == "" {
		return fallback
	}
	return message
}

// Write sends err on the {"error": "<message>"} contract.
//
// An *Error carries its own status. Anything else is a bug or an unexpected
// database failure: it is logged with its real message and reported as a bare
// 500, so an internal error never leaks through the API.
func Write(e *core.RequestEvent, err error) error {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return e.JSON(apiErr.Status, map[string]string{"error": apiErr.Message})
	}

	e.App.Logger().Error("GolfTrack route failed", "method", e.Request.Method, "url", e.Request.URL.Path, "error", err)

	return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Internal server error"})
}

// Handler adapts a domain route function to the router, writing any error it
// returns through Write. Route registrations use it so that no handler has to
// remember the contract.
func Handler(fn func(e *core.RequestEvent) error) func(e *core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if err := fn(e); err != nil {
			return Write(e, err)
		}
		return nil
	}
}
