# Hook and business-logic architecture

How GolfTrack's domain rules will be expressed in PocketBase, and the
constraints that shape that. Written in Phase 1 (#122); the modules it
describes are built in Phase 3 (#124).

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
└── internal/hooks/
    ├── hooks.go         Register() — the only place hooks are bound
    ├── collections.go   collection names/ids, field names and enum values
    ├── users.go         users.role field default
    ├── errors.go        error-contract helpers for custom routes
    └── domain/          Phase 3 domain packages land here
```

`schema.go` holds the embed rather than `main.go` so that `syncSchema` is a
callable seam: the tests build a bare PocketBase app and apply the committed
schema through the same function the binary runs at startup.

`internal/hooks.Register` — called once from `main.go` — is deliberately the
single registration point. Domain packages expose a `Register(app core.App)`
function and never bind hooks as an import side effect, so registration order
is an explicit, reviewable list rather than a consequence of import order.

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

### Needs a hook (Phase 3)

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
per-record loop would collide with the index on the first row — the Go
implementation should issue the same single UPDATE (`app.DB().NewQuery`), and
the Phase 3 tests should cover the ordering explicitly.

**Undo semantics.** Remove only the highest-numbered shot on the given hole;
no renumbering needed, and a no-op when the hole has no shots.

**Play-mode validity.** `front9` and `back9` are only valid on 18-hole
courses. Django: `create_round`.

**Exact `hole_count` values.** The schema bounds `hole_count` to 9–18; the
actual rule is "9 or 18". Django: `CourseIn.hole_count: Literal[9, 18]`.

**Course hole-set validity.** A course's holes must number exactly
`hole_count`, be unique, and run sequentially from 1. Django:
`CourseIn.validate_holes`.

**Round is mutable only while in progress.** Every shot and current-hole
mutation rejects with 409 once the round is completed. Django raises
`ConflictError("Round is already completed")` in each service function.

**Completion totals.** On completing a round, sum `par` and `strokes` across
its holes into `total_par`, `total_strokes` and `relative_to_par`, set
`status` and `finished_at`. Django: `complete_round` +
`calculate_round_totals`.

**Derived `total_par` on courses.** A Django property, not a column; computed
per response.

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
rejected write rather than a duplicate. Phase 3's concurrency tests should
assert that a rejected concurrent `add_shot` is the failure mode, not a
duplicated shot number.

## Custom routes

PocketBase's generated CRUD endpoints do not cover the verb-like operations in
the current API — completing a round, undoing a shot. Those are registered on
the router inside `OnServe` and delegate to a domain package. `API.md` lists
which endpoints are generated and which have to be written.

```go
app.OnServe().BindFunc(func(se *core.ServeEvent) error {
    se.Router.POST("/api/rounds/{id}/complete", func(e *core.RequestEvent) error {
        return rounds.Complete(e)
    }).Bind(apis.RequireAuth())
    return se.Next()
})
```

Route handlers write failures through `internal/hooks/errors.go`, which keeps
them on the existing `{"error": "<message>"}` contract.

## Testing

Phase 3 covers hook behaviour; Phase 5 (#126) covers endpoint parity against
the Django contract. Because the hooks are ordinary Go code, Phase 3 gets two
layers:

- pure logic (scoring, renumbering arithmetic) as plain `go test` unit tests;
- hook behaviour against a real database via PocketBase's `tests` package
  (`tests.NewTestApp`), which runs the full app core in-process.

Phase 2 (#123) built the second layer already, for the access rules — see
`testapp_test.go` for the seeded-app harness and fixture builders, and
`README.md` under "Tests" for how it is wired. Phase 3 should extend those
rather than start a second harness; in particular, `tests.ApiScenario` is how a
custom route gets tested end to end, and the note there about needing a fresh
app per scenario applies to any new suite.

The existing Django tests under `rounds/` and `courses/` are the
specification for both — they encode the behaviour this migration is required
to preserve, and porting them is cheaper than rewriting the rules from this
document.
