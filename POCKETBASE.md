# PocketBase architecture

GolfTrack runs on **PocketBase**, consumed as a Go framework rather than a
downloaded prebuilt binary. `pocketbase/` is the entire application: one Go
module that builds a single static binary (`golftrack-pb`) embedding the
PocketBase server, the collection schema, the domain hooks, and the
server-rendered frontend.

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
