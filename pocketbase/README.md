# GolfTrack on PocketBase

Phases 1 (#122), 2 (#123), 3 (#124) and 4 (#125) of the Django → PocketBase
migration tracked in epic #121. The overall plan lives in
[`POCKETBASE_MIGRATION_PLAN.md`](../POCKETBASE_MIGRATION_PLAN.md).

**Nothing in this directory is wired into the deployed app.** The Django app in
the repository root is still the only thing built and shipped (see
[`DJANGO.md`](../DJANGO.md) and [`DEPLOYMENT.md`](../DEPLOYMENT.md)). This
directory is a parallel, local-only environment until the Phase 9/10 cutover.

What is here so far: a PocketBase application you can build and run locally, the
six collections defined and reproducible from a committed schema file, their
access rules, the domain logic — the round and shot lifecycle, the stroke cache,
the scoring — as compiled-in Go hooks with the custom routes the generated CRUD
cannot cover, and OAuth sign-in with the admin role assigned from
`ADMIN_EMAILS`.

## One Go binary

PocketBase is consumed as a **Go framework**
(`github.com/pocketbase/pocketbase`), not as a downloaded prebuilt binary.
`go build` in this directory produces a single portable executable
(`golftrack-pb`) that embeds the PocketBase server, its standard CLI (`serve`,
`superuser`, …), the collection schema (`pb_schema.json` via `go:embed`), and
— from Phase 3 on — the domain hooks as compiled Go code. This is an explicit
owner decision, reversing the plan doc's original "JavaScript hooks first"
choice: one artifact to build and ship, and domain logic that is type-checked
and `go test`-able. Rationale and hook architecture: `ARCHITECTURE.md`.

## Layout

```
pocketbase/
├── main.go               # entrypoint: schema sync at startup + hook registration
├── schema.go             # go:embed of pb_schema.json + the sync itself
├── go.mod / go.sum       # module definition, PocketBase version pin
├── pb_schema.json        # source of truth for the six collections (embedded)
├── ARCHITECTURE.md       # hook / business-logic architecture
├── API.md                # PocketBase endpoints vs. the current Django contract
├── AUTH.md               # sign-in, OAuth setup, ADMIN_EMAILS, the auth env vars
├── acl_test.go           # access rules, over HTTP, with real auth tokens
├── auth_test.go          # sign-in and the admin role, over the real OAuth path
├── schema_test.go        # indexes, field validation, delete behaviour
├── domain_test.go        # the business rules, against a real database
├── routes_test.go        # the custom routes, over HTTP
├── parity_test.go        # parity with the Django contract, gap by gap
├── bench_test.go         # the performance baseline (excluded from make pb-test)
├── concurrency_test.go   # the races: two writers on one hole or one player
├── testapp_test.go       # test harness: seeded in-process app + fixtures
├── internal/
│   ├── collections/      # collection names/ids, field names, enum values
│   ├── apierr/           # the {"error": …} contract + the route wrapper
│   ├── authenv/          # the auth configuration, read from the environment
│   ├── records/          # record lookups the domain packages share
│   └── hooks/
│       ├── hooks.go      # Register() — the only place hooks are bound
│       ├── users.go      # users.role field default
│       ├── authconfig.go # OAuth2 providers + password login, from the env
│       ├── adminrole.go  # users.role from ADMIN_EMAILS, on OAuth2 sign-in
│       └── domain/       # one package per aggregate (see its README)
├── scripts/
│   ├── dev.sh            # build the binary + run the dev server
│   ├── apply_schema.py   # pb_schema.json  ->  running instance (reconcile)
│   ├── export_schema.py  # running instance ->  pb_schema.json
│   └── verify_schema.py  # assert the Phase 1 validation gate
└── .local/               # gitignored: compiled binary, pb_data
```

`internal/collections` and `internal/apierr` were `internal/hooks/collections.go`
and `internal/hooks/errors.go` until Phase 3. They moved down a level because
`internal/hooks` is the single hook registration point and therefore imports
every domain package, while the domain packages need the collection vocabulary
and the error contract — the dependency cannot run both ways.

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

## Collections

Six collections, mapped from the Django models they replace. `users` is
PocketBase's built-in auth collection with two fields added; the other five are
new base collections with explicit `golftrack_<collection>` ids so that relation
targets in `pb_schema.json` are legible in review. Unlike *record* ids — which
are fixed at 15 characters — collection ids only have to match `[A-Za-z0-9_]+`
and are not length-limited, so they can spell the collection name out in full.

| Collection | Id | Django model |
|---|---|---|
| `users` | `_pb_users_auth_` | `accounts.User` |
| `courses` | `golftrack_courses` | `courses.Course` |
| `course_holes` | `golftrack_course_holes` | `courses.CourseHole` |
| `rounds` | `golftrack_rounds` | `rounds.Round` |
| `round_holes` | `golftrack_round_holes` | `rounds.RoundHole` |
| `shots` | `golftrack_shots` | `rounds.Shot` |

### Field mapping

**`users`** — built-in auth fields (`id`, `email`, `password`, `verified`,
`name`, `avatar`, …) plus:

| Field | Type | Notes |
|---|---|---|
| `role` | `select` | `USER` / `ADMIN`, required. Defaults to `USER`; assigned from `ADMIN_EMAILS` on OAuth sign-in (Phase 4 — see `AUTH.md`). |
| `display_name` | `text` | max 150. Distinct from PocketBase's built-in `name`, which OAuth providers write to. |

**`courses`**

| Field | Type | Notes |
|---|---|---|
| `name` | `text` | required, max 100 |
| `hole_count` | `number` | required, int, 9–18 |
| `is_archived` | `bool` | |
| `created_at` / `updated_at` | `autodate` | |

Django's `Course.total_par` is a derived property, not a column; it stays
derived (computed in the response, Phase 3).

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
| `course` | `relation` → `courses` | required, **no** cascade delete — see below |
| `status` | `select` | `in_progress` / `completed`, required |
| `play_mode` | `select` | `full` / `front9` / `back9`, required |
| `started_at` | `date` | required |
| `finished_at` | `date` | |
| `note` | `text` | max 1000, matching the Django `CompleteRoundIn` validator |
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
| `strokes` | `number` | int, ≥ 0, **not** `required` — see below |
| `created_at` / `updated_at` | `autodate` | |

**`shots`**

| Field | Type | Notes |
|---|---|---|
| `round_hole` | `relation` → `round_holes` | required, cascade delete |
| `shot_number` | `number` | required, int, ≥ 1 |
| `club` | `text` | required, max 32 |
| `created_at` | `autodate` | |

### Indexes and constraints

Carried over from the Django models:

```
courses         idx_courses_name                        (name)
course_holes    idx_course_holes_course_hole_number     UNIQUE (course, hole_number)
rounds          idx_rounds_user / _status / _course     (user) (status) (course)
rounds          idx_rounds_one_in_progress_per_user     UNIQUE (user) WHERE status = 'in_progress'
round_holes     idx_round_holes_round_hole_number       UNIQUE (round, hole_number)
shots           idx_shots_round_hole_shot_number        UNIQUE (round_hole, shot_number)
```

The partial unique index is the one worth calling out: it is the database-level
expression of "one in-progress round per user", the same
`uq_user_one_in_progress_round` constraint Django emits. `verify_schema.py`
proves both halves — a second in-progress round is rejected, while a *completed*
round for the same user is accepted.

## Access rules

Set in Phase 2 (#123), inside `pb_schema.json` — see "Where the rules live"
below. Every rule is exercised over HTTP by `acl_test.go`.

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
- **`403` means the rule is `nil`** (superuser-only). After this phase none of
  the six collections have one, and `TestEveryCollectionHasExplicitRules` keeps
  it that way.

### Admins read, they do not write

The epic grants admins visibility into every round, not the ability to score
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
role changes remain possible from the Admin UI; Phase 4 automates them from
`ADMIN_EMAILS` on sign-in.

The create rule restricts the *context* of an account creation, not its
contents, which is why Phase 4 also discards the `createData` a client may send
with an OAuth2 sign-in — otherwise `{"role": "ADMIN"}` would walk straight
through it. See "What a caller may not smuggle in" in `AUTH.md`.

That create rule is also why `internal/hooks/users.go` exists. `role` is
required and PocketBase select fields carry no default, so an OAuth2 sign-up —
which supplies only email, name and avatar — would fail validation with
"role: cannot be blank". The hook fills in `USER`, which is the field default
Django writes declaratively as `default=Role.USER`.

### Where the rules live

In `pb_schema.json`, as the `listRule` / `viewRule` / `createRule` /
`updateRule` / `deleteRule` properties of each collection. #123 also asked for a
separate `pocketbase/rules.json`; there is deliberately no such file. PocketBase
has no separate ACL document — rules are properties of a collection, and that is
the only form the import endpoint and the Admin UI accept. A second file would
have to be merged into the schema before it could be applied, which is exactly
the "two sources of truth" problem the embedded-schema design avoids.

## Domain rules and custom routes

Added in Phase 3 (#124). The rules are ported from the Django service layer
(`rounds/services.py`, `courses/services.py`, `rounds/scoring.py`), which is
itself a port of the earlier Next.js `lib/` — behaviour is not supposed to
change again in this migration. `ARCHITECTURE.md` has the full split of which
rule is enforced by the schema, by an access rule, or by a hook; the domain
packages themselves are described in `internal/hooks/domain/README.md`.

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
| `POST` | `/api/rounds/` | create a round, snapshotting the course's holes by play mode |
| `GET` | `/api/rounds/` | list completed rounds, camelCase `RoundOut` shape |
| `GET` | `/api/rounds/in-progress` | the caller's in-progress round (camelCase `RoundDetailOut`) or `null` |
| `GET` | `/api/rounds/{id}` | a round's full detail — nested `course`, `holes`, `shots` — camelCase, `null` totals until completed |
| `POST` | `/api/rounds/{id}/complete` | sum the holes into the round's totals and finish it |
| `POST` | `/api/rounds/{id}/cancel` | delete an in-progress round and everything under it |
| `PATCH` | `/api/rounds/{id}/current-hole` | move to a hole this round is playing |
| `POST` | `/api/rounds/{id}/holes/{n}/shots` | add a shot, maintaining the stroke cache |
| `POST` | `/api/rounds/{id}/holes/{n}/undo` | remove the hole's highest-numbered shot |
| `PATCH` | `/api/rounds/{id}/holes/{n}/shots/{shotId}` | re-club a shot |
| `DELETE` | `/api/rounds/{id}/holes/{n}/shots/{shotId}` | delete a shot and close the numbering gap |

Every write route (and every `/api/rounds/...` read route) is bound with
`apis.RequireAuth()`, which is what restores the `401` an anonymous caller gets
from the current API — a generated endpoint would answer `200` with an empty
list, because a list rule filters rather than gates. The two `/api/courses`
read routes are unauthenticated, matching the current API's course endpoints.

These paths, their request/response field names (`courseId`, `playMode`, `club`,
`note`, `currentHole`, `holeCount`, `isArchived`, `totalStrokes`,
`relativeToPar`) and their status codes are the current contract's, so the
frontend does not have to learn a second one. Responses are camelCase for the
same reason, built by `courses.NewOut`/`rounds.NewOut`/`rounds.NewDetailOut`
(Phase 7, #128) rather than the record's raw field names. What is *not* yet
reconciled is the generated endpoints themselves (still snake_case), and record
ids, which are 15-character strings rather than the integers the current API
returns — both are recorded in `API.md` for Phase 6 (#127).

### Where a request can still be refused

| Situation | Response |
|---|---|
| No auth | `401` |
| A round or hole belonging to someone else | `404` — ownership collapses into the lookup, as it does in Django |
| A round already in progress | `409 {"error": "A round is already in progress"}` |
| A round already completed | `409 {"error": "Round is already completed"}` |
| Cancelling a completed round | `409 {"error": "Only in-progress rounds can be cancelled"}` |
| A play mode, club, note or hole number the rules reject | `400` with the Django message |

## Authentication

Added in Phase 4 (#125). Full detail — provider registration, the redirect URIs
to add, the `createData` guard, and what the Phase 7 frontend can build against
— is in [`AUTH.md`](AUTH.md); the short version:

- **Sign-in is OAuth2**, Google and Microsoft Entra ID, as in the shipped app.
- **Sign-up is OAuth2 and nothing else**, in every environment. Password
  *authentication* for accounts that already exist can be switched on for
  environments with no OAuth apps registered; self-service registration cannot.
  That is Phase 4's recorded decision — see "The password-login decision".
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

The names are Django's, so a host that already sets them needs no new secrets
when the Phase 9 (#130) container arrives; `DEPLOYMENT.md` notes that changing
an environment variable means recreating the container, which applies here too.
A fresh instance with none of them set has no way to sign in to the *app* and
says so at startup — the Admin UI is a separate collection and still opens.

## Validation gate

The Phase 1 (#122) and Phase 2 (#123) gates, and how each item is checked:

| Gate item | How |
|---|---|
| PocketBase local instance up and running | `pocketbase/scripts/dev.sh`, then `curl localhost:8090/api/health` |
| All 6 collections created with correct fields | `go test ./...` (`TestEveryCollectionHasExplicitRules` loads all six) and `verify_schema.py` |
| Admin UI accessible, collections visible | <http://127.0.0.1:8090/_/> returns 200 and lists the six collections |
| `GET /api/collections` returning schema | `curl` with a superuser token returns 11 collections (6 app + 5 PocketBase system) |
| ACL rules enforced (permission denied tests pass) | `acl_test.go` |
| Unique indexes working | `schema_test.go`, plus `verify_schema.py` |
| Field validation working | `schema_test.go`, plus `verify_schema.py` |

The Phase 3 (#124) gate:

| Gate item | How |
|---|---|
| All domain packages created and compiling | `go build ./...`, run by `make pb-test` and CI |
| Course lifecycle works end-to-end | `domain_test.go` — `hole_count`, hole bounds, derived `total_par` (`routes_test.go`) |
| Shot lifecycle: add → edit → delete with correct strokes/numbering | `domain_test.go`, `routes_test.go` |
| Renumbering test covers the ordering explicitly | `TestDeleteShotRenumbersSubsequentShots` — asserts the surviving clubs, not just the count |
| Concurrency tests pass | `concurrency_test.go` — a rejected concurrent write, never a duplicate shot number |
| Custom API routes functional | `routes_test.go`, every route including its 401 |
| 409 where Django returns 409 | `routes_test.go` — round already in progress, round already completed, cancelling a finished round |
| `make pb-test` green | it is |

The Phase 5 (#126) gate:

| Gate item | How |
|---|---|
| All endpoints accessible and return expected status codes | `parity_test.go` — `TestSuccessStatusCodes` walks the 2xx half (201 for round creation, 200 for the rest); the 4xx half is spread across the ownership, gap-7 and constraint tests |
| Response bodies match the contract, or the deviation is recorded with a decision | `API.md` § "Parity gaps" — a decision per gap, each naming the test that pins it |
| ACL enforcement tested on the custom routes | `TestCustomRoutesEnforceOwnership`, `TestCustomRoutesAreNotOpenToAdmins`, `TestCustomRoutesRejectUnknownIds`; the generated endpoints stay covered by `acl_test.go` |
| Pagination / filtering / sorting working | `TestListPagination`, `TestListFiltering`, `TestListSorting` |
| Error responses correct, including 409 and the anonymous-caller codes | `TestConstraintViolationStatusCodes`, `TestErrorBodyShapes`, `TestAnonymousCallerStatusCodes`, `TestUnauthorisedCallerStatusCodes` |
| Performance baseline vs. Django | `bench_test.go` and `API.md` § "Performance baseline". The PocketBase side is checked in; the Django side needs a live environment CI does not provision, and the section says why it is not automated |
| `make pb-test` green | it is |

Phase 5 changed one conclusion from Phase 1: gap 4 (nested relations) was
written up as possibly needing several requests, and measuring it showed the
round → holes → shots chain expands whole in one. `API.md` records the
correction.

The Phase 7 (#128) gate (in progress):

| Gate item | How |
|---|---|
| Custom read routes close gaps 1, 2 and 4 for the shapes the frontend renders | `GET /api/courses`, `/api/courses/{id}`, `/api/rounds`, `/api/rounds/in-progress`, `/api/rounds/{id}` — pinned by `read_routes_test.go`; `API.md` § "Parity gaps" records each gap as closed on these routes |
| All page routes load | pending — reference frontend not yet built |
| All API calls successful | the routes above are covered by HTTP-level tests; a live end-to-end pass against a running frontend is pending |
| Round creation and play flow works | already covered by Phase 3's write routes (`routes_test.go`); read side now closes the loop for rendering |
| No JavaScript console errors | pending — no frontend client checked in yet |
| Styling looks correct | pending |

See `AUTH.md` § "For the frontend" for the still-open auth-token-storage
decision this phase also has to make.

The Phase 4 (#125) gate:

| Gate item | How |
|---|---|
| OAuth flow completes end-to-end for both providers | `auth_test.go` drives the real `/auth-with-oauth2` endpoint with a fake provider registered under Google's name — everything past the token exchange is the production path. The exchange itself needs live credentials: **owner-verified**, see below. |
| Users created on first successful OAuth sign-in, and updated on subsequent ones | `TestFirstOAuth2SignInCreatesTheUserWithRoleUser`, `TestSecondOAuth2SignInUpdatesTheExistingUser` |
| A first-time sign-in lands with `role = USER` through the real OAuth path | `TestFirstOAuth2SignInCreatesTheUserWithRoleUser` — the record does not exist before the request |
| Admin role assigned from `ADMIN_EMAILS`, including a user who was already `USER` | `TestAdminEmailsGrantsAdminOnFirstSignIn`, `TestAdminEmailsPromotesAnExistingUser`; revocation and the empty-list valve are covered too |
| A non-admin still cannot self-assign `ADMIN` — `acl_test.go` still green | it is; `TestRoleIsNotSelfAssignable` is unchanged, and `TestOAuth2CreateDataCannotMintAnAdmin` closes the new path OAuth2 sign-up opened |
| Password-login decision recorded, and `acl_test.go` reflects it | "The password-login decision" in `AUTH.md`; `TestSignupIsOAuth2Only` gained a case proving sign-up stays closed *with* password login enabled |
| Frontend auth cookies validated | **not met, and deferred to Phase 7 (#128)** — see below |

Two items need the owner rather than the suite:

- **The live token exchange.** Whether Google and Microsoft accept the
  registered redirect URI and return a usable profile cannot be tested without
  real client credentials and a browser. `AUTH.md` lists the URI to add to each
  OAuth app.
- **Frontend auth cookies.** There is no PocketBase-facing frontend yet — #125
  scopes its task 5 to "the auth slice only", coordinated with Phase 7 (#128),
  which owns the frontend adaptation. PocketBase's session is a token the
  client holds rather than a server-set cookie, so "auth cookies validated"
  becomes a Phase 7 decision about where that token is stored; the contract and
  the trade-offs are written up in `AUTH.md` § "For the frontend".

#123's gate item "all 6 collections defined in Admin UI" is met a phase
differently than it was written: collections are defined in `pb_schema.json`,
embedded into the binary and reconciled at startup, so they appear in the Admin
UI without anyone clicking through it. Defining them by hand there would in fact
be reverted by the next restart (see "Schema changes").

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
| `parity_test.go` | parity with the Django contract: ownership on the custom routes, pagination/filtering/sorting, the status codes, and a characterisation test per gap in `API.md` |
| `bench_test.go` | the performance baseline — benchmarks, so `go test ./...` skips them |
| `concurrency_test.go` | two writers on one hole, one player starting two rounds, delete racing delete |
| `internal/hooks/domain/scoring/scoring_test.go` | the totals arithmetic, as plain unit tests |
| `testapp_test.go` | the harness: seeded app, fixture builders, auth tokens |

The domain and concurrency suites are ports of `tests/test_services.py` and
`tests/test_concurrency.py`, which are the specification for behaviour this
migration has to preserve.

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
rules. 24 checks as of Phase 1.

## Schema changes

`pb_schema.json` is the source of truth. It is embedded into the binary and
imported at every startup (an upsert by collection id — idempotent, and it
reconciles a drifted dev database). This is also the deterministic startup
path the Phase 9 (#130) container will rely on.

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
second, orderable-only-by-filename source of truth. (The prebuilt-binary
prototype hit exactly that: JS migration files created within the same second
share a timestamp prefix, replay alphabetically, and `created_course_holes`
loaded before `created_courses`.) Data-shape migrations, when Phase 6 needs
them, will be Go migrations compiled into the binary.

## Deviations from `POCKETBASE_MIGRATION_PLAN.md`

The plan's Phase 2 sketch differs from what is committed here in four places.
All four favour matching the Django models, which is what #122 task 2 asks for.

- **`course_holes` has no `created_at`/`updated_at`.** The plan lists them;
  `courses.CourseHole` has no such columns, so adding them would invent data the
  migration in Phase 6 has nothing to fill from.
- **`round_holes.strokes` is not `required`.** PocketBase treats a zero-valued
  number as empty for the purposes of `required`, so a required `strokes` would
  reject the `strokes = 0` a hole is created with. `min: 0` still holds the
  floor.
- **`rounds.course` uses `cascadeDelete: false`.** Django uses
  `on_delete=PROTECT` for this FK. PocketBase has no "restrict" option by name,
  but `cascadeDelete: false` on a *required* relation produces the same
  behaviour: deleting a course that still has rounds is rejected.
  `verify_schema.py` asserts this.
- **`hole_count` is a `number` bounded 9–18, not an enum of `{9, 18}`.**
  PocketBase's number field has no value-set validation. The exact check is
  listed in `ARCHITECTURE.md` as a Phase 3 hook rule.

Phase 2 adds one more, against #123 rather than the plan doc:

- **There is no `pocketbase/rules.json`.** PocketBase has no separate ACL
  document; rules are properties of a collection. See "Where the rules live".

Phase 3 adds two, against #124:

- **`errors.go` is `internal/apierr`, not `internal/hooks/errors.go`**, and the
  collection vocabulary moved with it into `internal/collections`. The issue
  names the original paths; keeping them there would have made
  `internal/hooks` — which imports every domain package so that registration
  stays one reviewable list — an import cycle. See "Layout".
- **Course hole-set completeness is checked at round creation**, not on a course
  write. Django validates a course and its holes as a single payload; PocketBase
  splits them across two collections, and round creation is the first place the
  set is read as a whole. The per-record half of the rule (a hole fits its
  course) is enforced on every `course_holes` write. See `ARCHITECTURE.md`.

Phase 4 adds two, against #125:

- **OAuth provider configuration is not in `pb_schema.json`.** The plan's
  wording ("configure PocketBase OAuth providers") reads as a schema or Admin
  UI change. Client secrets cannot be committed, and a schema file with
  per-environment client ids in it would have to be edited per deployment, so
  the providers are applied from environment variables at every startup instead
  — `internal/hooks/authconfig.go`. The committed schema holds the closed
  baseline. The same mechanism carries `passwordAuth.enabled`.
- **The OAuth2 sign-in hook does more than assign a role.** #125 asks for admin
  detection; it also discards the caller-supplied `createData`. Enabling OAuth2
  sign-up is what makes that map reachable, and the `users` create rule
  restricts only the *context* of an account creation, not its contents — so
  `{"role": "ADMIN"}` would have walked through the rule Phase 2 added
  specifically to stop it. Closing it belongs in the phase that opens it. See
  `AUTH.md`.

## Next phases

| Phase | Issue | Adds |
|---|---|---|
| 5 — API parity | #126 | The endpoint contract, including the anonymous-caller status codes noted in `API.md` |
| 6 — Data migration | #127 | The Django database imported, and the record-id question in `API.md` decided |
| 7 — Frontend | #128 | The frontend against this API, including where the auth token lives (`AUTH.md` § "For the frontend") |
