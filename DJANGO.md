# Django rewrite (in progress)

GolfTrack is being rewritten from Next.js onto **Django + Django Ninja + Tailwind CSS**.
Tracking issue: **#85** (phases #86–#94). Until the Phase 8 cutover, the Django app
lives alongside the existing Next.js app in this repo.

## Stack

Django 6.0 (Python 3.14) · Django Ninja · Tailwind CSS (standalone CLI) · SQLite · WhiteNoise · Gunicorn/Uvicorn.
Front-end interactivity: HTMX + Alpine.js against the Ninja JSON API. Auth (later): django-allauth.

## Layout

| Path | Purpose |
|------|---------|
| `config/` | Django project: `settings.py`, `urls.py`, `api.py` (NinjaAPI), `wsgi.py`/`asgi.py` |
| `accounts/` | Custom `User` model (`AUTH_USER_MODEL`) with USER/ADMIN `role` |
| `core/` | Shared/site app — home view, health |
| `courses/` | `Course` + `CourseHole` models |
| `rounds/` | `Round` + `RoundHole` + `Shot` models |
| `templates/` | Project-level Django templates (`base.html`, `home.html`) |
| `static/src/input.css` | Tailwind entry; compiles to `static/css/app.css` |
| `tests/` | pytest suite |
| `pyproject.toml` | Python deps + `ruff` / `pytest` config |
| `bin/build-css.sh` | Downloads the Tailwind standalone CLI and builds the stylesheet |

The REST API is mounted at `/api/` so paths match the existing contract (`GET /api/health` today).

**Domain model (Phase 1):** `Course` 1–N `CourseHole`; `Round` (belongs to a `User` + `Course`) 1–N `RoundHole` 1–N `Shot`. `RoundHole.par` is snapshotted at round creation and `RoundHole.strokes` caches the shot count (both maintained by the service layer in Phase 2). A partial unique index enforces one in-progress round per user.

## Dev commands

```bash
uv venv --python 3.14 .venv && source .venv/bin/activate
uv pip install "django>=6.0,<6.1" django-ninja whitenoise gunicorn uvicorn ruff pytest pytest-django

python manage.py migrate
python manage.py runserver        # http://localhost:8000  (and /api/health)

./bin/build-css.sh                # compile Tailwind -> static/css/app.css

ruff check .                      # lint
pytest -q                         # tests

DJANGO_DEBUG=false python manage.py collectstatic --noinput   # production static build
```

## Environment variables (Django)

| Variable | Purpose |
|----------|---------|
| `DJANGO_SECRET_KEY` | Django secret (required in production) |
| `DJANGO_DEBUG` | `true`/`false` (default `true` in dev) |
| `DJANGO_ALLOWED_HOSTS` | Comma-separated hostnames |
| `DJANGO_CSRF_TRUSTED_ORIGINS` | Comma-separated origins for CSRF |
| `DATABASE_URL` | SQLite path; accepts the `file:/data/prod.db` form for deploy parity |

Auth-related vars (`ADMIN_EMAILS`, OAuth credentials) are wired in Phase 4.
