# PocketBase architecture

GolfTrack runs on **PocketBase**, consumed as a Go framework rather than a
downloaded prebuilt binary. `pocketbase/` is the entire application: one Go
module that builds a single static binary (`golftrack-pb`) embedding the
PocketBase server, the collection schema, the domain hooks, and the
server-rendered frontend.

This is the result of a three-stack history — **Next.js → Django →
PocketBase** — tracked end to end in epic #121 and its predecessor #85.
Phase 11 (#132) removed the Django and Next.js source from this repository;
their code lives on in git history and in the `nextjs-final` / `django-final`
tags (see "Historical stacks" below).

## Layout

```
pocketbase/
├── main.go               # entrypoint: schema sync at startup + hook registration
├── schema.go              # go:embed of pb_schema.json + the sync itself
├── pb_schema.json          # source of truth for the six collections (embedded)
├── internal/
│   ├── collections/       # collection names/ids, field names, enum values
│   ├── apierr/            # the {"error": …} contract + the route wrapper
│   ├── authenv/           # auth configuration, read from the environment
│   ├── records/           # record lookups the domain packages share
│   ├── hooks/              # business logic — one package per aggregate
│   └── web/                # the frontend: page routes, templates, embedded static assets
├── Dockerfile / litestream.yml / entrypoint.sh   # the container
├── ARCHITECTURE.md, API.md, AUTH.md, DEPLOYMENT.md, README.md
└── *_test.go               # schema, access rules, hooks, routes, concurrency, performance
```

See [`pocketbase/README.md`](pocketbase/README.md) for the full directory
breakdown and dev commands, [`pocketbase/ARCHITECTURE.md`](pocketbase/ARCHITECTURE.md)
for the hook and business-logic design, and [`pocketbase/API.md`](pocketbase/API.md)
for the endpoint contract.

## Domain rules

Preserved unchanged through every rewrite:

- **Course snapshotting** — a round's hole pars are copied from the course at
  round creation; later course edits never affect past rounds.
- **Stroke cache** — the shot count per hole is cached alongside the shots
  themselves and kept in sync by the same hook that writes them.
- **Shot numbering** — shots are sequentially numbered within a hole; deleting
  a mid-round shot renumbers the rest to close the gap.
- **One in-progress round per user**, enforced at the schema level.
- **Play modes** — 18-hole courses support `full`, `front9`, `back9`; 9-hole
  courses only support `full`.

## Schema changes are additive

**Add fields to `pocketbase/pb_schema.json`; do not remove them.** A field
dropped from that file takes its column and every value in it on the next
container restart, with no confirmation step and no migration to reverse.
Retire a field by making the Go code stop reading it, not by deleting it from
the schema. Full rationale and the deprecation checklist:
[`pocketbase/README.md`](pocketbase/README.md) § "Schema changes are additive
by default" and `CLAUDE.md` § "Schema changes are additive".

## Deployment

Docker, single container, SQLite on a persistent volume, replicated by
Litestream. See [`DEPLOYMENT.md`](DEPLOYMENT.md) for the pipeline and
[`pocketbase/DEPLOYMENT.md`](pocketbase/DEPLOYMENT.md) for the container
itself.

## Historical stacks

The migration plan, decisions, and phase-by-phase history are in
[`POCKETBASE_MIGRATION_PLAN.md`](POCKETBASE_MIGRATION_PLAN.md) — kept as a
historical record; nothing in it describes the current repository layout
anymore.

Two git tags mark the last commit of each retired stack, for reference or
disaster recovery:

- `nextjs-final` — the last commit before the Django rewrite began (#85),
  when `app/`, `components/`, `lib/`, and `prisma/` were the deployed app.
- `django-final` — the last commit before Phase 11 (#132) removed the Django
  source, when `config/`, `accounts/`, `courses/`, and `rounds/` were still
  in-tree (though no longer deployed — PocketBase had already taken over in
  Phase 10, #131).

The last Django container image is preserved in GHCR as
`ghcr.io/<owner>/golftrack:django-latest` for manual recovery; see
[`DEPLOYMENT.md`](DEPLOYMENT.md) § "Rollback to Django".
