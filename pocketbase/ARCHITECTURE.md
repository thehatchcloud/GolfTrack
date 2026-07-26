# Hook and business-logic architecture

How GolfTrack's domain rules will be expressed in PocketBase, and the
constraints that shape that. Written in Phase 1 (#122); the modules it describes
are built in Phase 3 (#124).

The reference implementation throughout is the Django service layer —
`rounds/services.py`, `courses/services.py`, `rounds/scoring.py` — which is
itself a port of the earlier Next.js `lib/`. Behaviour should not change again
in this migration.

## The runtime, and what it forces

PocketBase embeds a Goja JavaScript VM. Three properties of it determine the
layout of `hooks/`:

**Only `*.pb.js` files directly inside the hooks directory are auto-loaded**, in
alphabetical order. Files in subdirectories are invisible to the loader and are
reached through `require` instead.

**Hook callbacks run in a pooled runtime that does not share scope with the file
that registered them.** A handler cannot close over a variable defined at module
top level; it must `require` what it needs inside its own body. This is the
single most common source of confusing failures in PocketBase hooks, and it is
why every handler below starts with a `require` line.

**`require` needs an absolute path**, available as the `__hooks` global:

```js
const { NAMES } = require(`${__hooks}/lib/collections.js`);
```

## Layout

```
hooks/
├── main.pb.js   the only auto-loaded file; registers every hook
├── lib/         infrastructure, no domain knowledge
└── domain/      one module per aggregate, mirroring the Django services
```

`main.pb.js` is deliberately the only `*.pb.js` file. Splitting registration
across several auto-loaded files would make load order a function of filenames;
keeping one entrypoint makes it an explicit, reviewable list. Domain modules
export a `register()` function and never bind hooks as an import side effect.

### `lib/`

| Module | Contents |
|---|---|
| `collections.js` | Collection names and ids, plus the enum values (`ROUND_STATUS`, `PLAY_MODE`, `USER_ROLE`) that mirror the `select` fields in `pb_schema.json`. |
| `errors.js` | Constructors for `ApiError`s on the existing error contract — `{"error": "<message>"}` with 400/401/403/404/409, matching `config/api.py`. |

### `domain/`

Phase 3 adds these, each porting one Django module:

| Module | Ports | Binds |
|---|---|---|
| `courses.js` | `courses/services.py` | `onRecordValidate("courses")`, `onRecordValidate("course_holes")` |
| `rounds.js` | `rounds/services.py` round lifecycle | `onRecordCreate("rounds")`, `onRecordValidate("rounds")`, plus custom routes |
| `round_holes.js` | round hole initialisation | called by `rounds.js` during snapshotting |
| `shots.js` | `rounds/services.py` shot handling | `onRecordAfterCreateSuccess("shots")`, `onRecordAfterDeleteSuccess("shots")`, plus custom routes |
| `scoring.js` | `rounds/scoring.py` | pure functions, no hooks |

## Where each rule lives

Some of the domain rules are expressible in the schema and are already enforced
by `pb_schema.json`; the rest need hooks. Splitting them out is the point of
this document.

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

### Needs a hook (Phase 3)

**Course snapshotting.** On round creation, copy `par` from each `course_holes`
row into a new `round_holes` row, filtered by play mode: `front9` selects holes
1–9, `back9` selects 10–18, `full` selects all. `current_hole` starts at the
lowest selected hole number. Django: `create_round`.

**Stroke cache.** `round_holes.strokes` must equal that hole's shot count, and
must be updated in the same transaction as the shot insert or delete. Django
keeps this in `add_shot` / `undo_last_shot` / `delete_shot`.

**Shot renumbering.** Deleting a shot from the middle of a hole decrements
`shot_number` for every later shot on that hole, so the sequence stays gap-free.
Django does this as a single `UPDATE ... SET shot_number = shot_number - 1 WHERE
shot_number > n`, which is safe under the unique index because it rewrites rows
in ascending order into vacated slots. A naive per-record loop in a hook would
collide with the index on the first row — the ordering matters, and the Phase 3
tests should cover it explicitly.

**Undo semantics.** Remove only the highest-numbered shot on the given hole; no
renumbering needed, and a no-op when the hole has no shots.

**Play-mode validity.** `front9` and `back9` are only valid on 18-hole courses.
Django: `create_round`.

**Exact `hole_count` values.** The schema bounds `hole_count` to 9–18; the
actual rule is "9 or 18". Django: `CourseIn.hole_count: Literal[9, 18]`.

**Course hole-set validity.** A course's holes must number exactly
`hole_count`, be unique, and run sequentially from 1. Django:
`CourseIn.validate_holes`.

**Round is mutable only while in progress.** Every shot and current-hole
mutation rejects with 409 once the round is completed. Django raises
`ConflictError("Round is already completed")` in each service function.

**Completion totals.** On completing a round, sum `par` and `strokes` across its
holes into `total_par`, `total_strokes` and `relative_to_par`, set `status` and
`finished_at`. Django: `complete_round` + `calculate_round_totals`.

**Derived `total_par` on courses.** A Django property, not a column; computed
per response.

**Admin role assignment.** Set `users.role` from the `ADMIN_EMAILS` environment
variable on OAuth login, via `onRecordAuthWithOAuth`. Deferred to Phase 4
(#125).

## Transactions

Every rule above that touches more than one record has to be atomic — the stroke
cache in particular is a cache, and is only correct if it cannot be observed out
of step with the shots it counts. In hooks that means `$app.runInTransaction`:

```js
$app.runInTransaction((txApp) => {
    // all reads and writes go through txApp, never $app
});
```

The Django implementation additionally takes a row lock (`select_for_update`) on
the `RoundHole` before computing the next `shot_number`. PocketBase's SQLite
connection serialises writes, so the equivalent protection comes from doing the
read and the write inside one transaction; the unique index on
`(round_hole, shot_number)` is the backstop that turns a lost race into a
rejected write rather than a duplicate. Phase 3's concurrency tests should
assert that a rejected concurrent `add_shot` is the failure mode, not a
duplicated shot number.

## Custom routes

PocketBase's generated CRUD endpoints do not cover the verb-like operations in
the current API — completing a round, undoing a shot. Those are registered with
`routerAdd` in `main.pb.js` and delegate to a domain module. `API.md` lists
which endpoints are generated and which have to be written.

```js
routerAdd("POST", "/api/rounds/{id}/complete", (e) => {
    const rounds = require(`${__hooks}/domain/rounds.js`);
    return rounds.complete(e);
}, $apis.requireAuth());
```

## Testing

Phase 3 covers hook behaviour; Phase 5 (#126) covers endpoint parity against the
Django contract. The existing Django tests under `rounds/` and `courses/` are
the specification for both — they encode the behaviour this migration is
required to preserve, and porting them is cheaper than rewriting the rules from
this document.
