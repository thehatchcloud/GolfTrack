# GolfTrack on PocketBase

Phase 1 (#122) of the Django → PocketBase migration tracked in epic #121. The
overall plan lives in [`POCKETBASE_MIGRATION_PLAN.md`](../POCKETBASE_MIGRATION_PLAN.md).

**Nothing in this directory is wired into the deployed app.** The Django app in
the repository root is still the only thing built and shipped (see
[`DJANGO.md`](../DJANGO.md) and [`DEPLOYMENT.md`](../DEPLOYMENT.md)). This
directory is a parallel, local-only environment until the Phase 9/10 cutover.

What Phase 1 delivers: a PocketBase application you can build and run locally,
the six collections defined and reproducible from a committed schema file, and
the hook package structure that Phase 3 will fill in. No business logic and no
access rules yet — those are Phases 2–3.

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
├── go.mod / go.sum       # module definition, PocketBase version pin
├── pb_schema.json        # source of truth for the six collections (embedded)
├── ARCHITECTURE.md       # hook / business-logic architecture
├── API.md                # PocketBase endpoints vs. the current Django contract
├── internal/hooks/
│   ├── hooks.go          # Register() — the only place hooks are bound
│   ├── collections.go    # collection names/ids and enum values
│   ├── errors.go         # error-contract helpers for custom routes
│   └── domain/           # Phase 3 domain packages land here
├── scripts/
│   ├── dev.sh            # build the binary + run the dev server
│   ├── apply_schema.py   # pb_schema.json  ->  running instance (reconcile)
│   ├── export_schema.py  # running instance ->  pb_schema.json
│   └── verify_schema.py  # assert the Phase 1 validation gate
└── .local/               # gitignored: compiled binary, pb_data
```

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
| `role` | `select` | `USER` / `ADMIN`, required. Assigned from `ADMIN_EMAILS` at login in Phase 4 (#125). |
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

Deliberately **not set** in this phase. The five new collections have `null`
API rules, which in PocketBase means superuser-only, and `users` keeps its
built-in defaults (`id = @request.auth.id` for list/view/update/delete, open
create). Phase 2 (#123) defines the real rules and the tests that enforce them.

The intended shape, from the plan:

| Collection | Read | Write |
|---|---|---|
| `users` | own record; admins read all | own record |
| `courses`, `course_holes` | any authenticated user | admins only |
| `rounds`, `round_holes`, `shots` | own rounds; admins read all | own rounds |

## Validation gate

The Phase 1 gate from #122, and how each item is checked:

| Gate item | How |
|---|---|
| PocketBase local instance up and running | `pocketbase/scripts/dev.sh`, then `curl localhost:8090/api/health` |
| All 6 collections created with correct fields | `verify_schema.py` — field-presence checks per collection |
| Admin UI accessible, collections visible | <http://127.0.0.1:8090/_/> returns 200 and lists the six collections |
| `GET /api/collections` returning schema | `curl` with a superuser token returns 11 collections (6 app + 5 PocketBase system) |

`verify_schema.py` goes past field presence and exercises the constraints that
only appear at write time — the partial unique index, the composite unique
indexes, relation cascades, the restrict-on-delete behaviour for courses, and
field range validation. It cleans up the records it creates, so it is safe to
re-run. 24 checks as of this phase.

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

## Next phases

| Phase | Issue | Adds |
|---|---|---|
| 2 — Collections & ACLs | #123 | Access rules and the tests that enforce them |
| 3 — Business logic hooks | #124 | Domain modules under `hooks/domain/`, custom routes |
| 4 — Auth & OAuth | #125 | Google / Microsoft Entra ID providers, `role` assignment |
