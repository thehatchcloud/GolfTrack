"""Auth helpers for the Ninja API.

Phase 3 authenticates via the Django session (``request.user``); django-allauth
wires the OAuth providers in Phase 4. These helpers raise exceptions that the
API's exception handlers map to 401/403 with the ``{"error": ...}`` body shape
the existing Next.js contract uses.
"""
from core.exceptions import ForbiddenError, UnauthorizedError


def require_user(request):
    user = getattr(request, "user", None)
    if user is None or not user.is_authenticated:
        raise UnauthorizedError("Unauthorized")
    return user


def require_admin(request):
    user = require_user(request)
    if not user.is_admin:
        raise ForbiddenError("Forbidden")
    return user
