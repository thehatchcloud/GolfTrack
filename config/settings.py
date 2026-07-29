"""
Django settings for GolfTrack.

Auth: django-allauth with Google + Microsoft OAuth (Phase 4, #90).
"""
import os
from pathlib import Path

from dotenv import load_dotenv

load_dotenv()  # loads .env (does not override vars already set in the environment)

BASE_DIR = Path(__file__).resolve().parent.parent


def _bool_env(name: str, default: bool = False) -> bool:
    return os.environ.get(name, str(default)).lower() in {"1", "true", "yes", "on"}


def _sqlite_path() -> str:
    """
    Resolve the SQLite file path.

    Mirrors the deployment contract from the Next.js app, which sets
    DATABASE_URL="file:/data/prod.db". We accept that ``file:`` form so the
    deploy workflow and docs can stay the same, and fall back to a local
    db.sqlite3 for development.
    """
    url = os.environ.get("DATABASE_URL")
    if url and url.startswith("file:"):
        return url[len("file:") :]
    if url:
        return url
    return str(BASE_DIR / "db.sqlite3")


SECRET_KEY = os.environ.get("DJANGO_SECRET_KEY", "dev-insecure-change-me")
DEBUG = _bool_env("DJANGO_DEBUG", default=True)

ALLOWED_HOSTS = [
    host.strip()
    for host in os.environ.get("DJANGO_ALLOWED_HOSTS", "localhost,127.0.0.1").split(",")
    if host.strip()
]

CSRF_TRUSTED_ORIGINS = [
    origin.strip()
    for origin in os.environ.get("DJANGO_CSRF_TRUSTED_ORIGINS", "").split(",")
    if origin.strip()
]

INSTALLED_APPS = [
    "django.contrib.admin",
    "django.contrib.auth",
    "django.contrib.contenttypes",
    "django.contrib.sessions",
    "django.contrib.messages",
    "django.contrib.staticfiles",
    "django.contrib.sites",
    # allauth
    "allauth",
    "allauth.account",
    "allauth.socialaccount",
    "allauth.socialaccount.providers.google",
    "allauth.socialaccount.providers.microsoft",
    # Local apps
    "accounts",
    "core",
    "courses",
    "rounds",
]

MIDDLEWARE = [
    "django.middleware.security.SecurityMiddleware",
    "whitenoise.middleware.WhiteNoiseMiddleware",
    "django.contrib.sessions.middleware.SessionMiddleware",
    "django.middleware.common.CommonMiddleware",
    "django.middleware.csrf.CsrfViewMiddleware",
    "django.contrib.auth.middleware.AuthenticationMiddleware",
    "allauth.account.middleware.AccountMiddleware",
    "django.contrib.messages.middleware.MessageMiddleware",
    "django.middleware.clickjacking.XFrameOptionsMiddleware",
    "core.middleware.CspMiddleware",
]

ROOT_URLCONF = "config.urls"

TEMPLATES = [
    {
        "BACKEND": "django.template.backends.django.DjangoTemplates",
        "DIRS": [BASE_DIR / "templates"],
        "APP_DIRS": True,
        "OPTIONS": {
            "context_processors": [
                "django.template.context_processors.debug",
                "django.template.context_processors.request",
                "django.contrib.auth.context_processors.auth",
                "django.contrib.messages.context_processors.messages",
                "core.middleware.csp_nonce",
                "core.context_processors.auth_options",
                "core.context_processors.app_version",
            ],
        },
    },
]

WSGI_APPLICATION = "config.wsgi.application"
ASGI_APPLICATION = "config.asgi.application"

DATABASES = {
    "default": {
        "ENGINE": "django.db.backends.sqlite3",
        "NAME": _sqlite_path(),
        "OPTIONS": {
            # Next.js used Prisma's Serializable isolation level for
            # concurrency-sensitive writes (shot add/undo/delete, round
            # create). SQLite's default BEGIN DEFERRED only takes a write
            # lock at the first write statement, so two transactions can
            # both read stale state before either writes — a lost-update
            # race. BEGIN IMMEDIATE takes the write lock at transaction
            # start, serializing concurrent writers the same way. See #85's
            # "Serializable transactions" risk call-out.
            "transaction_mode": "IMMEDIATE",
            "timeout": 10,
        },
        "TEST": {
            # A real file, not sqlite's default in-memory test DB, so
            # multiple connections (e.g. threads in concurrency tests) see
            # the same data — mirrors the Next.js suite running against a
            # real SQLite file (tests/helpers/test-context.ts).
            "NAME": str(BASE_DIR / "test.db"),
        },
    }
}

AUTH_PASSWORD_VALIDATORS = [
    {"NAME": "django.contrib.auth.password_validation.UserAttributeSimilarityValidator"},
    {"NAME": "django.contrib.auth.password_validation.MinimumLengthValidator"},
    {"NAME": "django.contrib.auth.password_validation.CommonPasswordValidator"},
    {"NAME": "django.contrib.auth.password_validation.NumericPasswordValidator"},
]

LANGUAGE_CODE = "en-us"
TIME_ZONE = "UTC"
USE_I18N = True
USE_TZ = True

STATIC_URL = "static/"
STATIC_ROOT = BASE_DIR / "staticfiles"
STATICFILES_DIRS = [BASE_DIR / "static"]

# Hashed-manifest static storage only in production (requires `collectstatic`).
# Dev and tests use the plain finder-backed storage so templates render without
# a prior collectstatic run.
_staticfiles_backend = (
    "django.contrib.staticfiles.storage.StaticFilesStorage"
    if DEBUG
    else "whitenoise.storage.CompressedManifestStaticFilesStorage"
)
STORAGES = {
    "default": {"BACKEND": "django.core.files.storage.FileSystemStorage"},
    "staticfiles": {"BACKEND": _staticfiles_backend},
}

DEFAULT_AUTO_FIELD = "django.db.models.BigAutoField"

AUTH_USER_MODEL = "accounts.User"

# Behind exe.dev's TLS-terminating proxy in production.
SECURE_PROXY_SSL_HEADER = ("HTTP_X_FORWARDED_PROTO", "https")

# django.contrib.sites — required by allauth
SITE_ID = 1

AUTHENTICATION_BACKENDS = [
    "django.contrib.auth.backends.ModelBackend",
    "allauth.account.auth_backends.AuthenticationBackend",
]

# allauth core
LOGIN_URL = "/accounts/login/"
LOGIN_REDIRECT_URL = "/"
LOGOUT_REDIRECT_URL = "/"

# Environments without OAuth apps registered (e.g. the dev server, per
# deploy-dev.yml) can opt into plain email+password login for pre-created
# accounts. Self-service signup stays disabled either way — see
# AccountAdapter.is_open_for_signup.
ALLOW_PASSWORD_LOGIN = _bool_env("DJANGO_ALLOW_PASSWORD_LOGIN", default=False)
SOCIALACCOUNT_ONLY = not ALLOW_PASSWORD_LOGIN
ACCOUNT_FORMS = {"login": "accounts.forms.StyledLoginForm"}

# allauth's LoginForm only renders a password field when the signup form would
# also collect one (it assumes login and signup password-ness match), even
# though self-service signup is disabled here regardless of this setting.
ACCOUNT_SIGNUP_FIELDS = ["email*", "password1*"] if ALLOW_PASSWORD_LOGIN else ["email*"]
ACCOUNT_LOGIN_METHODS = {"email"}
ACCOUNT_EMAIL_VERIFICATION = "none"
ACCOUNT_DEFAULT_HTTP_PROTOCOL = "http" if DEBUG else "https"
ACCOUNT_ADAPTER = "accounts.adapters.AccountAdapter"
SOCIALACCOUNT_ADAPTER = "accounts.adapters.SocialAccountAdapter"
SOCIALACCOUNT_AUTO_SIGNUP = True
SOCIALACCOUNT_STORE_TOKENS = False

def _admin_emails() -> set[str]:
    return {
        e.strip().lower()
        for e in os.environ.get("ADMIN_EMAILS", "").split(",")
        if e.strip()
    }

ADMIN_EMAILS = _admin_emails()

SOCIALACCOUNT_PROVIDERS = {
    "google": {
        "APP": {
            "client_id": os.environ.get("GOOGLE_CLIENT_ID", ""),
            "secret": os.environ.get("GOOGLE_CLIENT_SECRET", ""),
        },
        "SCOPE": ["openid", "profile", "email"],
        "AUTH_PARAMS": {"access_type": "online"},
        "FETCH_USERINFO": True,
    },
    "microsoft": {
        "APP": {
            "client_id": os.environ.get("MICROSOFT_CLIENT_ID", ""),
            "secret": os.environ.get("MICROSOFT_CLIENT_SECRET", ""),
        },
        "TENANT": "common",
        # "User.Read" is the Graph API delegated permission the adapter needs
        # to call https://graph.microsoft.com/v1.0/me after token exchange —
        # without it that call 401s and sign-in lands on the error page.
        "SCOPE": ["openid", "profile", "email", "User.Read"],
        "AUTH_PARAMS": {"prompt": "select_account"},
    },
}
