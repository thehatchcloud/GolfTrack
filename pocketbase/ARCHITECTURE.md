# Hook and business-logic architecture

How GolfTrack's domain rules are expressed in PocketBase, and the constraints
that shape that.

## The runtime: PocketBase as a Go framework

PocketBase is consumed as a Go library (`github.com/pocketbase/pocketbase`),
not as a downloaded binary. `main.go` builds to a **single portable
executable** that embeds:

- the PocketBase server and its standard CLI (`serve`, `superuser`, …),
- the collection schema (`pb_schema.json`, via `go:embed`), reconciled into
  the database at every startup,
- the GolfTrack hooks, compiled in as ordinary Go packages.

This is a deliberate choice: one artifact to build, ship and version, no
separate binary download, no interpreted hook layer, and domain logic that is
unit-testable with `go test` and type-checked against the real PocketBase
APIs at compile time. The container builds this package and ships the one
binary; the embedded schema sync gives it a deterministic startup path.

## Layout

```
pocketbase/
├── main.go              application entrypoint: schema sync + hook registration
├── schema.go            go:embed of pb_schema.json + the sync itself
├── go.mod / go.sum      module definition, PocketBase version pin
├── pb_schema.json       collection schema and access rules, embedded into the binary
└── internal/
    ├── collections/     collection names/ids, field names and enum values
    ├── apierr/          the {"error": …} contract: typed errors + the route wrapper
    ├── authenv/         the authentication configuration, read from the environment
    ├── records/         record lookups the domain packages share
    ├── hooks/
    │   ├── hooks.go     Register() — the only place hooks are bound
    │   ├── users.go     users.role field default
    │   ├── authconfig.go  OAuth2 providers + password login, applied on OnServe
    │   ├── adminrole.go   users.role from ADMIN_EMAILS, on OAuth2 sign-in
    │   └── domain/      one package per aggregate
    │       ├── courses/     course and hole validation, writes, derived total_par
    │       ├── rounds/      round lifecycle + its custom routes
    │       ├── roundholes/  hole initialisation, the stroke cache, the hole payload
    │       ├── shots/       shot lifecycle + the nested shot routes
    │       └── scoring/     calculate_round_totals (pure, no hooks)
    └── web/             the frontend: pages, templates, static assets
```

`schema.go` holds the embed rather than `main.go` so that `syncSchema` is a
callable seam: the tests build a bare PocketBase app and apply the committed
schema through the same function the binary runs at startup.

`internal/hooks.Register` — called once from `main.go` — is deliberately the
single registration point. Domain packages expose a `Register(app core.App)`
function and never bind hooks as an import side effect, so registration order
is an explicit, reviewable list rather than a consequence of import order.

The collection vocabulary and error helpers live in `internal/collections`
and `internal/apierr`, one level above `internal/hooks`: the single
registration point means `internal/hooks` imports every domain package, and
the domain packages need both — the dependency cannot run in both
directions. `internal/records` holds the lookups more than one domain
package needs, so that "a round, for its owner" is written once.

## Where each rule lives

Some of the domain rules are expressible in the schema and are already
enforced by `pb_schema.json`; the rest need hooks. Splitting them out is the
point of this document.

### Enforced by the schema today

| Rule | Mechanism |
|---|---|
| One in-progress round per user | partial unique index `idx_rounds_one_in_progress_per_user` |
| Sequential shot numbering is gap-free *and* unique | unique index `(round_hole, shot_number)` |
| One row per hole in a round / course | unique indexes `(round, hole_number)`, `(course, hole_number)` |
| Par 3–6, hole numbers 1–18, club ≤ 32 chars | number/text field bounds |
| Status and play mode value sets | `select` field values |
| Deleting a round removes its holes and shots | `cascadeDelete: true` on the relations |
| A course with rounds cannot be deleted | `cascadeDelete: false` on a required `rounds.course` |
| A user only reaches their own rounds, holes and shots | collection API rules |
| Only admins write courses; only owners write rounds | collection API rules |
| `role` is not self-assignable | `users` update rule guards `@request.body.role` |

The rules themselves, and the reasoning behind each, are in `README.md` under
"Access rules"; `acl_test.go` asserts them over HTTP.

Worth stating explicitly, because it shapes everything below: **API rules do not
apply to hook code.** They gate the generated endpoints and PocketBase's own
request handlers. A hook or custom route that goes through `app.Save`, `app.Delete`
or `RunInTransaction` writes as the application, so the rules in the table above
protect the *client*, not the domain logic — every invariant below still has
to be enforced in the hook itself.

### Enforced by a hook

Where each rule ended up:

| Rule | Package | Bound to |
|---|---|---|
| Course snapshotting | `rounds` | `POST /api/rounds/` |
| Play-mode validity | `rounds` | `POST /api/rounds/` |
| Course hole-set validity | `courses` + `rounds` | per record on write; as a whole payload on `POST`/`PUT /api/courses`; as a set at round creation |
| Exact `hole_count` | `courses` | `OnRecordCreate` / `OnRecordUpdate` |
| Stroke cache | `shots` → `roundholes` | `OnRecordCreate` / `OnRecordDelete` on `shots` |
| Shot renumbering | `shots` | `OnRecordDelete` on `shots` |
| Undo semantics | `shots` | `POST …/holes/{n}/undo` |
| Round is mutable only while in progress | `rounds`, `shots` | `OnRecordUpdate` on `rounds`, the shot hooks, and each route |
| Completion totals | `rounds` + `scoring` | `POST …/complete` |
| Derived `total_par` on courses | `courses` | `OnRecordEnrich` |

The invariants that a *client* could otherwise break on its own — the stroke
cache, the numbering, "a completed round is immutable" — are bound to record
hooks rather than living only in the route handlers. The round's owner is
allowed to write `shots` and `rounds` through the generated endpoints, so a rule
enforced only on a custom route would be one `PATCH` away from being bypassed.
Everything that is inherently a whole-request operation — snapshotting a course,
summing totals — lives on the route, because there is no single record write to
hang it on.

**Course snapshotting.** On round creation, copy `par` from each
`course_holes` row into a new `round_holes` row, filtered by play mode:
`front9` selects holes 1–9, `back9` selects 10–18, `full` selects all.
`current_hole` starts at the lowest selected hole number.

**Stroke cache.** `round_holes.strokes` must equal that hole's shot count,
and must be updated in the same transaction as the shot insert or delete.

**Shot renumbering.** Deleting a shot from the middle of a hole decrements
`shot_number` for every later shot on that hole, so the sequence stays
gap-free, via a single `UPDATE ... SET shot_number = shot_number - 1 WHERE
shot_number > n` (`app.DB().NewQuery`). This is safe under the unique index
because it rewrites rows in ascending order into vacated slots; a naive
per-record loop would collide with the index on the first row.
`TestDeleteShotRenumbersSubsequentShots` asserts the resulting order — the
surviving shots keep their clubs, not just their count.

**Undo semantics.** Remove only the highest-numbered shot on the given hole;
no renumbering needed, and a no-op when the hole has no shots.

**Play-mode validity.** `front9` and `back9` are only valid on 18-hole
courses.

**Exact `hole_count` values.** The schema bounds `hole_count` to 9–18; the
actual rule is "9 or 18", checked in the hook.

**Course hole-set validity.** A course's holes must number exactly
`hole_count`, be unique, and run sequentially from 1. `POST /api/courses/`
carries the course and its holes together as one payload, but PocketBase
splits them across two collections and two requests, so the rule is split
too. Uniqueness is the `(course, hole_number)` index; `1 ≤ hole_number ≤
course.hole_count` is checked on every `course_holes` write, and with the
index that pins a full set to exactly `{1..hole_count}`. What only a
whole-set check can catch is a course that is *missing* holes, which in
PocketBase is a reachable state, so `courses.ValidateHoleSet` runs at round
creation — the first point that reads the set as a whole, and the point
where an incomplete course would otherwise be snapshotted into a round.

**Round is mutable only while in progress.** Every shot and current-hole
mutation rejects with 409 once the round is completed.

**Completion totals.** On completing a round, sum `par` and `strokes` across
its holes into `total_par`, `total_strokes` and `relative_to_par`, set
`status` and `finished_at`.

**Derived `total_par` on courses.** A derived property, not a column;
computed per response. `OnRecordEnrich` is where PocketBase makes that
possible: it runs on serialization, so the value appears on the generated
list and view endpoints as well as anywhere a custom route returns a course.

**Admin role assignment.** Set `users.role` from the `ADMIN_EMAILS`
environment variable on OAuth login, via `OnRecordAuthWithOAuth2Request`
(`internal/hooks/adminrole.go`). It revokes as well as grants, and an empty
list is a no-op rather than a mass demotion. The same hook discards the
caller-supplied `createData` of an OAuth2 sign-in, which is otherwise a way
to write `role`, `password` or `email` onto a brand-new account. Full
reasoning: `AUTH.md`.

**Authentication configuration.** OAuth client credentials are secrets and the
password-login switch differs per environment, so neither can live in
`pb_schema.json`. The committed schema holds the closed baseline — no
providers, OAuth2 disabled, password authentication disabled — and
`internal/hooks/authconfig.go` reconciles the `users` collection to the
environment on `OnServe`, in the same one-authoritative-source-applied-on-boot
shape as the schema sync. It must stay bound *after* that sync, which imports
the collection wholesale and would otherwise reset the options it just wrote.

## Transactions

Every rule above that touches more than one record has to be atomic — the
stroke cache in particular is a cache, and is only correct if it cannot be
observed out of step with the shots it counts. In Go hooks that means
`RunInTransaction`:

```go
err := app.RunInTransaction(func(txApp core.App) error {
    // all reads and writes go through txApp, never the outer app
    return nil
})
```

PocketBase's SQLite connection serialises writes, so the protection against a
lost race comes from doing the read and the write inside one transaction; the
unique index on `(round_hole, shot_number)` is the backstop that turns a lost
race into a rejected write rather than a duplicate. `concurrency_test.go`
asserts exactly that: never two shots sharing a number, never a stroke cache
out of step with the shots it counts, and a *rejection* rather than a
duplicate when two writers collide.

One PocketBase detail that shapes where hook work goes: `OnRecordCreate` and
`OnRecordDelete` run their write as `e.Next()`, so a hook can read the
post-write state by continuing after it, and does so inside whatever transaction
the caller opened. The `AfterCreateSuccess` / `AfterDeleteSuccess` hooks are the
wrong place for cache maintenance — they are deferred until the transaction has
already completed. Record deletes are wrapped in a transaction by PocketBase
itself, and the parent record is removed *before* its cascade references, which
is why the shot hooks treat a missing round or hole as "already gone" rather
than as an error: cancelling a round legitimately reaches them that way.

## Custom routes

PocketBase's generated CRUD endpoints do not cover the verb-like operations
GolfTrack needs — completing a round, undoing a shot. Those are registered on
the router inside `OnServe` and delegate to a domain package. `API.md` lists
which endpoints are generated and which had to be written.

A second category of custom route exists purely for read shape: `GET
/api/courses`, `/api/courses/{id}`, `GET /api/rounds`, `/api/rounds/in-progress`
and `/api/rounds/{id}` sit next to the write routes in `courses/routes.go`
and `rounds/routes.go`, each backed by an `Out`/`DetailOut` builder
(`courses/out.go`, `rounds/out.go`) that walks the record(s) and produces the
camelCase, nested-relation, null-aware JSON body the generated endpoints
cannot.

```go
app.OnServe().BindFunc(func(se *core.ServeEvent) error {
    se.Router.POST("/api/rounds/{id}/complete", apierr.Handler(complete)).
        Bind(apis.RequireAuth())
    return se.Next()
})
```

`apis.RequireAuth()` is what makes an anonymous call return 401 rather than the
200-with-an-empty-list or 404 the rule-filtered generated endpoints give. Past
that point the collection API rules no longer help — hook code writes as the
application — so every handler resolves its round through
`records.FindRound(app, id, userID)`, which collapses ownership into the
lookup: another player's round is *not found*, never *forbidden*.

Course administration is the one place where a *role* decides the answer
rather than ownership. `POST /api/courses/` and `PUT /api/courses/{id}`
(`courses/write.go`) add `requireAdmin` on top of `apis.RequireAuth()`:
`role = ADMIN`, or a PocketBase superuser, and otherwise `403 {"error":
"Forbidden"}`. Both run `CourseIn`'s validators before opening their
transaction, so a course and its holes commit together or not at all — the
invariant `courses.ValidateHoleSet` would otherwise only catch at the next
round creation.

Handlers return errors rather than writing them; `apierr.Handler` turns an
`*apierr.Error` into `{"error": "<message>"}` with its status, and anything else
into a logged 500. That split is what lets the same domain function be called
from inside a transaction, where there is no `RequestEvent` to write to yet.

## The frontend

`internal/web` holds the page routes, their templates and the static assets,
all embedded in the same binary and served by the same process. It sits on
top of the layers above rather than beside them:

- **Pages read through the domain packages' exported queries**
  (`courses.List`, `rounds.InProgress`, `rounds.Detail`, …), which are the same
  functions the JSON routes call. A page cannot answer differently from the
  endpoint behind it, because there is only one query.
- **Pages never write.** Every mutation is an API call from the page's own
  JavaScript carrying the bearer token, so the write paths documented above are
  the only write paths, and the cookie can never authorise a write.
- **The cookie gates renders only**, and only the token half of it is trusted —
  `internal/web/auth.go` verifies the JWT and re-reads the user record, so the
  client-writable `record` blob in the cookie decides nothing.

`README.md` § "Frontend" covers the routes, the asset build and how to run it.

## Testing

Hook behaviour is tested at two layers, since the hooks are ordinary Go code:

- pure logic (scoring, renumbering arithmetic) as plain `go test` unit tests;
- hook behaviour against a real database via PocketBase's `tests` package
  (`tests.NewTestApp`), which runs the full app core in-process.

`testapp_test.go` holds the seeded-app harness and fixture builders shared
across the suite — see `README.md` under "Tests" for how it is wired.
`domain_test.go`, `routes_test.go` and `concurrency_test.go` all seed through
the same `newTestApp`, and `routes_test.go` uses the fresh-app-per-scenario
pattern `tests.ApiScenario` requires.
