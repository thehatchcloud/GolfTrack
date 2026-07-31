# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working in this repository.

GolfTrack runs on **PocketBase**, consumed as a Go framework: one binary
(`pocketbase/`) embeds the server, the collection schema, the domain hooks,
and the server-rendered frontend. It is the only app in this repository.

Previously the app was rewritten from Next.js to Django (#85), then from
Django to PocketBase (#121); both were removed in Phase 11 (#132) once
production had proven stable on PocketBase. See
[`POCKETBASE.md`](POCKETBASE.md) for the architecture and that history,
[`POCKETBASE_MIGRATION_PLAN.md`](POCKETBASE_MIGRATION_PLAN.md) for the
original migration plan (historical record), and
[`pocketbase/README.md`](pocketbase/README.md) for the full layout and dev
command reference.

## Workflow

- **All changes must happen on a feature branch.** Never commit directly to `main`. Before starting work, create a new branch (`git checkout -b <descriptive-name>`).
- **All merges to `main` go through a pull request.** Direct pushes to `main` are blocked by branch protection — including for repository admins. Open a PR with `gh pr create` and merge it from there.
- **Never merge a PR.** Claude may open PRs and push updates to them, but only the repository owner may merge. Do not run `gh pr merge` under any circumstances.
- Branch names should be short and descriptive (e.g., `fix/addshot-race`, `ci/run-on-prs`, `feat/auth-middleware`).

## Development commands

```bash
make pb-dev       # serve the app at http://127.0.0.1:8090
make pb-css       # compile Tailwind into the binary's embedded stylesheet
make pb-test      # go vet + the full Go test suite
make pb-bench     # benchmark the PocketBase hot paths (not part of pb-test)
make pb-loadtest  # load-test at 10/50/100 concurrent players (slow; not part of pb-test)
```

Run a single Go test from `pocketbase/`:

```bash
cd pocketbase && go test -run TestName ./...
```

See [`pocketbase/README.md`](pocketbase/README.md) for the full command list, including `pocketbase/scripts/dev.sh` and the schema apply/export/verify scripts.

## Stack

PocketBase (Go framework) · Go `html/template` + Alpine.js islands · Tailwind CSS · SQLite · Litestream

## Architecture

Everything lives under `pocketbase/`:

| Path | Responsibility |
|------|---------------|
| `main.go` / `schema.go` | Entrypoint, startup schema sync, hook registration |
| `pb_schema.json` | Source of truth for the six collections (embedded via `go:embed`) |
| `internal/collections/` | Collection names/ids, field names, enum values |
| `internal/apierr/` | The `{"error": …}` contract + the route wrapper |
| `internal/authenv/` | Auth configuration, read from the environment |
| `internal/records/` | Record lookups the domain packages share |
| `internal/hooks/` | Business logic — one package per aggregate (round lifecycle, shots, scoring, courses) |
| `internal/web/` | The frontend — page routes, templates, embedded static assets, the auth cookie |

Full breakdown: [`pocketbase/README.md`](pocketbase/README.md). Hook/business-logic design: [`pocketbase/ARCHITECTURE.md`](pocketbase/ARCHITECTURE.md). API contract: [`pocketbase/API.md`](pocketbase/API.md). Auth: [`pocketbase/AUTH.md`](pocketbase/AUTH.md).

## Schema changes are additive

**Add fields to `pocketbase/pb_schema.json`; do not remove them.** A field dropped from that file takes its column and every value in it on the next container restart, with no confirmation step and no migration to reverse — it is the one edit in this repository that silently destroys production data. Keeping a column nobody reads is close to free; removing one is not.

Retire a field by **stopping the Go code from reading it**, not by deleting it from the schema. Leave it in `pb_schema.json`, make sure it is not `required` (or every future create still has to supply it), and note the deprecation and its date. Deleting it is a separate, later decision that needs a compelling technical reason of its own — a genuine conflict, a security problem, or a storage cost that actually matters. Treat renames the same way: PocketBase matches fields by id, so a rename is a drop plus an add unless the id is preserved. Add the new field, backfill, leave the old one.

The startup sync does not protect you here. `ImportCollectionsByMarshaledJSON(schemaJSON, false)` passes `deleteMissing=false`, so a whole *collection* missing from the file is left alone — but the fields inside a collection the file does list are reconciled to exactly what it lists. Nothing in the deploy path warns about a dropped field. Details and the deprecation checklist: `pocketbase/README.md` § "Schema changes are additive by default".

## Key domain rules

- **Course snapshotting:** When a round is created, par values are copied from the course into the round's holes. Course edits never affect past rounds.
- **Stroke cache:** Each hole's shot count is cached and kept in sync with shot creation/deletion by the same hook.
- **Shot numbering:** Shots have a sequential number within a hole. Deleting a mid-round shot renumbers all subsequent shots to keep the sequence gap-free.
- **Undo semantics:** Removes only the last shot (highest shot number) on the current hole.
- **Cancel:** Deletes the round and all related rows.
- **One in-progress round per user**, enforced at the schema level.
- **Play modes:** 18-hole courses support `full`, `front9`, or `back9`; 9-hole courses only support `full`.

## Testing

The Go test suite (`pocketbase/*_test.go`) runs against a real PocketBase app instance built in-process — no mocking. `make pb-test` runs `go vet` and the full suite; `make pb-bench` and `make pb-loadtest` are separate, slower suites not included in `pb-test`. See [`pocketbase/README.md`](pocketbase/README.md) for what each test file covers.

### Notify the requester whenever tests change

**Any time tests are added, modified, or removed, Claude must explicitly notify the requester** — call it out in the chat reply and in the PR description, don't let it ride silently inside a larger diff. The notification must state **what changed** (which test files/cases) and **why** (the behavior now covered, the weakness being fixed, or the reason for a deletion/weakened assertion). This applies even when the test change is incidental to a feature or bug fix.

Reliable tests are our primary defense against automated tools and humans introducing breaking changes, so a weakened, deleted, or skipped test is a material change that the requester must have the chance to review — never quietly loosen an assertion, add a skip/`xfail`, or delete a test to make a suite pass.

### Testing responsibilities

**Claude runs these automatically** before opening or updating a PR — no user action needed:

- `make pb-test` — `go vet` + the full Go test suite

**You (the user) must run these** — Claude has no access to Docker or the production server:

- `docker build` / `docker run` — verifying the container image builds and starts correctly
- Checking that the correct OS user runs inside the container (`docker exec <container> whoami`)
- Any test that requires a live Litestream connection (S3 restore, WAL replication)
- Smoke-testing the deployed app on the production server after a deploy

When Claude writes a PR test plan, items in the first group are ones Claude should have already verified. Items in the second group are explicitly for you to check before merging.

## Deployment

Docker-based, single container, SQLite on a persistent volume mounted at `/data`, replicated by [Litestream](https://litestream.io) to an S3-compatible bucket (DO Spaces in production). `golftrack-pb` listens on port 8090 inside the container; production maps host 3000 → 8090 so the public URL stays `…:3000`, and dev maps 8000 → 8090.

```bash
mkdir -p pb-data
docker build -t golftrack-pb pocketbase/
docker run --rm -p 8090:8090 \
  -e GOLFTRACK_ALLOW_PASSWORD_LOGIN=true \
  -e ADMIN_EMAILS="you@example.com" \
  -v "$(pwd)/pb-data:/data" \
  golftrack-pb
```

Without `LITESTREAM_BUCKET` set, the container skips replication and runs the app standalone — that's the path the command above uses.

### Container startup contract

`pocketbase/entrypoint.sh` is the container's `CMD`. It is structured so that **`litestream replicate -exec` only ever wraps a single executable, never a shell pipeline.** Litestream v0.5's `-exec` does not invoke `sh -c`, so chaining commands with `&&` inside `-exec` would silently pass them as literal arguments to the first binary. There is nothing to chain here — `golftrack-pb serve` reconciles the database to its embedded `pb_schema.json` inside its own startup, so no migration step precedes it. Keep it that way when modifying the entrypoint.

Two SQLite databases are replicated, not one: `data.db` (the collections) and `auxiliary.db` (logs).

If you change the S3-compatible backend (e.g. away from DO Spaces), revisit `pocketbase/litestream.yml` — `force-path-style` is provider-specific (DO Spaces rejects it; B2 requires it). The `LITESTREAM_ENDPOINT` secret must be the region root, not the full bucket URL. Details in `DEPLOYMENT.md`.

The deploy scripts health-check `/api/version` (not `/api/health`, which carries no build identity) and fail the deploy unless the container reports the deploying commit's short SHA.

Full deployment pipeline, cutover history, and the manual Django rollback path: [`DEPLOYMENT.md`](DEPLOYMENT.md). The container itself: [`pocketbase/DEPLOYMENT.md`](pocketbase/DEPLOYMENT.md).
