# Golf Track

A mobile-first golf score tracking web app.

> **Runs on PocketBase** (migrated from Django, then from Next.js before that —
> see `POCKETBASE.md` for the app's architecture and history). The app in
> `pocketbase/` is the entire repository now — see `pocketbase/README.md` for
> its layout and dev commands, and `DEPLOYMENT.md` for the pipeline.

Stack:
- PocketBase consumed as a Go framework — one binary embedding the server, schema, domain hooks and frontend
- Server-rendered Go `html/template` pages with Alpine.js islands
- Tailwind CSS (standalone CLI)
- SQLite + Litestream

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
make pb-css      # compile Tailwind into the binary's embedded stylesheet
make pb-dev      # serve the app
```

Open:

```text
http://127.0.0.1:8090
```

See `pocketbase/README.md` for the full command list (schema, auth setup, tests).

## Database

SQLite, managed by PocketBase in its data directory — `pocketbase/.local/` in
development, `/data` in the container. There is no `DATABASE_URL`: the directory
is passed as `--dir`, and the schema is reconciled to the binary's embedded
`pb_schema.json` at every startup.

## Tests

```bash
make pb-test     # go vet + the full Go suite
```

## Production / deployment

See:
- `DEPLOYMENT.md` — pipeline, cutover and rollback
- `pocketbase/DEPLOYMENT.md` — the container itself
- `pocketbase/.env.example`
- `pocketbase/Dockerfile`

Production setup:
- Docker container (one static Go binary, supervised by Litestream)
- persistent volume mounted at `/data`
- port 8090 in the container, published as 3000 on the host
