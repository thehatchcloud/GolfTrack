"""Django Ninja API instance.

The API is mounted at /api in config/urls.py so the REST paths match the
existing Next.js contract. Service-layer exceptions are mapped to the same
status codes and ``{"error": ...}`` body shape the Next.js routes returned
(see lib/errors.ts).
"""
from ninja import NinjaAPI
from ninja.errors import ValidationError as NinjaValidationError

from core.exceptions import (
    ConflictError,
    ForbiddenError,
    NotFoundError,
    UnauthorizedError,
    ValidationError,
)
from core.version import get_version
from courses.api import router as courses_router
from rounds.api import router as rounds_router

api = NinjaAPI(title="GolfTrack API", version="0.1.0")

api.add_router("/courses", courses_router)
api.add_router("/rounds", rounds_router)


@api.get("/health")
def health(request):
    return {"status": "ok", "version": get_version()}


def _error(request, message: str, status: int):
    return api.create_response(request, {"error": message}, status=status)


@api.exception_handler(NotFoundError)
def on_not_found(request, exc):
    return _error(request, str(exc), 404)


@api.exception_handler(ConflictError)
def on_conflict(request, exc):
    return _error(request, str(exc), 409)


@api.exception_handler(ValidationError)
def on_validation(request, exc):
    return _error(request, str(exc), 400)


@api.exception_handler(UnauthorizedError)
def on_unauthorized(request, exc):
    return _error(request, str(exc), 401)


@api.exception_handler(ForbiddenError)
def on_forbidden(request, exc):
    return _error(request, str(exc), 403)


@api.exception_handler(NinjaValidationError)
def on_ninja_validation(request, exc):
    # Mirror the Next.js contract: request validation failures are 400 with the
    # first error message (Zod returned issues[0].message).
    first = exc.errors[0] if exc.errors else {}
    message = first.get("msg", "Invalid input")
    # Pydantic prefixes custom ValueError messages with "Value error, ".
    message = message.removeprefix("Value error, ")
    return _error(request, message, 400)
