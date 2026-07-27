# PocketBase API surface vs. the GolfTrack contract

What PocketBase generates for the collections in `pb_schema.json`, how it lines
up with the REST contract the frontend uses today, and what has to be built to
close the gap.

Written in Phase 1 (#122) from a live 0.39.9 instance, and updated in Phase 3
(#124) once the custom routes existed and Phase 4 (#125) once sign-in did. The
formal parity suite is Phase 5 (#126); this is the design input for it.

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

Six differences between the generated responses and the current contract. Each
needs a decision in Phase 5 (#126) / Phase 7 (#128) — flagging them here so the
choice is deliberate rather than discovered late. Phase 3 closed three of them
for the custom routes; the status line on each records where it stands.

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

One field is worth calling out: `total_par` on a course *is* returned by the
generated endpoints, in snake_case, because it is derived per response through
`OnRecordEnrich` rather than being a column. If gap 1 is closed by renaming, it
renames with the rest.

**2. Unset numbers come back as `0`, not `null`.** A fresh in-progress round
returns `"total_strokes": 0, "total_par": 0, "relative_to_par": 0`, where the
current API returns `null` for all three. This one is a live bug risk rather
than cosmetics: a round that has not been completed would render as "even par"
instead of blank. Unset dates behave the same way, returning `""` rather than
`null` for `finished_at`.

**3. Record ids are 15-character strings, not integers.** `IdOut` is typed
`id: int`, and the frontend builds URLs like `/rounds/12/play`. Phase 6 (#127)
has to decide whether to carry the Django integer ids across as a separate
column or accept string ids and update every consumer.

**4. Relations are ids plus `expand`, not inline nested objects.** `RoundDetailOut`
nests `course` (with its `holes`) and `holes` (with their `shots`) directly.
PocketBase returns `"course": "yceuliutpkhlhls"` and puts the record under
`expand.course` only when asked. Nesting depth is also limited, so
round → holes → shots may need either multiple requests or a custom route that
assembles the payload.

**5. Error bodies.** *Closed on the custom routes; open on the generated ones.*
The contract is `{"error": "<message>"}`; PocketBase returns
`{"data": {…}, "message": "…", "status": 400}`. `internal/apierr` writes
responses in the contract's shape and every custom route returns its failures
through it, so those are covered — built-in validation failures on the generated
endpoints are not.

**6. Status codes for constraint violations.** *Closed on the custom routes.*
Django returns 409 for "a round is already in progress" and "round is already
completed". A unique-index violation on a generated endpoint surfaces as 400.
The custom routes return 409 explicitly, including when the conflict is only
detected as a lost race — `rounds.Create` re-reads the player's in-progress
round after a failed save so that the partial unique index is reported as the
conflict it is rather than as a 400. `routes_test.go` asserts each of the three
409s.

Also worth noting, though not a gap in the same sense: `GET /api/health` returns
`{"message": "API is healthy.", "code": 200, "data": {}}` rather than
`{"status": "ok"}`. Phase 9 (#130) wires up the deployment health check and
should confirm which shape the probe expects.

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

This is a seventh parity gap on top of the six above, the only one Phase 2
introduces — and it is *closed on the custom routes*, which are all bound with
`apis.RequireAuth()`. `TestCustomRoutesRequireAuth` walks every one of them
anonymously and asserts the `401`. The generated endpoints still behave as the
table describes, so a frontend that reaches them directly sees the old codes;
that remainder is Phase 5 (#126) work.

Ownership on the custom routes is a related note rather than a gap: collection
API rules do not apply to hook code, so the handlers enforce it themselves,
and they do it the way Django does — another player's round is `404 {"error":
"Round not found"}`, never `403`.
