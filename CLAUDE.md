# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **Important:** The installed Next.js version has breaking API changes from earlier releases. Before writing any Next.js-specific code, check the relevant guide in `node_modules/next/dist/docs/`.

## Workflow

- **All changes must happen on a feature branch.** Never commit directly to `main`. Before starting work, create a new branch (`git checkout -b <descriptive-name>`).
- **All merges to `main` go through a pull request.** Direct pushes to `main` are blocked by branch protection — including for repository admins. Open a PR with `gh pr create` and merge it from there.
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

## Deployment

Docker-based, single container, SQLite on a persistent volume mounted at `/data`.

```bash
docker build -t golf-track .
docker run --rm -p 3000:3000 \
  -e DATABASE_URL="file:/data/prod.db" \
  -v $(pwd)/data:/data \
  golf-track
```

Before first boot, run migrations: `npx prisma migrate deploy`. See `DEPLOYMENT.md` for full details.
