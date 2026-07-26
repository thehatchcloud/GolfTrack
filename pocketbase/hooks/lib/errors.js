/**
 * Error helpers that keep hook failures on the existing GolfTrack error
 * contract.
 *
 * The Django API returns `{"error": "<message>"}` with 400/401/403/404/409
 * (see `config/api.py`), and the Next.js routes before it did the same.
 * PocketBase's own errors are shaped `{"data": {}, "message": ..., "status": ...}`,
 * so anything a hook raises has to be built explicitly to match.
 *
 * Reconciling PocketBase's *built-in* validation errors with this shape is a
 * Phase 5 (API parity) decision, not something these helpers do.
 */

/**
 * @param {number} status HTTP status code
 * @param {string} message human-readable message
 * @returns {ApiError} ready to `throw`
 */
function apiError(status, message) {
    return new ApiError(status, message, { error: message });
}

/** 400 — invalid input (Django `ValidationError`). */
function validationError(message) {
    return apiError(400, message);
}

/** 401 — no authenticated user (Django `UnauthorizedError`). */
function unauthorizedError(message) {
    return apiError(401, message || "Unauthorized");
}

/** 403 — authenticated but not permitted (Django `ForbiddenError`). */
function forbiddenError(message) {
    return apiError(403, message || "Forbidden");
}

/** 404 — record missing or not visible to the caller (Django `NotFoundError`). */
function notFoundError(message) {
    return apiError(404, message || "Not found");
}

/** 409 — request conflicts with current state (Django `ConflictError`). */
function conflictError(message) {
    return apiError(409, message);
}

module.exports = {
    apiError,
    validationError,
    unauthorizedError,
    forbiddenError,
    notFoundError,
    conflictError,
};
