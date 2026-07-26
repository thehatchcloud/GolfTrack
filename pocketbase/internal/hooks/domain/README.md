# `internal/hooks/domain/`

Domain modules — one Go package per aggregate, mirroring the Django service
layer in `rounds/services.py`, `courses/services.py` and `rounds/scoring.py`.

Each package exposes a `Register(app core.App)` function that binds its own
PocketBase hooks. `internal/hooks.Register` is the only caller; nothing here
registers hooks as an import side effect.

Phase 1 establishes the structure only. Phase 3 (#124) adds the packages
below; `../../../ARCHITECTURE.md` records which hook each one binds and which
Django function it ports.

| Package | Ports |
|---|---|
| `courses` | `courses/services.py` — course + hole validation |
| `rounds` | `rounds/services.py` — round lifecycle, course snapshotting |
| `roundholes` | `rounds/services.py` — hole initialisation |
| `shots` | `rounds/services.py` — add/undo/update/delete with renumbering and the stroke cache |
| `scoring` | `rounds/scoring.py` — `calculate_round_totals` (pure functions, unit-testable with `go test`) |
