# Hook and business-logic architecture

How GolfTrack's domain rules are expressed in PocketBase, and the constraints
that shape that. Written in Phase 1 (#122); the modules it describes were built
in Phase 3 (#124).

The reference implementation throughout is the Django service layer —
`rounds/services.py`, `courses/services.py`, `rounds/scoring.py` — which is
itself a port of the earlier Next.js `lib/`. Behaviour should not change again
in this migration.

## The runtime: PocketBase as a Go framework

PocketBase is consumed as a Go library (`github.com/pocketbase/pocketbase`),
not as a downloaded binary. `main.go` builds to a **single portable
executable** that embeds:

- the PocketBase server and its standard CLI (`serve`, `superuser`, …),
- the collection schema (`pb_schema.json`, via `go:embed`), reconciled into
  the database at every startup,
- the GolfTrack hooks, compiled in as ordinary Go packages.

This was an explicit owner decision (reversing the migration plan's original
"JavaScript hooks first" choice): one artifact to build, ship and version, no
separate binary download, no interpreted hook layer, and domain logic that is
unit-testable with `go test` and type-checked against the real PocketBase
APIs at compile time.

Deployment implication for Phase 9 (#130): the container builds this package
and ships the one binary; the embedded schema sync already gives it the
deterministic startup path the phase needs.

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
    ├── records/         record lookups the domain packages share
    └── hooks/
        ├── hooks.go     Register() — the only place hooks are bound
        ├── users.go     users.role field default
        └── domain/      one package per aggregate
            ├── courses/     course and hole validation, derived total_par
            ├── rounds/      round lifecycle + its custom routes
            ├── roundholes/  hole initialisation, the stroke cache, the hole payload
            ├── shots/       shot lifecycle + the nested shot routes
            └── scoring/     calculate_round_totals (pure, no hooks)
```

`schema.go` holds the embed rather than `main.go` so that `syncSchema` is a
callable seam: the tests build a bare PocketBase app and apply the committed
schema through the same function the binary runs at startup.

`internal/hooks.Register` — called once from `main.go` — is deliberately the
single registration point. Domain packages expose a `Register(app core.App)`
function and never bind hooks as an import side effect, so registration order
is an explicit, reviewable list rather than a consequence of import order.

Phase 1 put the collection vocabulary and the error helpers *inside*
`internal/hooks`. Phase 3 moved them down into `internal/collections` and
`internal/apierr`, because the single registration point means `internal/hooks`
imports every domain package, and the domain packages need both — the
dependency cannot run in both directions. `internal/records` was added in the
same move for the lookups more than one domain package needs, so that
"a round, for its owner" is written once.

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
| A user only reaches their own rounds, holes and shots | collection API rules (Phase 2, #123) |
| Only admins write courses; only owners write rounds | collection API rules |
| `role` is not self-assignable | `users` update rule guards `@request.body.role` |

The rules themselves, and the reasoning behind each, are in `README.md` under
"Access rules"; `acl_test.go` asserts them over HTTP.

Worth stating explicitly, because it shapes everything below: **API rules do not
apply to hook code.** They gate the generated endpoints and PocketBase's own
request handlers. A hook or custom route that goes through `app.Save`, `app.Delete`
or `RunInTransaction` writes as the application, so the rules in the table above
protect the *client*, not the domain logic — every invariant Phase 3 implements
still has to be enforced in the hook itself.

### Enforced by a hook (Phase 3)

Where each rule ended up:

| Rule | Package | Bound to |
|---|---|---|
| Course snapshotting | `rounds` | `POST /api/rounds/` |
| Play-mode validity | `rounds` | `POST /api/rounds/` |
| Course hole-set validity | `courses` + `rounds` | per record on write; as a set at round creation |
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
`current_hole` starts at the lowest selected hole number. Django:
`create_round`.

**Stroke cache.** `round_holes.strokes` must equal that hole's shot count,
and must be updated in the same transaction as the shot insert or delete.
Django keeps this in `add_shot` / `undo_last_shot` / `delete_shot`.

**Shot renumbering.** Deleting a shot from the middle of a hole decrements
`shot_number` for every later shot on that hole, so the sequence stays
gap-free. Django does this as a single `UPDATE ... SET shot_number =
shot_number - 1 WHERE shot_number > n`, which is safe under the unique index
because it rewrites rows in ascending order into vacated slots. A naive
per-record loop would collide with the index on the first row, so the Go
implementation issues the same single UPDATE (`app.DB().NewQuery`), and
`TestDeleteShotRenumbersSubsequentShots` asserts the resulting order — the
surviving shots keep their clubs, not just their count.

**Undo semantics.** Remove only the highest-numbered shot on the given hole;
no renumbering needed, and a no-op when the hole has no shots.

**Play-mode validity.** `front9` and `back9` are only valid on 18-hole
courses. Django: `create_round`.

**Exact `hole_count` values.** The schema bounds `hole_count` to 9–18; the
actual rule is "9 or 18". Django: `CourseIn.hole_count: Literal[9, 18]`.

**Course hole-set validity.** A course's holes must number exactly
`hole_count`, be unique, and run sequentially from 1. Django:
`CourseIn.validate_holes`, over one payload — `POST /api/courses/` carries the
course and its holes together. PocketBase splits them across two collections and
two requests, so the rule is split too. Uniqueness is the
`(course, hole_number)` index; `1 ≤ hole_number ≤ course.hole_count` is checked
on every `course_holes` write, and with the index that pins a full set to
exactly `{1..hole_count}`. What only a whole-set check can catch is a course
that is *missing* holes, which in PocketBase is a reachable state, so
`courses.ValidateHoleSet` runs at round creation — the first point that reads
the set as a whole, and the point where an incomplete course would otherwise be
snapshotted into a round.

**Round is mutable only while in progress.** Every shot and current-hole
mutation rejects with 409 once the round is completed. Django raises
`ConflictError("Round is already completed")` in each service function.

**Completion totals.** On completing a round, sum `par` and `strokes` across
its holes into `total_par`, `total_strokes` and `relative_to_par`, set
`status` and `finished_at`. Django: `complete_round` +
`calculate_round_totals`.

**Derived `total_par` on courses.** A Django property, not a column; computed
per response. `OnRecordEnrich` is where PocketBase makes that possible: it runs
on serialization, so the value appears on the generated list and view endpoints
as well as anywhere a custom route returns a course.

**Admin role assignment.** Set `users.role` from the `ADMIN_EMAILS`
environment variable on OAuth login, via `OnRecordAuthWithOAuth2Request`.
Deferred to Phase 4 (#125).

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

The Django implementation additionally takes a row lock (`select_for_update`)
on the `RoundHole` before computing the next `shot_number`. PocketBase's
SQLite connection serialises writes, so the equivalent protection comes from
doing the read and the write inside one transaction; the unique index on
`(round_hole, shot_number)` is the backstop that turns a lost race into a
rejected write rather than a duplicate. `concurrency_test.go` asserts exactly
that: never two shots sharing a number, never a stroke cache out of step with
the shots it counts, and a *rejection* rather than a duplicate when two writers
collide.

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

PocketBase's generated CRUD endpoints do not cover the verb-like operations in
the current API — completing a round, undoing a shot. Those are registered on
the router inside `OnServe` and delegate to a domain package. `API.md` lists
which endpoints are generated and which had to be written.

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
`records.FindRound(app, id, userID)`, which collapses ownership into the lookup
the way Django's `.get(pk=..., user=user)` does: another player's round is *not
found*, never *forbidden*.

Handlers return errors rather than writing them; `apierr.Handler` turns an
`*apierr.Error` into `{"error": "<message>"}` with its status, and anything else
into a logged 500. That split is what lets the same domain function be called
from inside a transaction, where there is no `RequestEvent` to write to yet.

## Testing

Phase 3 covers hook behaviour; Phase 5 (#126) covers endpoint parity against
the Django contract. Because the hooks are ordinary Go code, Phase 3 gets two
layers:

- pure logic (scoring, renumbering arithmetic) as plain `go test` unit tests;
- hook behaviour against a real database via PocketBase's `tests` package
  (`tests.NewTestApp`), which runs the full app core in-process.

Phase 2 (#123) built the second layer already, for the access rules — see
`testapp_test.go` for the seeded-app harness and fixture builders, and
`README.md` under "Tests" for how it is wired. Phase 3 extended those rather
than starting a second harness: `domain_test.go`, `routes_test.go` and
`concurrency_test.go` all seed through the same `newTestApp`, and
`routes_test.go` reuses the fresh-app-per-scenario pattern `tests.ApiScenario`
requires.

The existing Django tests under `tests/` — `test_services.py` and
`test_concurrency.py` in particular — are the specification for both. They
encode the behaviour this migration is required to preserve, and porting them
was cheaper than rewriting the rules from this document.
