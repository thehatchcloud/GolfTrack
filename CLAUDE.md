# CLAUDE.md

> **Rewrite in progress (#85):** GolfTrack is being migrated to **Django + Django Ninja + Tailwind CSS**. The Django app currently lives alongside this Next.js app — see `DJANGO.md` for its layout and dev commands. The guidance below describes the existing Next.js app, which remains authoritative until the Phase 8 cutover.

> **PocketBase migration (#121) — cut over as of Phase 10 (#131):** the app in `pocketbase/` is now **the deployed artifact**. CI builds `pocketbase/Dockerfile` from the `pocketbase/` context and pushes it to `ghcr.io/<owner>/golftrack:latest`; `bin/deploy-prod.sh` and `bin/deploy-dev.sh` run it as the `golftrack-pb` container and remove the Django one. See `pocketbase/README.md` for the app, `pocketbase/DEPLOYMENT.md` for the container, and `DEPLOYMENT.md` for the pipeline, the cutover runbook and the rollback (`bin/rollback-prod.sh`, against the preserved `:django-latest` tag).
>
> Its tests are `make pb-test`; its Tailwind build is `make pb-css`; `make pb-dev` runs it locally. `make pb-bench` and `make pb-loadtest` are separate from `pb-test` and written up in `pocketbase/performance_report.md`.
>
> **The Django app is no longer deployed anywhere.** Its code, tests and CI job stay in-tree only until Phase 11 (#132) removes them, so that the rollback path keeps working. Everything below this line describes the Next.js app, which has been dead since #94.

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **Important:** The installed Next.js version has breaking API changes from earlier releases. Before writing any Next.js-specific code, check the relevant guide in `node_modules/next/dist/docs/`.

## Workflow

- **All changes must happen on a feature branch.** Never commit directly to `main`. Before starting work, create a new branch (`git checkout -b <descriptive-name>`).
- **All merges to `main` go through a pull request.** Direct pushes to `main` are blocked by branch protection — including for repository admins. Open a PR with `gh pr create` and merge it from there.
- **Never merge a PR.** Claude may open PRs and push updates to them, but only the repository owner may merge. Do not run `gh pr merge` under any circumstances.
- Branch names should be short and descriptive (e.g., `fix/addshot-race`, `ci/run-on-prs`, `feat/auth-middleware`).

## Development commands

```bash
npm run dev          # start Next.js dev server at localhost:3000
npm test             # run all integration tests
npm run lint         # ESLint
npm run build        # production build
npm start            # serve production build
```

Run a single test file:
```bash
npx tsx --test --test-concurrency=1 tests/rounds.test.ts
```

Prisma / database:
```bash
npm run db:migrate   # create and apply a new migration (dev)
npm run db:deploy    # apply existing migrations (production)
npm run db:generate  # regenerate Prisma client after schema changes
npm run db:seed      # seed the database with sample data
```

## Stack

Next.js · TypeScript · Tailwind CSS v4 · Prisma ORM · SQLite

## Architecture

### Service layer (`lib/`)

All database access and business logic lives here; route handlers are thin wrappers.

| File | Responsibility |
|------|---------------|
| `db.ts` | Prisma client singleton |
| `courses.ts` | CRUD for Course and CourseHole |
| `rounds.ts` | Full round lifecycle: create, addShot, undoLastShot, deleteShot, updateShot, setCurrentHole, completeRound, cancelRound |
| `scoring.ts` | `calculateRoundTotals` — sums par, strokes, relative-to-par |
| `validation.ts` | Zod schemas for all API inputs |
| `round-play.ts` | `getCurrentHolePosition` — maps hole list + currentHole to prev/next navigation |
| `clubs.ts` | Default club list |
| `api-auth.ts` | `getRequestToken` / `requireAdmin` — decrypt the Auth.js session cookie in route handlers and gate admin-only endpoints (401/403) |

### API routes (`app/api/`)

REST endpoints under `app/api/` call into `lib/` functions. Route structure mirrors the spec.

### Pages (`app/`)

Next.js App Router pages. Key routes:
- `/` — home with in-progress round resume
- `/courses`, `/courses/new`, `/courses/[id]`, `/courses/[id]/edit`
- `/rounds`, `/rounds/new`, `/rounds/[id]`, `/rounds/[id]/play`, `/rounds/[id]/review`

### Components (`components/`)

UI building blocks used across pages: `club-pad.tsx`, `shot-list.tsx`, `hole-header.tsx`, `round-summary-bar.tsx`, `live-round-controls.tsx`, `review-round-form.tsx`, `start-round-form.tsx`, `course-form.tsx`, `app-shell.tsx`, etc.

## Key domain rules

- **Course snapshotting:** When a round is created, par values are copied from `CourseHole` into `RoundHole`. Course edits never affect past rounds.
- **Stroke cache:** `RoundHole.strokes` is maintained as a cached count equal to the number of shots. Updated in the same transaction as shot insert/delete.
- **Shot numbering:** Shots have a sequential `shotNumber` within a hole. Deleting a mid-round shot renumbers all subsequent shots to keep the sequence gap-free.
- **Undo semantics:** Removes only the last shot (highest `shotNumber`) on the current hole.
- **Cancel:** Deletes the round and all related rows (cascading foreign keys).
- **Play modes:** 18-hole courses support `full`, `front9`, or `back9`; 9-hole courses only support `full`.

## Testing

Tests use Node's built-in test runner via `tsx --test`. They run against a **real SQLite database** (`prisma/test.db`) created fresh via `prisma db push` at test start — no mocking. Concurrency is set to 1 to avoid DB conflicts.

`tests/helpers/test-context.ts` exports `setupTestContext()` which provisions the test DB, returns `{ db, courses, rounds, resetDatabase, teardown }`, and cleans up after each suite.

### Notify the requester whenever tests change

**Any time tests are added, modified, or removed, Claude must explicitly notify the requester** — call it out in the chat reply and in the PR description, don't let it ride silently inside a larger diff. The notification must state **what changed** (which test files/cases) and **why** (the behavior now covered, the weakness being fixed, or the reason for a deletion/weakened assertion). This applies even when the test change is incidental to a feature or bug fix.

Reliable tests are our primary defense against automated tools and humans introducing breaking changes, so a weakened, deleted, or skipped test is a material change that the requester must have the chance to review — never quietly loosen an assertion, add a skip/`xfail`, or delete a test to make a suite pass.

### Testing responsibilities

**Claude runs these automatically** before opening or updating a PR — no user action needed:

- `npm run lint` — ESLint
- `npm run build` — TypeScript compilation + Next.js build
- `npm test` — full integration test suite against a local SQLite test DB

**You (the user) must run these** — Claude has no access to Docker or the production server:

- `docker build` / `docker run` — verifying the container image builds and starts correctly
- Checking that the correct OS user runs inside the container (`docker exec <container> whoami`)
- Any test that requires a live Litestream connection (S3 restore, WAL replication)
- Smoke-testing the deployed app on the production server after a deploy

When Claude writes a PR test plan, items in the first group are ones Claude should have already verified. Items in the second group are explicitly for you to check before merging.

## Deployment

> **As of Phase 10 (#131)** the deployed artifact is the **PocketBase** app —
> `pocketbase/Dockerfile` (static Go binary + Litestream), built by CI from the
> `pocketbase/` context. The details below describe that container. The Django app
> still lives in-tree until Phase 11 (#132) but is no longer built or deployed;
> its image survives in GHCR as `:django-latest` for rollback. Full docs:
> `DEPLOYMENT.md` (pipeline, cutover, rollback) and `pocketbase/DEPLOYMENT.md`
> (the container).

Docker-based, single container, SQLite on a persistent volume mounted at `/data`. The container runs [Litestream](https://litestream.io) as its supervising process, replicating SQLite to an S3-compatible bucket (DO Spaces in production). `golftrack-pb` listens on port 8090 inside the container; the production deploy maps host 3000 → 8090 so the public URL stays `…:3000`, and the dev deploy maps 8000 → 8090.

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

`pocketbase/entrypoint.sh` is the container's `CMD`. It is structured so that **`litestream replicate -exec` only ever wraps a single executable, never a shell pipeline.** Litestream v0.5's `-exec` does not invoke `sh -c`, so chaining commands with `&&` inside `-exec` would silently pass them as literal arguments to the first binary. There is nothing to chain here — `golftrack-pb serve` reconciles the database to its embedded `pb_schema.json` inside its own startup, so no migration step precedes it. Keep it that way when modifying the entrypoint. (The Django entrypoint met the same constraint differently, by running `manage.py migrate` as a separate step first.)

Two SQLite databases are replicated, not one: `data.db` (the collections) and `auxiliary.db` (logs), under the `pocketbase/*` bucket paths — distinct from the Django app's `django` path.

If you change the S3-compatible backend (e.g. away from DO Spaces), revisit `pocketbase/litestream.yml` — `force-path-style` is provider-specific (DO Spaces rejects it; B2 requires it). The `LITESTREAM_ENDPOINT` secret must be the region root, not the full bucket URL. Details in `DEPLOYMENT.md`.

The deploy scripts health-check `/api/version` (not `/api/health`, which carries no build identity) and fail the deploy unless the container reports the deploying commit's short SHA.
