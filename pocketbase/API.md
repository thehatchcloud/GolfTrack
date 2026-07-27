# PocketBase API surface vs. the GolfTrack contract

What PocketBase generates for the collections in `pb_schema.json`, how it lines
up with the REST contract the frontend uses today, and what has to be built to
close the gap.

Written in Phase 1 (#122) from a live 0.39.9 instance, and updated in Phase 3
(#124) once the custom routes existed, Phase 4 (#125) once sign-in did, and
Phase 5 (#126) once the parity suite measured the gaps rather than inferring
them. `parity_test.go` is that suite; every gap below names the test that pins
it.

The current contract is defined by `config/api.py`, `courses/api.py` and
`rounds/api.py` — itself a port of the Next.js route handlers it replaced, so
the shapes below have survived two rewrites and the frontend depends on them.

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

Which of those actually answer is decided by Phase 4's configuration, applied
from the environment at startup — `AUTH.md` has the detail. The four the
frontend uses:

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/collections/users/auth-methods` | which providers are enabled, and whether password login is; unauthenticated |
| `POST` | `/api/collections/users/auth-with-oauth2` | the sign-in; answers `{token, record, meta}` |
| `POST` | `/api/collections/users/auth-with-password` | `403` unless `GOLFTRACK_ALLOW_PASSWORD_LOGIN` is on |
| `POST` | `/api/collections/users/auth-refresh` | a fresh token for a still-valid one |

There is no equivalent of Django's `/accounts/login/` page or its session
cookie: the session is a JWT the client holds and sends as `Authorization`.
That is the one auth-shaped difference the frontend has to absorb, and it is
written up for Phase 7 (#128) in `AUTH.md` § "For the frontend".

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

## Mapping to the current contract

| Current endpoint | PocketBase equivalent |
|---|---|
| `GET /api/health` | `GET /api/health` — body differs (see below) |
| `GET /api/courses/` | `GET /api/collections/courses/records?expand=…` |
| `POST /api/courses/` | `POST /api/collections/courses/records` + N hole creates |
| `GET /api/courses/{id}` | `GET /api/collections/courses/records/{id}` |
| `PUT /api/courses/{id}` | `PATCH` + hole reconciliation |
| `GET /api/rounds/` | `GET /api/collections/rounds/records?filter=(status='completed')` |
| `POST /api/rounds/` | **built (Phase 3)** — same path; creation snapshots holes |
| `GET /api/rounds/in-progress` | `GET …/records?filter=(status='in_progress')&perPage=1` |
| `GET /api/rounds/{id}` | `GET …/records/{id}?expand=…` |
| `POST /api/rounds/{id}/complete` | **built (Phase 3)** — same path |
| `POST /api/rounds/{id}/cancel` | **built (Phase 3)** — same path, returning the `{id, cancelled}` body |
| `PATCH /api/rounds/{id}/current-hole` | **built (Phase 3)** — same path |
| `POST /api/rounds/{id}/holes/{n}/shots` | **built (Phase 3)** — same path; stroke cache |
| `POST /api/rounds/{id}/holes/{n}/undo` | **built (Phase 3)** — same path |
| `PATCH /api/rounds/{id}/holes/{n}/shots/{shotId}` | **built (Phase 3)** — same path |
| `DELETE /api/rounds/{id}/holes/{n}/shots/{shotId}` | **built (Phase 3)** — same path; renumbering |

The rows marked *built* are the ones where the generated CRUD was not enough on
its own, because a single request has to touch more than one collection
atomically, or because the path addresses a shot by `(round, hole_number)`.
`ARCHITECTURE.md` covers what each does; `routes_test.go` exercises them.

The `PATCH …/shots/{shotId}` row was originally listed as coverable by
`PATCH /api/collections/shots/records/{id}`. It is built as a custom route
anyway, so that the whole nested path space behaves consistently — the same
401 for an anonymous caller, the same `{"error": …}` body, the same
`(round, hole_number)` addressing — rather than one verb in the group behaving
like a PocketBase endpoint and the rest like GolfTrack ones.

The `GET` rows are still generated endpoints. Phase 3 built only what the
generated CRUD could not express; assembling `RoundDetailOut`-shaped read
responses is Phase 5/7 work.

Note that the nested paths carry a hole number the generated endpoints have no
concept of — `POST /api/rounds/12/holes/3/shots` addresses a shot by
`(round, hole_number)`, where PocketBase addresses it by `round_hole` id. Custom
routes have to resolve that, and it is also a lookup the frontend would
otherwise have to make itself.

## Parity gaps

Seven differences between the generated responses and the current contract.
Phase 5 (#126) measured each one and recorded a decision; the status line on
each says where it stands and which test in `parity_test.go` holds it there.

The tests for the still-open gaps are *characterisation* tests: they assert
PocketBase's behaviour as it is, not as the contract wants it. That is
deliberate. It means closing a gap in Phase 7 (#128) starts with a failing test
naming the gap, rather than with a silent change nobody notices until the
frontend renders an unfinished round as even par.

The through-line of the decisions below: **the frontend reads through custom
routes wherever the shape matters.** Four of the seven gaps (1, 2, 4, 5) close
for free on a route that owns its own response body, and none of them can close
on generated CRUD without giving something up. Phase 3 already built every
custom *write*; Phase 7 adds the reads.

| # | Gap | Decision | Pinned by |
|---|---|---|---|
| 1 | camelCase vs snake_case | Transform in custom routes; keep the schema on Django's model names | `TestGeneratedEndpointsReturnSnakeCase`, `TestCustomRoutesReturnCamelCase` |
| 2 | Unset numbers are `0`, not `null` | Carried to Phase 7 — needs a custom read route | `TestUnsetNumbersComeBackAsZero` |
| 3 | Ids are 15-char strings | Carried to Phase 6 (#127) with the data migration | `TestRecordIdsAreStrings` |
| 4 | Relations are ids plus `expand` | Resolved as *not* a blocker: the chain expands whole | `TestRelationsAreIdsPlusExpand` |
| 5 | Error bodies | Closed on custom routes; open on generated | `TestErrorBodyShapes` |
| 6 | 409 for constraint violations | Closed on custom routes; generated create stays 400 | `TestConstraintViolationStatusCodes` |
| 7 | Anonymous/unauthorised status codes | Closed on custom routes; frontend keys off the token, not 401 | `TestAnonymousCallerStatusCodes`, `TestUnauthorisedCallerStatusCodes` |

**1. Field naming.** *Closed on the custom routes; open on the generated ones.*
The API returns camelCase (`holeCount`, `isArchived`,
`playMode`, `currentHole`, `totalStrokes`, `relativeToPar`); PocketBase returns
whatever the field is called, which here is snake_case. The collections are
named to match the *Django models*, per Phase 1's brief, mirroring Django's
own split between snake_case model fields and a camelCase serialisation layer
(`core/schemas.py`). Two ways to close it: rename the schema fields to camelCase
and give up model parity, or transform in the response layer. Phase 3 took the
latter on the routes it built — they read `courseId`, `playMode`, `currentHole`
and return `holeNumber`, `shotNumber` — which argues for routing the frontend
through custom routes wherever the shape matters. Generated CRUD endpoints
cannot do it, so a course still lists as `hole_count` and `is_archived`.

*Phase 5 decision:* keep the schema on the model names and transform in the
route layer. Renaming would close this gap and only this gap, while breaking the
one-to-one mapping Phase 6's data migration reads from, and gap 2 would still
need a custom read route anyway — so the rename buys nothing that the route does
not already buy.

One field is worth calling out: `total_par` on a course *is* returned by the
generated endpoints, in snake_case, because it is derived per response through
`OnRecordEnrich` rather than being a column. If gap 1 is closed by renaming, it
renames with the rest.

**2. Unset numbers come back as `0`, not `null`.** *Open; carried to Phase 7.*
A fresh in-progress round
returns `"total_strokes": 0, "total_par": 0, "relative_to_par": 0`, where the
current API returns `null` for all three. This one is a live bug risk rather
than cosmetics: a round that has not been completed would render as "even par"
instead of blank. Unset dates behave the same way, returning `""` rather than
`null` for `finished_at`.

*Phase 5 decision:* it cannot be closed on the schema — a PocketBase number
field has no null — so the read path has to go through a route that can emit
one. That is the same route gap 1 wants, which is why the two are carried
together. Until it exists, a client must treat a round with
`status = "in_progress"` as having no score at all rather than reading the
zeroes.

**3. Record ids are 15-character strings, not integers.** *Open; carried to
Phase 6 (#127).* `IdOut` is typed
`id: int`, and the frontend builds URLs like `/rounds/12/play`. Phase 6
has to decide whether to carry the Django integer ids across as a separate
column or accept string ids and update every consumer. The custom routes already
return and accept the string form, so the decision is not deferrable past the
data migration.

**4. Relations are ids plus `expand`, not inline nested objects.** *Resolved:
not a blocker.* `RoundDetailOut`
nests `course` (with its `holes`) and `holes` (with their `shots`) directly.
PocketBase returns `"course": "yceuliutpkhlhls"` and puts the record under
`expand.course` only when asked.

This was written up in Phase 1 as possibly needing several requests, on the
assumption that expansion depth would not reach round → holes → shots. Phase 5
measured it and the assumption was wrong: `?expand=round_holes_via_round.
shots_via_round_hole` returns the whole two-level chain in one response, well
inside PocketBase's expansion depth cap. So the remaining difference is
placement and naming — the
data arrives under back-relation keys in snake_case rather than as `holes` and
`shots` inline — which is gap 1 again, and closes with it in the same custom
read route. No extra round trips, no separate design.

**5. Error bodies.** *Closed on the custom routes; open on the generated ones.*
The contract is `{"error": "<message>"}`; PocketBase returns
`{"data": {…}, "message": "…", "status": 400}`. `internal/apierr` writes
responses in the contract's shape and every custom route returns its failures
through it, so those are covered — built-in validation failures on the generated
endpoints are not.

*Phase 5 decision:* leave the generated endpoints as they are. Rewriting their
error bodies would mean a global response middleware that reshapes PocketBase's
own errors, including on the auth and admin-UI routes, where the shape is what
the SDK and the Admin UI expect. The frontend reads errors from the custom
routes, which match; anything it reaches directly (the collection reads) it has
to handle in PocketBase's shape, and Phase 7 does that in one place in the
client.

**6. Status codes for constraint violations.** *Closed on the custom routes.*
Django returns 409 for "a round is already in progress" and "round is already
completed". A unique-index violation on a generated endpoint surfaces as 400.
The custom routes return 409 explicitly, including when the conflict is only
detected as a lost race — `rounds.Create` re-reads the player's in-progress
round after a failed save so that the partial unique index is reported as the
conflict it is rather than as a 400. `routes_test.go` asserts each of the three
409s, and `TestConstraintViolationStatusCodes` asserts both ends of the same
conflict side by side: 409 through `POST /api/rounds/`, 400 through the
generated create.

*Phase 5 decision:* the generated create stays 400. That difference is the
concrete reason round creation is a custom route and the frontend must not post
round records directly.

Also worth noting, though not a gap in the same sense: `GET /api/health` returns
`{"message": "API is healthy.", "code": 200, "data": {}}` rather than
`{"status": "ok"}`. `TestHealthEndpointShape` asserts the PocketBase shape so
Phase 9 (#130) wires the deployment probe against something known rather than
discovering it from a failing container health check.

## Access rules

Set in Phase 2 (#123); the rule set and its rationale are in `README.md`, and
`acl_test.go` asserts each rule over HTTP. What matters for parity is the status
codes they produce, because they do not line up with the Django contract:

| Situation | Django | PocketBase |
|---|---|---|
| Unauthenticated list | `401 {"error": "Unauthorized"}` | `200 {"items":[],"totalItems":0}` — a list rule filters rather than gates |
| Unauthenticated view of an existing record | `401` | `404` |
| Authenticated but not permitted (e.g. a non-admin writing a course) | `403 {"error": "Forbidden"}` | `404` on update/delete, `400` on create |
| Another user's round | `404 {"error": "Round not found"}` | `404` — the one that already matches |

None of these leak data, and the 404-instead-of-403 choice is deliberate on
PocketBase's side: a response cannot be used to probe whether a record id
exists. But a frontend that keys off `401` to redirect to sign-in would see an
empty list instead.

This is the seventh parity gap, the only one Phase 2
introduces — and it is *closed on the custom routes*, which are all bound with
`apis.RequireAuth()`. `TestCustomRoutesRequireAuth` walks every one of them
anonymously and asserts the `401`. The generated endpoints still behave as the
table describes, and `TestAnonymousCallerStatusCodes` /
`TestUnauthorisedCallerStatusCodes` pin each row of it.

*Phase 5 decision:* the generated endpoints are not fronted. Wrapping them to
restore the `401` would mean overriding the list rules with a middleware that
duplicates them, and it would break the auth endpoints, which are
unauthenticated by definition. Instead the frontend decides "signed in?" from
the token it holds rather than from a status code, and treats a `404` on a
record it expected as "gone or not yours" — which is what Django's
`.get(pk=…, user=user)` already meant. The one code it must keep handling is the
`401` from the custom routes, which is the honest one: it means the token was
missing or expired.

Ownership on the custom routes is a related note rather than a gap: collection
API rules do not apply to hook code, so the handlers enforce it themselves,
and they do it the way Django does — another player's round is `404 {"error":
"Round not found"}`, never `403`. `TestCustomRoutesEnforceOwnership` walks the
whole custom surface as another player and `TestCustomRoutesAreNotOpenToAdmins`
walks it as an admin and as a superuser, because that is the one place a role
could plausibly override ownership and must not: `role = ADMIN` widens the
*read* rules on rounds, but Django's services scope on `user=user` with no role
branch, so an admin cannot score somebody else's round either. Every one of
those scenarios also asserts that the denied request wrote nothing.

## Pagination, filtering and sorting

Covered by `TestListPagination`, `TestListFiltering` and `TestListSorting`.
These are less a parity comparison than a record of what the generated endpoints
offer in place of the contract's fixed list endpoints, because the two do not
correspond: `GET /api/rounds/` takes `limit`/`page` and returns a bare JSON
array, where a PocketBase list takes `page`/`perPage` and returns the
`{items, page, perPage, totalItems, totalPages}` envelope.

What the tests establish:

- **Pagination.** `page`/`perPage` with the full envelope; a page past the end
  is `200` with `"items":[]` and the totals still present, not a `404`;
  `skipTotal=1` drops the counting query and reports `totalItems: -1`.
- **Filtering.** The contract's two round lists are filter expressions —
  `filter=(status='completed')` and
  `filter=(status='in_progress')&perPage=1` — over a list already narrowed by
  the rule, so a filter cannot widen access: asking for another player's round
  by id still returns `totalItems: 0`. A malformed filter is `400`, in
  PocketBase's error shape (gap 5).
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

Taking the Django side for comparison needs a live environment this repository's
CI does not provision (`make install` then `make dev`, and hit the same paths
with `hey` or `wrk`). It is deliberately not automated: the two stacks are not
running the same request in the same process, so a checked-in cross-stack ratio
would be measuring the harness. The value of the table above is as a *baseline
against itself* — a later PocketBase change that adds a query per request shows
up as a step in these numbers.
