# GolfTrack on PocketBase

**This directory is the entire app.** CI builds the container from
`pocketbase/Dockerfile`; both the dev server and production run it. See
[`DEPLOYMENT.md`](DEPLOYMENT.md) for the container and
[`../DEPLOYMENT.md`](../DEPLOYMENT.md) for the deployment pipeline.

What is here: a PocketBase application you can build and run locally, the
six collections defined and reproducible from a committed schema file, their
access rules, the domain logic — the round and shot lifecycle, the stroke cache,
the scoring — as compiled-in Go hooks with the custom routes the generated CRUD
cannot cover, OAuth sign-in with the admin role assigned from `ADMIN_EMAILS`,
and the **frontend**: every page, server-rendered Go templates inside the same
binary. `go run . serve` gives you the whole app, not just an API. It also
builds and runs as a **container** — `Dockerfile`, `litestream.yml`,
`entrypoint.sh`, documented in [`DEPLOYMENT.md`](DEPLOYMENT.md) — which is what
CI builds and what both servers run.

## One Go binary

PocketBase is consumed as a **Go framework**
(`github.com/pocketbase/pocketbase`), not as a downloaded prebuilt binary.
`go build` in this directory produces a single portable executable
(`golftrack-pb`) that embeds the PocketBase server, its standard CLI (`serve`,
`superuser`, …), the collection schema (`pb_schema.json` via `go:embed`), and
the domain hooks as compiled Go code — one artifact to build and ship, and
domain logic that is type-checked and `go test`-able. Rationale and hook
architecture: `ARCHITECTURE.md`.

## Layout

```
pocketbase/
├── main.go               # entrypoint: schema sync at startup + hook registration
├── schema.go             # go:embed of pb_schema.json + the sync itself
├── go.mod / go.sum       # module definition, PocketBase version pin
├── pb_schema.json        # source of truth for the six collections (embedded)
├── ARCHITECTURE.md       # hook / business-logic architecture
├── API.md                # PocketBase API endpoint contract
├── AUTH.md               # sign-in, OAuth setup, ADMIN_EMAILS, the auth env vars
├── acl_test.go           # access rules, over HTTP, with real auth tokens
├── auth_test.go          # sign-in and the admin role, over the real OAuth path
├── schema_test.go        # indexes, field validation, delete behaviour
├── domain_test.go        # the business rules, against a real database
├── routes_test.go        # the custom routes, over HTTP
├── parity_test.go        # internal API consistency: generated vs. custom route conventions
├── bench_test.go         # the performance baseline (excluded from make pb-test)
├── perf_test.go          # statements per request, query plans, the no-leak check
├── loadtest_test.go      # 10/50/100 concurrent players (gated; see Performance)
├── performance_report.md # the performance measurements and what they mean
├── concurrency_test.go   # the races: two writers on one hole or one player
├── export_import_test.go # round export/import: cross-instance round trips
├── course_write_routes_test.go # the course write routes, HTTP + reconciliation
├── frontend_workflow_test.go   # each workflow as a request sequence
├── read_routes_test.go   # the custom read routes' shapes
├── web_test.go           # the page routes: rendering, gating, assets
├── testapp_test.go       # test harness: seeded in-process app + fixtures
├── internal/
│   ├── collections/      # collection names/ids, field names, enum values
│   ├── apierr/           # the {"error": …} contract + the route wrapper
│   ├── authenv/          # the auth configuration, read from the environment
│   ├── records/          # record lookups the domain packages share
│   ├── hooks/
│   │   ├── hooks.go      # Register() — the only place hooks are bound
│   │   ├── users.go      # users.role field default
│   │   ├── authconfig.go # OAuth2 providers + password login, from the env
│   │   ├── adminrole.go  # users.role from ADMIN_EMAILS, on OAuth2 sign-in
│   │   └── domain/       # one package per aggregate (see its README)
│   └── web/               # the frontend — see "Frontend" below
│       ├── web.go        # Register(): page routes, static mount, rendering
│       ├── pages.go      # one handler per page
│       ├── auth.go       # the auth cookie: read, verify, never trust
│       ├── funcs.go      # the template functions the markup needs
│       ├── templates/    # go:embed — base layout + one file per page
│       └── static/       # go:embed — Tailwind output, Alpine, SDK, icons
├── scripts/
│   ├── dev.sh            # build the binary + run the dev server
│   ├── apply_schema.py   # pb_schema.json  ->  running instance (reconcile)
│   ├── export_schema.py  # running instance ->  pb_schema.json
│   ├── verify_schema.py  # assert the schema validation gate
│   └── browser-walkthrough.mjs # drive all seven workflows in a real browser
├── Dockerfile             # builder + debian-slim runtime image
├── litestream.yml         # S3 replication for data.db + auxiliary.db
├── entrypoint.sh          # restore-then-serve, under litestream when configured
├── .env.example           # the container's environment variables
├── DEPLOYMENT.md          # how to build/run the container
└── .local/               # gitignored: compiled binary, pb_data
```

`internal/collections` and `internal/apierr` live one level above
`internal/hooks`, because `internal/hooks` is the single hook registration
point and therefore imports every domain package, while the domain packages
need the collection vocabulary and the error contract — the dependency
cannot run both ways.

## Quick start

Requires a Go toolchain (`go.mod` pins the language version; `go` fetches the
matching toolchain itself if the installed one is older) and Python 3 for the
scripts (standard library only).

```bash
# terminal 1 — builds golftrack-pb, syncs the schema, serves
pocketbase/scripts/dev.sh

# terminal 2 — collections already exist (embedded schema); just check them
python3 pocketbase/scripts/verify_schema.py
```

Then open the Admin UI at <http://127.0.0.1:8090/_/> and log in with
`dev@golftrack.local` / `devdevdevdev` (override with `PB_SUPERUSER_EMAIL` /
`PB_SUPERUSER_PASSWORD`).

`dev.sh` honours `PB_HOST` and `PB_PORT`. Everything it writes lands in
`pocketbase/.local/`, so deleting that directory resets the environment
completely.

It also passes the environment through, so a local instance you can actually
sign in to is a matter of exporting the auth variables first:

```bash
# OAuth against a real provider (see AUTH.md for the redirect URI to register)
GOOGLE_CLIENT_ID=… GOOGLE_CLIENT_SECRET=… \
ADMIN_EMAILS=you@example.com \
pocketbase/scripts/dev.sh

# or, with no OAuth app to hand: password login for an account you create
# yourself in the Admin UI
GOLFTRACK_ALLOW_PASSWORD_LOGIN=true pocketbase/scripts/dev.sh
```

With none of them set the app has no sign-in method and says so at startup;
the Admin UI still opens, because superusers are a separate collection.

## Frontend

The app's pages are served by this binary: Go `html/template` pages,
Alpine.js islands, Tailwind, all embedded. There is no second server, no
build step at run time and no CDN — `go build` produces something you can
hand to a browser.

| Route | Gate |
|---|---|
| `/` | open |
| `/courses/` | open |
| `/courses/{id}/` | open |
| `/courses/new/`, `/courses/{id}/edit/` | admin |
| `/courses/archived/` | admin |
| `/rounds/`, `/rounds/new/` | signed in |
| `/rounds/{id}/`, `/rounds/{id}/play/`, `/rounds/{id}/review/` | signed in |
| `/accounts/login/`, `/accounts/logout/` | open |

The course pages are open because `GET /api/courses` is unauthenticated and
viewing courses does not require sign-in. Everything under `/rounds/`
redirects a signed-out visitor to `/accounts/login/?next=…`, and the admin
pages answer a signed-in non-admin with the 403 page.

Three properties are worth stating outright, because they shape the design:

- **Pages are GET-only.** Every write — creating a course, adding a shot,
  archiving, completing a round — is a JSON API call from the page's own
  JavaScript with an `Authorization` header. Nothing is written on the strength
  of a cookie, so there is no CSRF token to protect and none is needed.
- **The cookie is read, never trusted.** `pb_auth` carries a token *and* a copy
  of the user record, and the record half is client-writable. `internal/web/auth.go`
  verifies the token and re-reads the user from the database; a cookie
  hand-edited to `"role":"ADMIN"` opens nothing (`TestTamperedCookieRecordCannotGrantAdmin`).
- **Sign-in is the SDK's popup OAuth2 flow**, completed against
  `/api/oauth2-redirect` — the redirect URI `AUTH.md` already tells you to
  register, so no provider reconfiguration is needed. Sign-out discards the
  cookie; there is no server session to end.

### Running it

```bash
make pb-dev          # or: cd pocketbase && go run . serve
```

Then <http://127.0.0.1:8090/>. Sign-in needs an auth method, so in development
either export the OAuth variables or use
`GOLFTRACK_ALLOW_PASSWORD_LOGIN=true` and create an account in the Admin UI
(self-service sign-up stays closed).

### The asset build

```bash
make pb-css          # bin/build-pb-css.sh — Tailwind -> internal/web/static/css/app.css
```

Root `tailwind.config.js` scans `internal/web/templates` and writes
`internal/web/static/css/app.css`. **The compiled CSS is committed**, because
`go:embed` needs it at build time — a checkout must be able to `go build`
with no Tailwind toolchain installed. Re-run `make pb-css` after touching a
template's classes.

Alpine and the PocketBase JS SDK are vendored under `internal/web/static/js/`
for the same reason the CSS is committed, and because a PWA that needs unpkg.com
to boot is not one. htmx is *not* vendored: `base.html` loaded it at one
point, but no template in the app ever used an `hx-` attribute.

### Checking it in a browser

`web_test.go` renders every page but never runs the JavaScript.
`scripts/browser-walkthrough.mjs` does: it drives Chromium through all seven
workflows and fails on any console error, any third-party request, or any
unexpected status. Its header comments say how to seed an instance for it.

## Collections

Six collections. `users` is PocketBase's built-in auth collection with two
fields added; the other five are new base collections with explicit
`golftrack_<collection>` ids so that relation targets in `pb_schema.json` are
legible in review. Unlike *record* ids — which are fixed at 15 characters —
collection ids only have to match `[A-Za-z0-9_]+` and are not length-limited,
so they can spell the collection name out in full.

| Collection | Id |
|---|---|
| `users` | `_pb_users_auth_` |
| `courses` | `golftrack_courses` |
| `course_holes` | `golftrack_course_holes` |
| `rounds` | `golftrack_rounds` |
| `round_holes` | `golftrack_round_holes` |
| `shots` | `golftrack_shots` |

### Field mapping

**`users`** — built-in auth fields (`id`, `email`, `password`, `verified`,
`name`, `avatar`, …) plus:

| Field | Type | Notes |
|---|---|---|
| `role` | `select` | `USER` / `ADMIN`, required. Defaults to `USER`; assigned from `ADMIN_EMAILS` on OAuth sign-in (see `AUTH.md`). |
| `display_name` | `text` | max 150. Distinct from PocketBase's built-in `name`, which OAuth providers write to. |

**`courses`**

| Field | Type | Notes |
|---|---|---|
| `name` | `text` | required, max 100 |
| `hole_count` | `number` | required, int, 9–18 |
| `time_zone` | `text` | optional IANA time zone (for example `America/New_York`); blank means UTC in the UI |
| `is_archived` | `bool` | |
| `created_at` / `updated_at` | `autodate` | |

`total_par` is a derived property, not a column; it stays derived, computed
in the response.

**`course_holes`**

| Field | Type | Notes |
|---|---|---|
| `course` | `relation` → `courses` | required, cascade delete |
| `hole_number` | `number` | required, int, 1–18 |
| `par` | `number` | required, int, 3–6 |

**`rounds`**

| Field | Type | Notes |
|---|---|---|
| `user` | `relation` → `users` | required, cascade delete |
| `course` | `relation` → `courses` | required, **no** cascade delete. PocketBase has no explicit "restrict" option, but `cascadeDelete: false` on a *required* relation produces the same effect: deleting a course that still has rounds is rejected (`verify_schema.py` asserts this). |
| `status` | `select` | `in_progress` / `completed`, required |
| `play_mode` | `select` | `full` / `front9` / `back9`, required |
| `started_at` | `date` | required |
| `finished_at` | `date` | |
| `note` | `text` | max 1000 |
| `current_hole` | `number` | required, int, 1–18 |
| `total_strokes`, `total_par` | `number` | int, ≥ 0, populated on completion |
| `relative_to_par` | `number` | int, may be negative |
| `created_at` / `updated_at` | `autodate` | |

**`round_holes`**

| Field | Type | Notes |
|---|---|---|
| `round` | `relation` → `rounds` | required, cascade delete |
| `hole_number` | `number` | required, int, 1–18 |
| `par` | `number` | required, int, 3–6. Snapshotted from `course_holes` at round creation. |
| `strokes` | `number` | int, ≥ 0, **not** `required`. PocketBase treats a zero-valued number as empty for the purposes of `required`, so a required `strokes` would reject the `strokes = 0` a hole is created with. |
| `created_at` / `updated_at` | `autodate` | |

**`shots`**

| Field | Type | Notes |
|---|---|---|
| `round_hole` | `relation` → `round_holes` | required, cascade delete |
| `shot_number` | `number` | required, int, ≥ 1 |
| `club` | `text` | required, max 32 |
| `created_at` | `autodate` | |

### Indexes and constraints

```
courses         idx_courses_name                        (name)
course_holes    idx_course_holes_course_hole_number     UNIQUE (course, hole_number)
rounds          idx_rounds_user / _status / _course     (user) (status) (course)
rounds          idx_rounds_one_in_progress_per_user     UNIQUE (user) WHERE status = 'in_progress'
round_holes     idx_round_holes_round_hole_number       UNIQUE (round, hole_number)
shots           idx_shots_round_hole_shot_number        UNIQUE (round_hole, shot_number)
```

The partial unique index is the one worth calling out: it is the database-level
expression of "one in-progress round per user". `verify_schema.py`
proves both halves — a second in-progress round is rejected, while a *completed*
round for the same user is accepted.

Two more indexes were added later, based on query plans rather than the
initial design:

```
courses         idx_courses_archived_name               (is_archived, name)
rounds          idx_rounds_user_status_finished         (user, status, finished_at DESC, started_at DESC)
```

The second is the one that mattered. `idx_rounds_status` is a single-column
index on a two-valued column, so the completed-rounds list was selecting every
completed round *in the database* and filtering by user afterwards, then sorting
in a temporary B-tree. `idx_rounds_user` and `idx_rounds_status` are kept — they
mirror the original single-column indexes — but the planner now prefers the
composite. `TestHotQueriesUseAnIndex` asks SQLite for the plan of every query the
read paths issue and fails on a table scan, which is how both were found.

## Access rules

Defined inside `pb_schema.json` — see "Where the rules live" below. Every
rule is exercised over HTTP by `acl_test.go`.

| Collection | Read | Write |
|---|---|---|
| `users` | own record; admins read all | own record, except `role`; sign-up is OAuth2-only |
| `courses`, `course_holes` | any authenticated user | admins only |
| `rounds`, `round_holes`, `shots` | own rounds; admins read all | own rounds only |

Two building blocks appear in every rule: `@request.auth.id != ""` for
"authenticated", and `@request.auth.role = "ADMIN"` for the admin check, which
reads the `role` field off the caller's own `users` record. `round_holes` and
`shots` reach their owner across relations — `round.user = @request.auth.id`
and `round_hole.round.user = @request.auth.id`.

Three consequences worth knowing before reading a response:

- **A list rule filters, it does not gate.** A caller who may see nothing gets
  `200` with `"totalItems":0`, never `403`.
- **A failing view/update/delete rule returns `404`**, so a response cannot be
  used to probe whether a record id exists. A failing create rule returns `400`.
- **`403` means the rule is `nil`** (superuser-only). None of the six
  collections have one, and `TestEveryCollectionHasExplicitRules` keeps
  it that way.

### Admins read, they do not write

Admins have visibility into every round, not the ability to score
one: `rounds`, `round_holes` and `shots` are admin-readable but owner-writable.
Course management is the reverse — admins are the only writers.

### `role` is not self-assignable

`role` is an ordinary field on a collection users can update, so the update rule
carries an explicit guard:

```
@request.auth.id != "" && id = @request.auth.id
  && (@request.body.role:isset = false || @request.body.role = role)
```

Without the second clause any user could `PATCH` themselves to `ADMIN` and
inherit read access to every round in the database. The other half of that path
— registering a fresh account with `"role": "ADMIN"` — is closed by the create
rule, `@request.context = "oauth2"`: sign-up happens through an OAuth provider
and nothing else, in every environment. Superusers bypass rules entirely, so
role changes remain possible from the Admin UI; the app also automates them from
`ADMIN_EMAILS` on sign-in.

The create rule restricts the *context* of an account creation, not its
contents, which is why the app also discards the `createData` a client may send
with an OAuth2 sign-in — otherwise `{"role": "ADMIN"}` would walk straight
through it. See "What a caller may not smuggle in" in `AUTH.md`.

That create rule is also why `internal/hooks/users.go` exists. `role` is
required and PocketBase select fields carry no default, so an OAuth2 sign-up —
which supplies only email, name and avatar — would fail validation with
"role: cannot be blank". The hook fills in `USER` as the field default.

### Where the rules live

In `pb_schema.json`, as the `listRule` / `viewRule` / `createRule` /
`updateRule` / `deleteRule` properties of each collection. There is
deliberately no separate `pocketbase/rules.json` file: PocketBase has no
separate ACL document — rules are properties of a collection, and that is the
only form the import endpoint and the Admin UI accept. A second file would
have to be merged into the schema before it could be applied, which is exactly
the "two sources of truth" problem the embedded-schema design avoids.

## Domain rules and custom routes

The domain rules are implemented in Go packages under `internal/hooks/domain`.
`ARCHITECTURE.md` has the full split of which rule is enforced by the schema,
by an access rule, or by a hook; the domain packages themselves are described
in `internal/hooks/domain/README.md`.

The one thing worth repeating here, because it decides where every rule lives:
**API rules do not apply to hook code.** They gate the generated endpoints and
PocketBase's own request handlers. Anything going through `app.Save`,
`app.Delete` or `RunInTransaction` writes as the application, so a custom route
enforces ownership itself, and any invariant a client could otherwise break is
bound to a record hook rather than to a route.

### Custom routes

| Method | Path | Does |
|---|---|---|
| `GET` | `/api/courses` | list courses, camelCase `CourseOut` shape, holes nested inline |
| `GET` | `/api/courses/{id}` | a single course, same shape |
| `POST` | `/api/courses/` | create a course and its whole hole set in one transaction (admin) |
| `PUT` | `/api/courses/{id}` | rename, resize and reconcile the hole set in one transaction (admin) |
| `POST` | `/api/rounds/` | create a round, snapshotting the course's holes by play mode |
| `GET` | `/api/rounds/` | list completed rounds, camelCase `RoundOut` shape |
| `GET` | `/api/rounds/in-progress` | the caller's in-progress round (camelCase `RoundDetailOut`) or `null` |
| `GET` | `/api/rounds/{id}` | a round's full detail — nested `course`, `holes`, `shots` — camelCase, `null` totals until completed |
| `POST` | `/api/rounds/{id}/complete` | sum the holes into the round's totals, optionally overriding `startedAt` and `finishedAt`, and finish it |
| `POST` | `/api/rounds/{id}/cancel` | delete an in-progress round and everything under it |
| `PATCH` | `/api/rounds/{id}/current-hole` | move to a hole this round is playing |
| `POST` | `/api/rounds/{id}/holes/{n}/shots` | add a shot, maintaining the stroke cache |
| `POST` | `/api/rounds/{id}/holes/{n}/undo` | remove the hole's highest-numbered shot |
| `PATCH` | `/api/rounds/{id}/holes/{n}/shots/{shotId}` | re-club a shot |
| `DELETE` | `/api/rounds/{id}/holes/{n}/shots/{shotId}` | delete a shot and close the numbering gap |

Every write route (and every `/api/rounds/...` read route) is bound with
`apis.RequireAuth()`, which gives an anonymous caller a `401` — a generated
endpoint would instead answer `200` with an empty list, because a list rule
filters rather than gates. The two `/api/courses` read routes are
unauthenticated.

The two course *write* routes go one step further and require `role = ADMIN`
— a signed-in player gets `403 {"error": "Forbidden"}`. A PocketBase superuser
passes: it already bypasses the collection rules the generated endpoints
enforce, so refusing it on the custom route would make the custom route the
stricter path for no reason an operator would expect. Round routes have no such
branch — "admin" here means course administration, never impersonation, and
`parity_test.go` keeps it that way.

These paths, their request/response field names (`courseId`, `playMode`,
`club`, `note`, `currentHole`, `holeCount`, `isArchived`, `totalStrokes`,
`relativeToPar`) and their status codes are the app's stable contract, so the
frontend does not have to learn a second one. Responses are camelCase for the
same reason, built by `courses.NewOut`/`rounds.NewOut`/`rounds.NewDetailOut`
rather than the record's raw field names. The generated endpoints remain
snake_case, and record ids are 15-character strings — both documented in
`API.md`.

### Where a request can still be refused

| Situation | Response |
|---|---|
| No auth | `401` |
| Creating or editing a course as a non-admin | `403 {"error": "Forbidden"}` |
| Editing a course that does not exist | `404 {"error": "Course not found"}` |
| A course payload the `CourseIn` rules reject | `400` with a validation message, and nothing written |
| A round or hole belonging to someone else | `404` — ownership collapses into the lookup |
| A round already in progress | `409 {"error": "A round is already in progress"}` |
| A round already completed | `409 {"error": "Round is already completed"}` |
| Cancelling a completed round | `409 {"error": "Only in-progress rounds can be cancelled"}` |
| A play mode, club, note or hole number the rules reject | `400` with a validation message |

## Authentication

Full detail — provider registration, the redirect URIs to add, the
`createData` guard, and what the frontend can build against — is in
[`AUTH.md`](AUTH.md); the short version:

- **Sign-in is OAuth2**, Google and Microsoft Entra ID.
- **Sign-up is OAuth2 and nothing else**, in every environment. Password
  *authentication* for accounts that already exist can be switched on for
  environments with no OAuth apps registered; self-service registration cannot.
  See "The password-login decision" in `AUTH.md`.
- **`users.role` is synced from `ADMIN_EMAILS` on every OAuth sign-in**,
  granting *and* revoking, with an empty list deliberately doing nothing.

None of it is committed, because none of it can be: client secrets are secrets
and the rest differs per environment. `pb_schema.json` holds the closed
baseline and `internal/hooks/authconfig.go` applies the environment over it at
startup.

| Variable | Default | Effect |
|---|---|---|
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | unset | registers the Google provider |
| `MICROSOFT_CLIENT_ID` / `MICROSOFT_CLIENT_SECRET` | unset | registers the Microsoft Entra ID provider |
| `ADMIN_EMAILS` | unset | comma-separated addresses granted `role = ADMIN` |
| `GOLFTRACK_ALLOW_PASSWORD_LOGIN` | `false` | email+password login for existing accounts |

`DEPLOYMENT.md` notes that changing an environment variable means recreating
the container, which applies here too.
A fresh instance with none of them set has no way to sign in to the *app* and
says so at startup — the Admin UI is a separate collection and still opens.

## Performance

The measurements and the reasoning behind each assertion are in
[`performance_report.md`](performance_report.md); the short version:

- **Reads are 3–6 statements each, whatever the size of the round.** They used
  to be one query per hole and one per round — 41 statements for an 18-hole
  round's detail, 66 for the home page of a player with a dozen rounds behind
  them. The batched lookups live in `internal/records`.
- **The round-detail read is 3.2× faster** than before these query
  optimisations, and faster than the generated endpoint with `expand` for the
  same data.
- **Writes saturate at ~290 requests/second** on a 4-vCPU machine, because
  PocketBase runs them on a one-connection pool over SQLite's single writer. At a
  realistic pace every endpoint is under 5 ms p95 with a hundred players on the
  course.

```bash
make pb-bench         # the timings
make pb-loadtest      # 10/50/100 concurrent players; slow, and gated
```

The load sweep is gated on `GOLFTRACK_LOADTEST`, so it does not run with
`make pb-test`: it seeds a hundred players per level, and it is a measuring
instrument whose output is a table for a human rather than a pass/fail. What
*is* in `make pb-test` is the part that does not depend on the machine — the
statement counts, the query plans and the leak check.

## Tests

```bash
make pb-test          # from the repository root
cd pocketbase && go test ./...
```

The suites boot the real PocketBase application core in-process
(`tests.NewTestApp`) against an empty data directory, then apply the committed
`pb_schema.json` through the same `syncSchema` the binary runs at startup — so
what the tests exercise is the schema as committed, with no second fixture copy
that could drift from it.

| File | Covers |
|---|---|
| `acl_test.go` | every rule, over HTTP, as anonymous / user / other user / admin / superuser |
| `auth_test.go` | sign-in over the real OAuth path: record creation, the `ADMIN_EMAILS` sync, the `createData` guard, the password-login switch |
| `internal/authenv/authenv_test.go` | the environment parsing, as plain unit tests |
| `schema_test.go` | unique indexes, field bounds and enums, cascade and restrict deletes, the `role` default |
| `domain_test.go` | the business rules — snapshotting, the stroke cache, renumbering, totals, cancellation |
| `routes_test.go` | the custom routes end to end, including their 401s, 404s and 409s |
| `read_routes_test.go` | the custom read routes: camelCase, nested relations, `null` totals |
| `course_write_routes_test.go` | `POST`/`PUT /api/courses`: admin gating, `CourseIn` validation, hole reconciliation, and that a rejected payload writes nothing |
| `frontend_workflow_test.go` | each frontend workflow as the request sequence a browser client would make |
| `parity_test.go` | internal API consistency: ownership on the custom routes, pagination/filtering/sorting, the status codes, and a characterisation test per documented design decision in `API.md` |
| `web_test.go` | the page routes: every page renders, the sign-in redirects, the admin refusals, the auth cookie (valid, absent, forged), the embedded assets, and that no page references a CDN |
| `bench_test.go` | the performance baseline — benchmarks, so `go test ./...` skips them |
| `perf_test.go` | how many statements each read path issues, counted at two data sizes so an N+1 shows up as a number that moves; the query plan of every hot query; and that sustained traffic leaks neither heap nor goroutines |
| `loadtest_test.go` | 10/50/100 concurrent players, reads, writes and both — gated on `GOLFTRACK_LOADTEST`, because seeding a hundred players costs more than the rest of this directory put together |
| `concurrency_test.go` | two writers on one hole, one player starting two rounds, delete racing delete |
| `export_import_test.go` | round export/import (GOL-1): the JSON and CSV round trips across two separate app instances, the missing-course and hole-count-mismatch rejections, duplicate skipping, per-round validation, the import template's validity in both formats, and the routes' 401/400/409s |
| `internal/hooks/domain/scoring/scoring_test.go` | the totals arithmetic, as plain unit tests |
| `testapp_test.go` | the harness: seeded app, fixture builders, auth tokens |
| `scripts/browser-walkthrough.mjs` | the seven workflows in a real Chromium — the one check that runs the JavaScript. Manual: it needs a seeded instance, so it is not part of `make pb-test` |

`acl_test.go` drives `tests.ApiScenario`, which builds a router and triggers
`OnServe` per scenario. PocketBase's UI extension routes re-register on every
trigger and panic the second time round, so each scenario gets its own freshly
seeded app — which is why the fixture record ids are fixed strings rather than
generated: a scenario's URL and expected body are written before its app exists.

`verify_schema.py` remains as the against-a-live-server check, and is the one
that proves the schema works when applied over HTTP rather than embedded. It
exercises the write-time constraints — the partial unique index, the composite
unique indexes, relation cascades, the restrict-on-delete behaviour for courses,
and field range validation — and cleans up the records it creates, so it is safe
to re-run. It authenticates as a superuser, so it is unaffected by the access
rules.

## Schema changes

`pb_schema.json` is the source of truth. It is embedded into the binary and
imported at every startup (an upsert by collection id — idempotent, and it
reconciles a drifted dev database). This is also the deterministic startup
path the container relies on.

### Schema changes are additive by default

**Add fields; do not remove them.** A field dropped from `pb_schema.json` takes
its column and every value in it with it on the next restart, irreversibly and
with no confirmation step — that is the one edit to this file that destroys
production data. Retiring a field only when there is a compelling technical
reason to (a genuine conflict, a security problem, a storage cost that actually
matters) costs a column nobody reads; removing one costs the data.

The protection the startup sync gives you stops at the collection boundary:
`ImportCollectionsByMarshaledJSON(schemaJSON, false)` passes `deleteMissing=false`,
so a collection missing from the file is left alone — but fields *within* a
collection present in the file are reconciled to exactly what the file lists.
Nothing in the deploy path re-checks that, so this convention is the guard.

In practice, when a field falls out of use:

- **Leave it in `pb_schema.json`**, and mark it in the JSON comment-free way
  available here — a `// deprecated` note in this document's table, plus the
  reason and the date.
- **Make sure it is not `required`**, or every future create has to keep
  supplying a value for a field nothing reads.
- **Stop reading it in Go**, which is the change that actually retires it.
  Removing it from the schema is a separate, later decision that needs its own
  justification.

Renames are removals wearing a hat: PocketBase matches fields by id, so a
renamed field is a drop plus an add unless the id is preserved. Add the new
field, backfill, then leave the old one.

To change the schema:

- **Edit `pb_schema.json`**, then restart (`dev.sh`) — the startup sync
  applies it. To apply without a restart, `python3
  pocketbase/scripts/apply_schema.py` pushes the file over HTTP instead.
- **Edit in the Admin UI**, then export it back **before restarting**:

  ```bash
  python3 pocketbase/scripts/export_schema.py   # instance -> pb_schema.json
  git diff pocketbase/pb_schema.json
  ```

  The order matters: on restart the startup sync reverts any Admin UI change
  that wasn't exported (the binary embeds the file as committed — rebuild
  picks up edits since `dev.sh` always rebuilds). Set
  `GOLFTRACK_SCHEMA_SYNC=0` to serve without the sync while experimenting.

Export strips collection-level `created`/`updated` timestamps and sorts keys,
so sync → export round-trips byte-for-byte and diffs stay readable.

It also leaves the `users` collection's `oauth2` and `passwordAuth` blocks as
committed. Those come from the environment at startup (see "Authentication"),
so a running instance carries the deployment's client ids and password-login
state — exporting them would commit a per-environment provider list, and one
that no longer imports, since PocketBase redacts client secrets on the way out.
To change *those*, change the environment; `pb_schema.json` only holds their
closed baseline.

There are deliberately no `pb_migrations` files: with the schema reconciled
from one committed document at startup, per-change migration files would be a
second, orderable-only-by-filename source of truth. Data-shape migrations,
if ever needed, will be Go migrations compiled into the binary.

The redundant `idx_rounds_user` / `idx_rounds_status` indexes noted above
(superseded by `idx_rounds_user_status_finished` but kept because they mirror
the original single-column indexes) are a follow-up cleanup candidate — see
the index discussion above before touching them.
