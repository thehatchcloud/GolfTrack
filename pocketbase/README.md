# GolfTrack on PocketBase

Phase 1 (#122) of the Django → PocketBase migration tracked in epic #121. The
overall plan lives in [`POCKETBASE_MIGRATION_PLAN.md`](../POCKETBASE_MIGRATION_PLAN.md).

**Nothing in this directory is wired into the deployed app.** The Django app in
the repository root is still the only thing built and shipped (see
[`DJANGO.md`](../DJANGO.md) and [`DEPLOYMENT.md`](../DEPLOYMENT.md)). This
directory is a parallel, local-only environment until the Phase 9/10 cutover.

What Phase 1 delivers: a PocketBase instance you can run locally, the six
collections defined and reproducible from a committed schema file, and the hook
directory structure that Phase 3 will fill in. No business logic and no access
rules yet — those are Phases 2–3.

## Layout

```
pocketbase/
├── pb_schema.json        # source of truth for the six collections
├── ARCHITECTURE.md       # hook / business-logic architecture
├── API.md                # PocketBase endpoints vs. the current Django contract
├── hooks/
│   ├── main.pb.js        # the only file PocketBase auto-loads
│   ├── lib/              # shared infrastructure (collection constants, errors)
│   └── domain/           # Phase 3 domain modules land here
├── scripts/
│   ├── dev.sh            # download the pinned binary + run the dev server
│   ├── apply_schema.py   # pb_schema.json  ->  running instance
│   ├── export_schema.py  # running instance ->  pb_schema.json
│   └── verify_schema.py  # assert the Phase 1 validation gate
└── .local/               # gitignored: binary, pb_data, pb_migrations
```

## Quick start

Requires `curl`, `unzip` and Python 3 (no virtualenv — the scripts use only the
standard library).

```bash
# terminal 1 — downloads PocketBase 0.39.9 on first run, then serves
pocketbase/scripts/dev.sh

# terminal 2 — create the collections, then check them
python3 pocketbase/scripts/apply_schema.py
python3 pocketbase/scripts/verify_schema.py
```

Then open the Admin UI at <http://127.0.0.1:8090/_/> and log in with
`dev@golftrack.local` / `devdevdevdev` (override with `PB_SUPERUSER_EMAIL` /
`PB_SUPERUSER_PASSWORD`).

`dev.sh` honours `PB_VERSION`, `PB_HOST` and `PB_PORT`. Everything it writes
lands in `pocketbase/.local/`, so deleting that directory resets the
environment completely.

## Collections

Six collections, mapped from the Django models they replace. `users` is
PocketBase's built-in auth collection with two fields added; the other five are
new base collections with fixed, readable ids (`golftrack_*`) so that relation
targets in `pb_schema.json` are legible in review.

| Collection | Id | Django model |
|---|---|---|
| `users` | `_pb_users_auth_` | `accounts.User` |
| `courses` | `golftrack_crses` | `courses.Course` |
| `course_holes` | `golftrack_chls` | `courses.CourseHole` |
| `rounds` | `golftrack_rnds` | `rounds.Round` |
| `round_holes` | `golftrack_rhls` | `rounds.RoundHole` |
| `shots` | `golftrack_shts` | `rounds.Shot` |

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

`pb_schema.json` is the source of truth, and `apply_schema.py` pushes it through
PocketBase's collection import endpoint (an upsert by collection id — safe to
re-run, and it reconciles a drifted dev instance).

To change the schema, either edit `pb_schema.json` and apply it, or edit in the
Admin UI and export:

```bash
python3 pocketbase/scripts/export_schema.py   # instance -> pb_schema.json
git diff pocketbase/pb_schema.json
```

Export strips collection-level `created`/`updated` timestamps and sorts keys, so
apply → export round-trips byte-for-byte and diffs stay readable.

`dev.sh` runs the server with `--automigrate=0` on purpose. With automigration
on, PocketBase writes a `pb_migrations/*.js` file for every Admin UI change —
and when several collections are created within the same second, those files
share a timestamp prefix and replay in *alphabetical* order, which puts
`created_course_holes` before `created_courses` and fails on the missing
relation target. Since `pb_schema.json` is the source of truth here, the
simplest fix is to not generate those files at all.

Phase 9 (#130) revisits this: a production container needs a deterministic way
to apply the schema at startup, which is likely a single hand-ordered migration
generated from `pb_schema.json` rather than an HTTP import.

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
