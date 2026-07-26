# PocketBase API surface vs. the GolfTrack contract

What PocketBase generates for the collections in `pb_schema.json`, how it lines
up with the REST contract the frontend uses today, and what has to be built to
close the gap.

Written in Phase 1 (#122) from a live 0.39.9 instance. The formal parity suite
is Phase 5 (#126); this is the design input for it.

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
| `POST /api/rounds/` | custom route — creation snapshots holes |
| `GET /api/rounds/in-progress` | `GET …/records?filter=(status='in_progress')&perPage=1` |
| `GET /api/rounds/{id}` | `GET …/records/{id}?expand=…` |
| `POST /api/rounds/{id}/complete` | **custom route** |
| `POST /api/rounds/{id}/cancel` | `DELETE …/records/{id}`, or a custom route for the `{id, cancelled}` body |
| `PATCH /api/rounds/{id}/current-hole` | `PATCH …/records/{id}` with validation |
| `POST /api/rounds/{id}/holes/{n}/shots` | **custom route** — stroke cache |
| `POST /api/rounds/{id}/holes/{n}/undo` | **custom route** |
| `PATCH /api/rounds/{id}/holes/{n}/shots/{shotId}` | `PATCH /api/collections/shots/records/{id}` |
| `DELETE /api/rounds/{id}/holes/{n}/shots/{shotId}` | **custom route** — renumbering |

The rows marked *custom route* are the ones where the generated CRUD is not
enough on its own, because a single request has to touch more than one
collection atomically. `ARCHITECTURE.md` covers what each does.

Note that the nested paths carry a hole number the generated endpoints have no
concept of — `POST /api/rounds/12/holes/3/shots` addresses a shot by
`(round, hole_number)`, where PocketBase addresses it by `round_hole` id. Custom
routes have to resolve that, and it is also a lookup the frontend would
otherwise have to make itself.

## Parity gaps

Six differences between the generated responses and the current contract. Each
needs a decision in Phase 5 (#126) / Phase 7 (#128) — flagging them here so the
choice is deliberate rather than discovered late.

**1. Field naming.** The API returns camelCase (`holeCount`, `isArchived`,
`playMode`, `currentHole`, `totalStrokes`, `relativeToPar`); PocketBase returns
whatever the field is called, which here is snake_case. The collections are
named to match the *Django models*, per this phase's brief, mirroring Django's
own split between snake_case model fields and a camelCase serialisation layer
(`core/schemas.py`). Two ways to close it: rename the schema fields to camelCase
and give up model parity, or transform in the response layer. Custom routes can
do the latter for free; generated CRUD endpoints cannot, which argues for
routing the frontend through custom routes wherever the shape matters.

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

**5. Error bodies.** The contract is `{"error": "<message>"}`; PocketBase returns
`{"data": {…}, "message": "…", "status": 400}`. `internal/hooks/errors.go`
already writes responses in the contract's shape, so custom routes are covered —
built-in validation failures on the generated endpoints are not.

**6. Status codes for constraint violations.** Django returns 409 for "a round is
already in progress" and "round is already completed". A unique-index violation
on a generated endpoint surfaces as 400. Custom routes can return 409 explicitly
via `errors.go`.

Also worth noting, though not a gap in the same sense: `GET /api/health` returns
`{"message": "API is healthy.", "code": 200, "data": {}}` rather than
`{"status": "ok"}`. Phase 9 (#130) wires up the deployment health check and
should confirm which shape the probe expects.

## Access rules

The five new collections have `null` API rules, so every generated endpoint on
them is superuser-only — an unauthenticated list returns:

```
403 {"data":{},"message":"Only superusers can perform this action.","status":403}
```

`users` keeps PocketBase's defaults. Phase 2 (#123) replaces both.
