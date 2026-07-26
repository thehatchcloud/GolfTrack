# `hooks/domain/`

Domain modules — one per aggregate, mirroring the Django service layer in
`rounds/services.py`, `courses/services.py` and `rounds/scoring.py`.

Each module is a plain CommonJS file exporting a `register()` function that
binds its own PocketBase hooks. `../main.pb.js` is the only caller; nothing here
registers hooks as a side effect of being required.

Phase 1 establishes the structure only. Phase 3 (#124) adds the modules below;
`../../ARCHITECTURE.md` records which hook each one binds and which Django
function it ports.

| Module | Ports |
|---|---|
| `courses.js` | `courses/services.py` — course + hole validation |
| `rounds.js` | `rounds/services.py` — round lifecycle, course snapshotting |
| `round_holes.js` | `rounds/services.py` — hole initialisation |
| `shots.js` | `rounds/services.py` — add/undo/update/delete with renumbering and the stroke cache |
| `scoring.js` | `rounds/scoring.py` — `calculate_round_totals` |
