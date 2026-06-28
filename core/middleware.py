import base64
import uuid

from django.conf import settings


def _generate_nonce():
    return base64.b64encode(uuid.uuid4().bytes).decode()


class CspMiddleware:
    """Add a per-request CSP nonce and security headers, mirroring proxy.ts withCsp()."""

    def __init__(self, get_response):
        self.get_response = get_response

    def __call__(self, request):
        nonce = _generate_nonce()
        request.csp_nonce = nonce

        response = self.get_response(request)

        is_dev = settings.DEBUG
        csp_parts = [
            "default-src 'self'",
            f"script-src 'self' 'nonce-{nonce}' 'strict-dynamic'"
            + (" 'unsafe-eval'" if is_dev else ""),
            "style-src 'self' 'unsafe-inline'",
            "img-src 'self' data:",
            "font-src 'self'",
            "connect-src 'self'",
            "object-src 'none'",
            "base-uri 'self'",
            "form-action 'self' https://accounts.google.com https://login.microsoftonline.com",
            "frame-ancestors 'none'",
        ]
        response["Content-Security-Policy"] = "; ".join(csp_parts)
        response["X-Content-Type-Options"] = "nosniff"
        response["Referrer-Policy"] = "strict-origin-when-cross-origin"
        response["Permissions-Policy"] = "geolocation=(), camera=(), microphone=()"

        return response


def csp_nonce(request):
    """Template context processor: exposes csp_nonce from the request."""
    return {"csp_nonce": getattr(request, "csp_nonce", "")}
