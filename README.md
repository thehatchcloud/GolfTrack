# Golf Track

A mobile-first golf score tracking web app.

> **Rewrite in progress (#85):** GolfTrack is being migrated to **Django + Django
> Ninja + Tailwind CSS**. The Django app is now what builds and deploys (see
> `DEPLOYMENT.md`); the legacy Next.js app remains in-tree until the Phase 8
> cutover (#94). See `DJANGO.md` for the Django app's layout and dev commands.

Stack (Django rewrite):
- Django + Django Ninja
- Tailwind CSS (standalone CLI)
- SQLite + Litestream
- HTMX + Alpine.js
- gunicorn + WhiteNoise

## Features

- create 9-hole and 18-hole courses
- define par for each hole
- start rounds from saved courses
- for 18-hole courses, choose:
  - full course
  - front 9
  - back 9
- track shots hole-by-hole
- record the club used for each shot
- undo, edit, and delete shots
- review and submit rounds with notes
- cancel in-progress rounds
- browse completed round history

## Local development

```bash
make install     # create .venv (Python 3.14) and install deps
make migrate     # apply migrations (creates db.sqlite3)
make dev         # runserver
```

Open:

```text
http://localhost:8000
```

See `DJANGO.md` for the full command list (CSS build, seeding, tests, lint).

## Database

Local development uses SQLite (`db.sqlite3`). In production, set:

```env
DATABASE_URL="file:/data/prod.db"
```

## Tests

```bash
make test        # pytest, verbose, with coverage
```

## Production / deployment

See:
- `DEPLOYMENT.md`
- `.env.example`
- `Dockerfile`

Recommended production setup:
- Docker container (gunicorn + WhiteNoise + Litestream)
- persistent volume mounted at `/data`
- `DATABASE_URL=file:/data/prod.db`
