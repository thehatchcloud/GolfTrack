# `internal/hooks/domain/`

Domain modules — one Go package per aggregate.

Each package exposes a `Register(app core.App)` function that binds its own
PocketBase hooks and any custom routes it owns. `internal/hooks.Register` is the
only caller; nothing here registers hooks as an import side effect.

`../../../ARCHITECTURE.md` records which hook each package binds.

| Package | Owns | Binds |
|---|---|---|
| `courses` | course and hole validation | `hole_count` is 9 or 18; a hole fits its course; derived `total_par` on serialization |
| `rounds` | round lifecycle — create, complete, cancel, set current hole | a completed round is immutable; `POST /api/rounds/`, `/complete`, `/cancel`, `PATCH /current-hole` |
| `roundholes` | hole initialisation | the `strokes` default, the stroke-cache recount, the `RoundHoleOut` payload |
| `shots` | shot lifecycle — add, undo, update, delete | the stroke cache and the renumbering; the four nested shot routes |
| `scoring` | `calculate_round_totals` | nothing: pure functions, no hooks |

`scoring` is the one package with no `Register`. It has no PocketBase types in
its signatures and no hooks to bind, so a no-op registration would be noise; it
is called by `rounds` on completion.

## What these packages import

Collection names, field names and enum values come from
`internal/collections`; the `{"error": …}` contract comes from
`internal/apierr`; the record lookups they share — find a round for its owner,
resolve `(round, hole_number)` — come from `internal/records`.

None of the three lives in `internal/hooks`, because `internal/hooks` imports
the packages here in order to register them, and the dependency cannot run both
ways.
