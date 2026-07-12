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

The REST API is mounted at `/api/` so paths match the existing contract. Phase 3
implements the full surface in Django Ninja: courses (`GET/POST /api/courses`,
`GET/PUT /api/courses/{id}`), rounds (`GET/POST /api/rounds`, `GET /api/rounds/{id}`,
`GET /api/rounds/in-progress`, `POST .../complete`, `.../cancel`,
`PATCH .../current-hole`), shots (`POST/PATCH/DELETE .../holes/{n}/shots[/{id}]`,
`POST .../undo`), and `GET /api/health`. Routers live in `courses/api.py` and
`rounds/api.py`; request/response schemas serialize to camelCase to match the
old contract. Service-layer exceptions map to 400/401/403/404/409 in
`config/api.py`.

**Domain model (Phase 1):** `Course` 1–N `CourseHole`; `Round` (belongs to a `User` + `Course`) 1–N `RoundHole` 1–N `Shot`. `RoundHole.par` is snapshotted at round creation and `RoundHole.strokes` caches the shot count (both maintained by the service layer in Phase 2). A partial unique index enforces one in-progress round per user.

## Dev commands

```bash
uv venv --python 3.14 .venv && source .venv/bin/activate
uv pip install "django>=6.0,<6.1" django-ninja whitenoise gunicorn uvicorn ruff pytest pytest-django

python manage.py migrate
python manage.py runserver        # http://localhost:8000  (and /api/health)
python manage.py seed             # wipe + reseed with sample data (dev/test DBs only)

./bin/build-css.sh                # compile Tailwind -> static/css/app.css

ruff check .                      # lint
pytest -q                         # tests

DJANGO_DEBUG=false python manage.py collectstatic --noinput   # production static build
```

## Testing (Phase 6, #92)

pytest + pytest-django, run against a **real SQLite file** (`test.db`, see the
`TEST.NAME` override in `config/settings.py`) rather than sqlite's default
in-memory test DB — this matches the Next.js suite's philosophy of testing
against a real database, and lets concurrency tests open independent
connections from separate threads that see the same data.

| File | Covers |
|------|--------|
| `tests/test_models.py` | Model constraints (unique indexes, cascade/protect deletes) |
| `tests/test_services.py` | Service-layer logic — scoring, round lifecycle, courses |
| `tests/test_validation.py` | Ninja/Pydantic schema validation edge cases (course holes, par range, note/club length, play mode) |
| `tests/test_api.py` | Every `/api/...` route, including the 400/401/403/404/409 contract |
| `tests/test_concurrency.py` | Real-thread races (concurrent shot add/undo/delete, concurrent round creation) |
| `tests/test_views.py` | Server-rendered course/round pages |
| `tests/test_auth.py` | Login page + password-login opt-in |
| `tests/test_seed.py` | `seed` management command + its guard rules |
| `tests/test_health.py` | `/api/health`, home page |

**SQLite transaction mode:** the Next.js app used Prisma's Serializable
isolation for concurrency-sensitive writes (shot add/undo/delete, round
create). SQLite's default `BEGIN DEFERRED` only takes a write lock at the
first write statement, so two transactions can both read stale state before
either writes (a lost-update race). `config/settings.py` sets
`OPTIONS.transaction_mode = "IMMEDIATE"` so `transaction.atomic()` takes the
write lock up front instead, serializing concurrent writers the way the old
app did. `tests/test_concurrency.py` exercises this directly with real
threads; don't remove that setting without re-verifying those tests.

**Seeding:** `python manage.py seed` ports `prisma/seed.ts` — same guard
rules (refuses to run unless `DEBUG` is on and the resolved DB path looks
like a local dev/test database) and the same sample data (a test user, a
9-hole and an 18-hole course, one completed and one in-progress round).

## Environment variables (Django)

| Variable | Purpose |
|----------|---------|
| `DJANGO_SECRET_KEY` | Django secret (required in production) |
| `DJANGO_DEBUG` | `true`/`false` (default `true` in dev) |
| `DJANGO_ALLOWED_HOSTS` | Comma-separated hostnames |
| `DJANGO_CSRF_TRUSTED_ORIGINS` | Comma-separated origins for CSRF |
| `DATABASE_URL` | SQLite path; accepts the `file:/data/prod.db` form for deploy parity |
| `ADMIN_EMAILS` | Comma-separated emails granted `role=ADMIN` on every sign-in |
| `GOOGLE_CLIENT_ID` | Google OAuth client ID (Django's own OAuth app) |
| `GOOGLE_CLIENT_SECRET` | Google OAuth client secret (Django's own OAuth app) |
| `MICROSOFT_CLIENT_ID` | Microsoft Entra ID client ID (Django's own OAuth app) |
| `MICROSOFT_CLIENT_SECRET` | Microsoft Entra ID client secret (Django's own OAuth app) |
| `DJANGO_ALLOW_PASSWORD_LOGIN` | `true`/`false` (default `false`). Opt into a plain email+password login form for environments without OAuth apps registered (e.g. the dev server) — see `deploy-dev.yml`. Self-service signup stays disabled either way; only pre-created accounts (`docker exec ... manage.py shell`) can sign in this way. |

Django uses **its own OAuth client apps**, separate from the Next.js `AUTH_*` credentials. Providers validate redirect URIs per-client, so giving Django dedicated apps lets its `/accounts/.../login/callback/` URIs be registered without modifying the Next.js OAuth apps.

A `.env` file in the project root is loaded automatically via `python-dotenv` (does not override variables already set in the shell environment).

OAuth redirect URIs — both must be registered in each provider's console during the coexistence period:
- **Next.js** — Google: `{origin}/api/auth/callback/google` · Microsoft: `{origin}/api/auth/callback/microsoft-entra-id`
- **Django** — Google: `{origin}/accounts/google/login/callback/` · Microsoft: `{origin}/accounts/microsoft/login/callback/`

> **Phase 8 note:** At cutover the Next.js redirect URIs can be removed from the provider apps.
