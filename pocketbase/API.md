# PocketBase API surface

What PocketBase generates for the collections in `pb_schema.json`, and the
custom routes built on top for the endpoints the frontend actually calls.
`parity_test.go` is the suite that pins the behaviour described below.

## Generated endpoints

Every collection gets full CRUD under `/api/collections/{collection}/records`:

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/collections/{c}/records` | list; `page`, `perPage`, `sort`, `filter`, `expand`, `fields`, `skipTotal` |
| `GET` | `/api/collections/{c}/records/{id}` | view; `expand`, `fields` |
| `POST` | `/api/collections/{c}/records` | create |
| `PATCH` | `/api/collections/{c}/records/{id}` | update |
| `DELETE` | `/api/collections/{c}/records/{id}` | delete |

`users` is an auth collection, so it also gets `/auth-with-password`,
`/auth-with-oauth2`, `/auth-refresh`, `/request-password-reset`,
`/request-verification`, `/request-otp` and friends under
`/api/collections/users/`.

Which of those actually answer is decided by the auth configuration applied
from the environment at startup — `AUTH.md` has the detail. The four the
frontend uses:

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/collections/users/auth-methods` | which providers are enabled, and whether password login is; unauthenticated |
| `POST` | `/api/collections/users/auth-with-oauth2` | the sign-in; answers `{token, record, meta}` |
| `POST` | `/api/collections/users/auth-with-password` | `403` unless `GOLFTRACK_ALLOW_PASSWORD_LOGIN` is on |
| `POST` | `/api/collections/users/auth-refresh` | a fresh token for a still-valid one |

The session is a JWT the client holds and sends as `Authorization`, rather
than a server-set cookie. That distinction is written up in `AUTH.md` §
"For the frontend".

`POST /api/collections/users/records` — the generated create, i.e. self-service
sign-up — is closed in every environment by the `users` create rule.

Schema and health endpoints:

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/collections` | collection schema; superuser token required |
| `GET` | `/api/health` | unauthenticated |
| `GET` | `/_/` | Admin UI |

List responses are `{items, page, perPage, totalItems, totalPages}`. Relations
are returned as id strings, with the related records nested under `expand` when
requested:

```
GET /api/collections/rounds/records?expand=course&filter=(status='in_progress')

{"items": [{"id": "n44sjihdzpo0i8l", "course": "yceuliutpkhlhls", "status": "in_progress",
            "expand": {"course": {"id": "yceuliutpkhlhls", "name": "Probe GC", ...}}}], ...}
```

## Custom routes

The generated CRUD above is not enough for every endpoint the frontend needs,
because some requests have to touch more than one collection atomically, or
because the path addresses a shot by `(round, hole_number)` rather than by
its own record id:

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/courses` | list courses, camelCase `CourseOut` shape |
| `POST` | `/api/courses/` | create a course and its holes in one transaction, admin-only |
| `GET` | `/api/courses/{id}` | a single course, same shape |
| `PUT` | `/api/courses/{id}` | rename, resize and reconcile the hole set in one transaction, admin-only |
| `GET` | `/api/rounds/` | list completed rounds, camelCase `RoundOut` |
| `POST` | `/api/rounds/` | create a round, snapshotting the course's holes |
| `GET` | `/api/rounds/in-progress` | the caller's in-progress round, camelCase `RoundDetailOut`, or `null` |
| `GET` | `/api/rounds/{id}` | a round's full detail — nested `course`/`holes`/`shots`, camelCase |
| `POST` | `/api/rounds/{id}/complete` | sum the holes into the round's totals, optionally overriding `startedAt`/`finishedAt`, and finish it |
| `POST` | `/api/rounds/{id}/cancel` | delete an in-progress round and everything under it |
| `PATCH` | `/api/rounds/{id}/current-hole` | move to a hole this round is playing |
| `POST` | `/api/rounds/{id}/holes/{n}/shots` | add a shot, maintaining the stroke cache |
| `POST` | `/api/rounds/{id}/holes/{n}/undo` | remove the hole's highest-numbered shot |
| `PATCH` | `/api/rounds/{id}/holes/{n}/shots/{shotId}` | re-club a shot |
| `DELETE` | `/api/rounds/{id}/holes/{n}/shots/{shotId}` | delete a shot and close the numbering gap |

`ARCHITECTURE.md` covers what each does; `routes_test.go` exercises them.

The two course write routes exist for the same reason `POST /api/rounds/`
does: a course is submitted as one payload and stored across two collections
(`courses` and `course_holes`), so an 18-hole course through generated CRUD
would be 19 separate requests, and a failure part-way through would leave a
course whose hole set is incomplete — nothing would reject that course at the
time, since `rounds.Create` only refuses an incomplete hole set at the *next
round*, one request removed from the edit that broke it. `POST /api/courses/`
and `PUT /api/courses/{id}` do the whole thing in one transaction, validate
the payload before opening it, and gate on `role = ADMIN`. `PUT` reconciles
rather than replaces: a hole still in the payload keeps its record (and its
id), a hole absent from it is deleted. Archive and unarchive stay on the
generated `PATCH` — one field, already admin-gated by the access rules.
`course_write_routes_test.go` exercises both.

`PATCH …/shots/{shotId}` is built as a custom route (rather than the
generated `PATCH /api/collections/shots/records/{id}`) so that the whole
nested path space behaves consistently — the same 401 for an anonymous
caller, the same `{"error": …}` body, the same `(round, hole_number)`
addressing — rather than one verb in the group behaving like a generated
endpoint and the rest like custom ones.

`GET` on nested/collection paths (`/rounds/{id}/holes/...`) is still handled
by generated endpoints; only the top-level `RoundDetailOut`/`CourseOut`-shaped
reads are custom.

The nested write paths carry a hole number the generated endpoints have no
concept of — `POST /api/rounds/12/holes/3/shots` addresses a shot by
`(round, hole_number)`, where PocketBase addresses it by `round_hole` id. The
custom routes resolve that, which is also a lookup the frontend would
otherwise have to make itself.

## Design decisions in the custom routes

A handful of differences between the generated CRUD and the custom routes are
worth calling out explicitly, because they are deliberate and each is pinned
by a test in `parity_test.go`.

**Field naming.** The schema fields are snake_case, so the generated
endpoints return `hole_count`, `is_archived`, `play_mode`, `current_hole`,
`total_strokes`, `relative_to_par`. The custom routes transform these to
camelCase (`holeCount`, `isArchived`, `totalPar`, `playMode`, `currentHole`,
`totalStrokes`, `relativeToPar`) via `courses.NewOut`/`rounds.NewOut`/
`rounds.NewDetailOut`, because the frontend that consumes them is
JavaScript. Keeping the schema on snake_case keeps it legible against the
domain model; transforming in the route layer avoids forcing every consumer
of the generated endpoints — including the Admin UI — into a naming
convention PocketBase itself doesn't use.
Pinned by `TestGeneratedEndpointsReturnSnakeCase`, `TestCustomRoutesReturnCamelCase`,
`TestCourseReadRoutesReturnCamelCase`, `TestRoundDetailRouteReturnsCamelCaseAndNestedCourse`.

One field is worth calling out on its own: `total_par` on a course is
returned by the generated endpoints too, in snake_case, because it is
derived per response through `OnRecordEnrich` rather than being a column.

**Unset numbers are omitted, not zero.** A PocketBase number field has no
null, so a fresh in-progress round's generated representation returns
`"total_strokes": 0, "total_par": 0, "relative_to_par": 0` — which would
render a round that hasn't been completed as "even par" instead of blank.
Unset dates behave the same way, returning `""` rather than being absent.
`GET /api/rounds/{id}` and `GET /api/rounds/in-progress` check `finished_at`
on the record and, when it is unset, omit `finishedAt`, `totalStrokes`,
`totalPar` and `relativeToPar` from the response body entirely (encoded as
JSON `null` via untyped `interface{}` fields) rather than serialising the
PocketBase zero values.
Pinned by `TestRoundDetailRouteEmitsNullForUnsetTotals` and
`TestInProgressRouteReturnsTheDetailShape`.

**Record ids are 15-character strings**, PocketBase's own id format. Pinned
by `TestRecordIdsAreStrings`.

**Relations nest inline on the custom read routes.** `RoundDetailOut` nests
`course` (with its `holes`) and `holes` (with their `shots`) directly, rather
than returning `"course": "yceuliutpkhlhls"` with the record available only
under `expand.course` on request. The full round → holes → shots chain
expands in one PocketBase query
(`?expand=round_holes_via_round.shots_via_round_hole`), well inside
PocketBase's expansion depth cap, so `GET /api/rounds/{id}` reshapes exactly
that expand chain into the nested response — no extra round trips.
Pinned by `TestRelationsAreIdsPlusExpand`,
`TestRoundDetailRouteReturnsCamelCaseAndNestedCourse`.

**Error bodies.** Custom routes return `{"error": "<message>"}`, written by
`internal/apierr`. Generated endpoints return PocketBase's own shape —
`{"data": {…}, "message": "…", "status": 400}` — left as-is: rewriting it
would mean a global response middleware reshaping PocketBase's own errors,
including on the auth and Admin UI routes, where the SDK and Admin UI expect
PocketBase's native shape.

**Status codes for constraint violations.** The custom routes return `409`
explicitly for "a round is already in progress" and "round is already
completed", including when the conflict is only detected as a lost race —
`rounds.Create` re-reads the player's in-progress round after a failed save
so that the partial unique index is reported as the conflict it is rather
than as a `400`. A unique-index violation on a generated endpoint still
surfaces as `400`; that difference is the concrete reason round creation is
a custom route and the frontend must not post round records directly.
`routes_test.go` asserts each of the three `409`s, and
`TestConstraintViolationStatusCodes` asserts both sides of the same conflict:
`409` through `POST /api/rounds/`, `400` through the generated create.

Also worth noting: `GET /api/health` returns
`{"message": "API is healthy.", "code": 200, "data": {}}`.
`TestHealthEndpointShape` pins this shape so the deployment health-check
probe is wired against something known.

## Access rules and status codes

The access rule set and its rationale are in `README.md`; `acl_test.go`
asserts each rule over HTTP. What matters here is the status codes the
generated endpoints produce for an unauthenticated or unauthorised caller:

| Situation | Response |
|---|---|
| Unauthenticated list | `200 {"items":[],"totalItems":0}` — a list rule filters rather than gates |
| Unauthenticated view of an existing record | `404` |
| Authenticated but not permitted (e.g. a non-admin writing a course) | `404` on update/delete, `400` on create |
| Another user's round | `404 {"error": "Round not found"}` |

None of these leak data, and 404-instead-of-403 is deliberate: a response
cannot be used to probe whether a record id exists. But a frontend that keys
off `401` to redirect to sign-in would see an empty list instead — which is
why the custom routes are all bound with `apis.RequireAuth()` and answer
`401` for an anonymous caller. `TestCustomRoutesRequireAuth` walks every one
of them anonymously and asserts the `401`. The generated endpoints still
behave as the table above describes, and `TestAnonymousCallerStatusCodes` /
`TestUnauthorisedCallerStatusCodes` pin each row of it.

The generated endpoints are deliberately not fronted with a `401`-restoring
middleware: that would mean overriding the list rules with logic that
duplicates them, and it would break the auth endpoints, which are
unauthenticated by definition. Instead the frontend decides "signed in?"
from the token it holds rather than from a status code, and treats a `404`
on a record it expected as "gone or not yours". The one code it must keep
handling is the `401` from the custom routes, which is the honest one: it
means the token was missing or expired.

Ownership on the custom routes is enforced by the handlers themselves, since
collection API rules do not apply to hook code: another player's round is
`404 {"error": "Round not found"}`, never `403`.
`TestCustomRoutesEnforceOwnership` walks the whole custom surface as another
player and `TestCustomRoutesAreNotOpenToAdmins` walks it as an admin and as a
superuser, because that is the one place a role could plausibly override
ownership and must not: `role = ADMIN` widens the *read* rules on rounds,
but nothing widens write access — an admin cannot score somebody else's
round either. Every one of those scenarios also asserts that the denied
request wrote nothing.

## Pagination, filtering and sorting

Covered by `TestListPagination`, `TestListFiltering` and `TestListSorting`.
`GET /api/rounds/` (the custom route) takes `limit`/`page` and returns a
bare JSON array; a generated PocketBase list takes `page`/`perPage` and
returns the `{items, page, perPage, totalItems, totalPages}` envelope.

What the tests establish:

- **Pagination.** `page`/`perPage` with the full envelope; a page past the end
  is `200` with `"items":[]` and the totals still present, not a `404`;
  `skipTotal=1` drops the counting query and reports `totalItems: -1`.
- **Filtering.** The two round lists are filter expressions —
  `filter=(status='completed')` and
  `filter=(status='in_progress')&perPage=1` — over a list already narrowed by
  the rule, so a filter cannot widen access: asking for another player's round
  by id still returns `totalItems: 0`. A malformed filter is `400`, in
  PocketBase's error shape.
- **Sorting.** Ascending, descending (`-shot_number`) and multi-key
  (`round,-hole_number`). An unknown sort field is `400`.

## Performance baseline

`bench_test.go` measures the endpoints the play and review screens hit, in
process, against the parity suite's fixture:

```
cd pocketbase && go test -run '^$' -bench . -benchmem
```

Taken on a CI runner (AMD EPYC 7763, 4 vCPU, Go 1.25, SQLite in a temp
directory):

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| `BenchmarkAddShot` | ~1.55 M | 142 k | 2 762 |
| `BenchmarkSetCurrentHole` | ~0.77 M | 99 k | 1 941 |
| `BenchmarkRoundDetail` | ~1.58 M | 287 k | 4 509 |
| `BenchmarkListRounds` | ~0.83 M | 113 k | 1 996 |
| `BenchmarkHealth` | ~0.13 M | 22 k | 324 |

Read these as ratios, not as absolutes. There is no network, no TLS and no
Litestream in the loop, the database holds a few dozen rows, and the numbers
move with the runner. `BenchmarkHealth` is the floor — routing and serialisation
with no database work — so everything above it is roughly 0.6–1.4 ms of actual
query and transaction cost — an order of magnitude under the ~50 ms a tap should
take to feel instant, before the network is added.

`BenchmarkAddShot` undoes its own write between iterations, untimed. Without
that it would be timing a response body that grows with `b.N`, since the route
returns the hole with every shot on it — an easy way to produce a number that
looks like a regression and is not.

The table above measures the *generated* endpoints, which is the right
baseline for "is PocketBase fast" and a different question from "is the app
fast" — the frontend goes through the custom routes. `performance_report.md`
measures those, and the benchmarks for them are in `bench_test.go` alongside
these.

A second thing worth measuring beyond raw timing: how many *queries* a
request issues. Timings on a small fixture cannot see a per-record read.
That is asserted rather than benchmarked, in `perf_test.go`, by counting
statements at two data sizes and requiring the counts to match.
