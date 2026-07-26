# Pocketbase Migration Plan for GolfTrack

## Executive Summary

GolfTrack is transitioning from Django + Django Ninja backend to Pocketbase. This is a backend-replacement operation that preserves the domain model and business logic while adopting Pocketbase's simpler deployment and operational model. The migration is sequenced to maintain production availability and data integrity throughout.

**Key Strategic Decisions:**
- Use Pocketbase's built-in REST API and auth system (eliminating Django/django-allauth)
- Implement domain logic as Go hooks — PocketBase is consumed as a Go framework, built into a single portable binary (decision revised in #122 review; originally JavaScript hooks on the prebuilt binary)
- Maintain SQLite as the database (leveraging Pocketbase's native support)
- Preserve Litestream S3 replication for production resilience
- Migrate data in-place (no "clean slate")
- Keep the frontend unchanged initially (HTMX/Alpine.js or Next.js against new API)

**Timeline: 12-17 weeks (3-4 months)** for full production migration

---

## Phase Breakdown

### PHASE 1: Foundation & Design (1-2 weeks)

**Objective:** Establish Pocketbase architecture and collection schema

#### Tasks:
1. Set up Pocketbase development environment
2. Create initial Pocketbase database schema matching Django models
3. Design Pocketbase API schema and document auto-generated endpoints
4. Establish hook/business-logic architecture
5. Document collection validation rules and ACLs

#### Deliverables:
- `pocketbase/pb_schema.json` (collection definitions)
- `pocketbase/internal/hooks/` package structure (Go; the original `pocketbase/hooks/*.js` layout was dropped with the JavaScript decision in #122)
- `pocketbase/README.md` (setup and local dev)

#### Validation Gate:
- [ ] Pocketbase local instance up and running
- [ ] All 6 collections created with correct fields
- [ ] Admin UI accessible, collections visible
- [ ] GET /api/collections endpoints returning schema

---

### PHASE 2: Collection Definition & ACLs (1 week)

**Objective:** Define full Pocketbase schema with validation and permissions

#### Collections to Define:

**users**
- Extend Pocketbase's built-in user model
- Add fields: `role` (enum: USER, ADMIN), `display_name`
- ACL: only admins can read other users' data

**courses**
- Fields: `id`, `name`, `hole_count`, `is_archived`, `created_at`, `updated_at`
- Index on `name`
- ACL: anyone can read, only admins can create/update/delete
- Validation: `hole_count` in [9, 18]

**course_holes**
- Fields: `id`, `course` (relation), `hole_number`, `par`, `created_at`, `updated_at`
- Composite unique index: `(course, hole_number)`
- Validation: `hole_number` 1-18, `par` 3-6

**rounds**
- Fields: `id`, `user` (relation), `course` (relation), `status` (enum), `play_mode`, `started_at`, `finished_at`, `note`, `current_hole`, `total_strokes`, `total_par`, `relative_to_par`
- Partial unique index: `(user)` where `status = 'in_progress'`
- ACL: users see own rounds only, admins see all

**round_holes**
- Fields: `id`, `round` (relation), `hole_number`, `par`, `strokes`
- Composite unique index: `(round, hole_number)`

**shots**
- Fields: `id`, `round_hole` (relation), `shot_number`, `club`, `created_at`
- Composite unique index: `(round_hole, shot_number)`

#### Deliverables:
- `pocketbase/pb_schema.json` with full ACL rules
- ~~`pocketbase/rules.json` (Pocketbase record ACL format)~~ — *not created. PocketBase has no separate ACL document; rules are properties of a collection, and that is the only form the import endpoint and the Admin UI accept. See `pocketbase/README.md` § "Where the rules live".*

#### Validation Gate:
- [x] All 6 collections defined — from the embedded `pb_schema.json`, applied by the startup sync, rather than by hand in the Admin UI (a hand edit there is reverted on the next restart)
- [x] ACL rules enforced (permission denied tests pass) — `pocketbase/acl_test.go`
- [x] Unique indexes working — `pocketbase/schema_test.go`
- [x] Field validation working — `pocketbase/schema_test.go`

*Delivered in #123. The rule set, and the reasoning behind `role` not being self-assignable and sign-up being OAuth2-only, are in `pocketbase/README.md` § "Access rules".*

---

### PHASE 3: Business Logic Hooks (2 weeks)

**Objective:** Implement domain rules via Go hooks compiled into the golftrack-pb binary

#### Core Domain Rules to Implement:
1. **Course Snapshotting:** When a round is created, copy par values from CourseHole into RoundHole
2. **Stroke Cache:** RoundHole.strokes = number of shots; update in same transaction as shot add/delete
3. **Shot Numbering:** Sequential within hole; delete mid-round shot renumbers all subsequent shots. Issue this as a single `UPDATE ... WHERE shot_number > n` — a per-record loop collides with the unique index on the first row
4. **Undo Semantics:** Remove only the last shot (highest shotNumber) on current hole
5. **One Active Round:** ~~Partial unique index on (user, status=in_progress)~~ — *the index shipped in Phase 1 and is tested in Phase 2. What remains here is the status code: an index violation surfaces as 400, where Django returns 409*
6. **Play Modes:** 18-hole courses support full/front9/back9; 9-hole courses only full
7. **Exact `hole_count`:** the schema bounds it 9–18; the rule is "9 or 18"
8. **Course hole-set validity:** holes number exactly `hole_count`, unique, sequential from 1
9. **Round mutable only while in progress:** shot and current-hole mutations reject with 409 once completed
10. **Completion totals:** sum par and strokes into `total_par` / `total_strokes` / `relative_to_par`, set `status` and `finished_at`
11. **Derived `total_par` on courses:** a Django property, not a column — compute per response

Rules 7–11 were implicit in the original list; `pocketbase/ARCHITECTURE.md` carries the full split of schema-enforced vs. hook-enforced, and is the working reference for this phase.

**API rules do not apply to hook code.** The Phase 2 access rules gate the generated endpoints and PocketBase's own request handlers. Anything going through `app.Save`, `app.Delete` or `RunInTransaction` writes as the application, so every invariant above still has to be enforced in the hook itself.

#### Hook Packages to Create (under `pocketbase/internal/hooks/domain/`):
- `courses` — course creation/update validation
- `rounds` — round lifecycle (create, update, validate)
- `shots` — shot add/undo/delete with renumbering
- `roundholes` — minimal initialization
- `scoring` — calculateRoundTotals logic (pure functions, unit-tested)

#### Custom API Routes:
- `POST /api/rounds/` — creation snapshots holes, so the generated create is not enough
- `POST /api/rounds/{id}/holes/{n}/shots` — maintains the stroke cache
- `POST /api/rounds/{id}/complete` — calculate totals, set status
- `POST /api/rounds/{id}/cancel` — delete round
- `PATCH /api/rounds/{id}/current-hole` — update current hole
- `POST /api/rounds/{id}/holes/{n}/undo` — remove last shot
- `PATCH/DELETE /api/rounds/{id}/holes/{n}/shots/{id}` — edit/delete shot

Bind `apis.RequireAuth()` on these — it is what makes an anonymous call return 401 rather than the 200/404 the generated endpoints give.

#### Deliverables:
- All domain packages under `pocketbase/internal/hooks/domain/`, each exposing `Register(app core.App)` and wired up from `internal/hooks.Register` (never as an import side effect)
- Custom route implementations (registered on the router in `OnServe`), with failures written through `internal/hooks/errors.go`
- Unit tests for scoring logic
- Hook-behaviour tests extending the Phase 2 harness in `pocketbase/testapp_test.go` — note that each `tests.ApiScenario` needs its own app, which is why the fixture record ids are fixed strings

#### Validation Gate:
- [ ] All domain packages created and compiling (`go build ./...`)
- [ ] Course lifecycle works end-to-end
- [ ] Shot lifecycle: add → edit → delete with correct strokes/numbering
- [ ] Renumbering test covers the ordering explicitly
- [ ] Concurrency tests pass — a rejected concurrent `add_shot`, not a duplicated shot number
- [ ] Custom API routes functional
- [ ] 409 returned where Django returns 409 (round already in progress, round already completed)
- [ ] `make pb-test` green

---

### PHASE 4: Authentication & OAuth (1-2 weeks)

**Objective:** Integrate OAuth and adapt authentication to Pocketbase

#### Tasks:
1. Configure Pocketbase OAuth providers (Google, Microsoft Entra ID)
2. ~~Extend Pocketbase user schema with `role` field~~ — *done in Phase 1; Phase 2 added the access rules over it*
3. Implement admin detection (check `ADMIN_EMAILS` env var) via `OnRecordAuthWithOAuth2Request`
4. Decide the password-login question — see below; this is a decision, not just a toggle
5. Update frontend to use Pocketbase auth — coordinate with Phase 7, which owns frontend adaptation generally

`pocketbase/internal/hooks/users.go` already defaults `users.role` to `USER` on create — the field default Django writes as `default=Role.USER`, needed because `role` is required and an OAuth2 sign-up supplies only email, name and avatar. This phase adds the *promotion* on top of it.

Password login is not purely additive: Phase 2 set the `users` create rule to `@request.context = "oauth2"`, which closes the account-minting half of a privilege-escalation path. Enabling password *sign-up* means relaxing that rule while keeping `role` unsettable by the client; password *authentication* for an already-provisioned account is governed by `authRule` and needs no change. Either way `pocketbase/acl_test.go` has to be updated to match — `TestSignupIsOAuth2Only` asserts the current behaviour.

#### Deliverables:
- Admin role assignment in `pocketbase/internal/hooks/` (Go), registered from `hooks.Register`
- Updated environment variables documentation (`ADMIN_EMAILS`, provider client ids/secrets)
- Frontend auth integration
- A recorded decision on password login, with `acl_test.go` matching it

#### Validation Gate:
- [ ] OAuth flow completes end-to-end for both providers
- [ ] Users created/updated on successful OAuth
- [ ] A first-time sign-in lands with `role = USER` through the real OAuth path
- [ ] Admin role assigned correctly from `ADMIN_EMAILS`, including a user who was already `USER` before their address was added
- [ ] A non-admin still cannot self-assign `ADMIN` — `acl_test.go` still green
- [ ] Frontend auth cookies validated

---

### PHASE 5: API Parity Testing (1-2 weeks)

**Objective:** Validate all endpoints match Django contract

#### Tasks:
1. Document Pocketbase auto-generated endpoints and custom routes — `pocketbase/API.md` already does this; keep it current
2. Port the Django API tests into the existing Go suite in `pocketbase/` (package `main`)
3. Test filtering, pagination, sorting
4. Verify ACL enforcement **on the custom routes** — the generated endpoints are already covered by `pocketbase/acl_test.go`, which tests all six collections as anonymous / user / other user / admin / superuser. The custom routes enforce ownership in hook code instead, and are untested until this phase
5. Validate error responses (200/201/400/401/403/404/409)

`API.md` records seven parity gaps, and closing or consciously deferring each of them is the substance of "response bodies match Django contract": camelCase field naming, unset numbers returning `0`/`""` instead of `null`, 15-character string ids vs. integers, relations as ids plus `expand` rather than inline objects, error body shape, 400 instead of 409 on constraint violations, and — new in Phase 2 — anonymous callers getting 200/404/400 where Django returns 401/403.

#### Deliverables:
- Parity tests added to the existing Go suite in `pocketbase/` (`go test ./...`, using PocketBase's `tests` package for full-app tests)
- `pocketbase/API.md` updated — each gap marked resolved, or carried forward with the decision recorded
- Performance baseline vs. Django

#### Validation Gate:
- [ ] All endpoints accessible and return expected status codes
- [ ] Response bodies match Django contract, or the deviation is recorded in `API.md` with a decision
- [ ] ACL enforcement tested on the custom routes
- [ ] Pagination/filtering working
- [ ] Error responses correct, including 409 and the anonymous-caller codes
- [ ] `make pb-test` green

---

### PHASE 6: Data Migration (1 week)

**Objective:** Migrate existing Django SQLite database to Pocketbase

#### Tasks:
1. Export Django database to JSON
2. Transform export (field names, FK references, enum values, timestamps)
3. Create Pocketbase import script using JS SDK
4. Validate migration (row counts, spot checks, referential integrity)
5. Create rollback plan

#### Deliverables:
- `migration/django_export.py` (export script)
- `migration/transform.js` (transformation logic)
- `migration/import.js` (Pocketbase import)
- `migration/README.md` (instructions)
- `migration/validation_report.txt` (checklist)

#### Validation Gate:
- [ ] All data successfully imported
- [ ] Row counts match Django source
- [ ] Spot checks pass (sample rounds displayable)
- [ ] No referential integrity errors
- [ ] Rollback procedure documented

---

### PHASE 7: Frontend API Adaptation (1-2 weeks)

**Objective:** Update frontend to work with Pocketbase API

#### Tasks:
1. Update API client library to use Pocketbase auth cookies/tokens
2. Adjust API endpoint calls for Pocketbase format
3. Test all frontend workflows end-to-end
4. Verify CSS/Tailwind styling

#### Workflows to Test:
- Home page (resume in-progress round or list completed)
- Courses list/create/edit/delete
- Start round (pick course, play mode)
- Play round (add/undo/edit/delete shots)
- Complete round (calculate totals)
- Cancel round
- Review completed rounds

#### Validation Gate:
- [ ] All page routes load
- [ ] All API calls successful
- [ ] Round creation and play flow works
- [ ] No JavaScript console errors
- [ ] Styling looks correct

---

### PHASE 8: Performance & Optimization (1 week)

**Objective:** Ensure Pocketbase performance meets production requirements

#### Tasks:
1. Load testing (10, 50, 100 concurrent users)
2. Optimize hook performance (profile, batch operations, caching)
3. Review database indexing
4. Monitor memory/resource usage

#### Deliverables:
- `pocketbase/performance_report.md` — benchmarks vs. Django
- Optimized hook implementations (if needed)

#### Validation Gate:
- [ ] Response times acceptable (<200ms p95 for most endpoints)
- [ ] No memory leaks
- [ ] Concurrency tests pass
- [ ] Queries efficient

---

### PHASE 9: Docker & Deployment Setup (1-2 weeks)

**Objective:** Package Pocketbase for production deployment

#### Tasks:
1. Create Pocketbase Dockerfile (single binary, Litestream, static assets)
2. Integrate Litestream (S3 replication to DO Spaces)
3. Map Django env vars to Pocketbase equivalents
4. Implement `/api/health` endpoint
5. Document initialization procedure

#### Deliverables:
- `Dockerfile` (Pocketbase-based)
- `litestream.yml` (updated if needed)
- `entrypoint.sh` (Pocketbase startup)
- `.env.example` (Pocketbase configuration)
- Updated `DEPLOYMENT.md`

#### Validation Gate:
- [ ] Docker image builds
- [ ] Container starts and serves API
- [ ] Litestream replicates without errors
- [ ] Health check passes
- [ ] Static assets served correctly

---

### PHASE 10: CI/CD & Rollout (1-2 weeks)

**Objective:** Update CI/CD and prepare production deployment

#### Tasks:
1. Update GitHub Actions workflows to build Pocketbase image
2. Deploy to dev server first (test everything)
3. Run full staging test with real OAuth
4. Execute production deployment
5. Monitor logs for 24 hours post-deployment

#### Deployment Strategy Options:
- **Zero-downtime:** Run Django and Pocketbase side-by-side, switch proxy after validation
- **Single-server:** Maintenance window to migrate data and switch containers

#### Rollback Plan:
- Keep Django image available in GHCR
- Point proxy back to Django if issues arise
- Recovery time: <30 minutes
- Test rollback procedure in staging first

#### Validation Gate:
- [ ] CI/CD pipeline passes
- [ ] Pocketbase image pushed to GHCR
- [ ] Dev server deployment succeeds
- [ ] Staging deployment succeeds
- [ ] Production deployment succeeds
- [ ] Users can log in and use app

---

### PHASE 11: Decommissioning & Cleanup (1 week)

**Objective:** Remove Django code and finalize the migration

#### Tasks:
1. Remove Django code (`config/`, `accounts/`, `courses/`, `rounds/`)
2. Remove Next.js legacy code (`app/`, `components/`, `lib/`, `prisma/`)
3. Reorganize repository structure
4. Update documentation (README, DEPLOYMENT.md, delete DJANGO.md)
5. Archive historical branches

#### Deliverables:
- Cleaned repository structure
- POCKETBASE.md architecture documentation
- Final version tags for Django and Next.js

#### Validation Gate:
- [ ] Django code removed
- [ ] Next.js code removed
- [ ] Documentation updated
- [ ] Prod still functioning
- [ ] Historical commits preserved

---

## Risk Assessment & Mitigation

| Risk | Severity | Mitigation |
|------|----------|-----------|
| Business logic bugs in hooks | High | Comprehensive test coverage, peer review, staged rollout |
| Data corruption during migration | High | Backup Django DB, validate row counts, test rollback, keep Django read-only during cutover |
| Performance regression | Medium | Load testing in Phase 8, optimize hooks, compare to Django baseline |
| OAuth provider misconfiguration | Medium | Test with real credentials in staging, documented runbooks |
| Concurrent shot edits (race condition) | Medium | IMMEDIATE transaction mode, concurrency tests, Pocketbase DB locking |
| Litestream replication issues | Medium | Test restore flow, S3 bucket backups, monitor replication lag |
| Breaking frontend changes | Medium | Comprehensive Phase 7 testing, no format changes unless documented |
| Deployment/container issues | Low | Docker best practices, health checks, CI/CD validation |

---

## Migration Checklist

### Pre-Migration (Before Phase 1)
- [ ] Stakeholder approval of Pocketbase adoption
- [ ] Access to production VM and S3/DO Spaces
- [ ] GitHub Actions secrets ready (OAuth, deploy keys)
- [ ] Backup of current Django database

### Pre-Production (Before Phase 10)
- [ ] All phases 1-9 complete and tested
- [ ] Performance benchmarks acceptable
- [ ] Data migration validated on staging
- [ ] Rollback plan documented and tested
- [ ] User communication plan

### Production Cutover (Phase 10)
- [ ] Final backup of Django database
- [ ] Pocketbase image built and pushed to GHCR
- [ ] Dev server switched to Pocketbase
- [ ] Prod server switched to Pocketbase
- [ ] Health checks passing
- [ ] OAuth login tested with real accounts
- [ ] Full end-to-end test (create round, play, complete)
- [ ] Monitor logs for 24 hours

### Post-Cutover (Phase 11 & beyond)
- [ ] Django code removed from repo
- [ ] Documentation updated
- [ ] Team trained on Pocketbase
- [ ] Old Django database backed up (archive)

---

## Critical Files for Implementation

1. `pocketbase/collections/users.json` — User collection schema with role field
2. `pocketbase/collections/rounds.json` — Round collection with partial unique index
3. `pocketbase/internal/hooks/domain/rounds` — Round lifecycle logic
4. `pocketbase/internal/hooks/domain/shots` — Shot creation, deletion, renumbering
5. `pocketbase/internal/hooks/domain/scoring` — Round total calculation
6. `pocketbase/main.go` — Custom API routes and Litestream integration
7. `Dockerfile` — Pocketbase container with Litestream
8. `migration/import.js` — Data migration from Django to Pocketbase
9. `templates/base.html` — Frontend updated for Pocketbase auth/API
10. `.github/workflows/deploy.yml` — CI/CD pipeline for Pocketbase

---

## Key Architectural Decisions

1. **Hook Language:** Go from the start — PocketBase as a Go framework, one portable binary embedding server, schema and hooks. *(Revised during #122 review; the original decision was JavaScript hooks for Phase 3-8 with a possible later Go migration.)*
2. **Frontend Framework:** Keep HTMX + Alpine.js (lightweight), or revert to Next.js (separate concern)
3. **Data Migration:** In-place migration (preserves history), not "clean slate"
4. **Deployment Model:** Single container, SQLite + Litestream (same as Django)
5. **Zero-Downtime Strategy:** Dual-stack period recommended to minimize risk

---

## Timeline Summary

| Phase | Duration | Key Deliverable |
|-------|----------|-----------------|
| 1. Foundation | 1-2 weeks | Pocketbase local dev environment |
| 2. Schema | 1 week | Collection definitions with ACLs |
| 3. Hooks | 2 weeks | Business logic implementation |
| 4. Auth | 1-2 weeks | OAuth integration + admin assignment |
| 5. API Testing | 1-2 weeks | Full endpoint validation |
| 6. Data Migration | 1 week | Django → Pocketbase data transfer |
| 7. Frontend | 1-2 weeks | API adaptation and testing |
| 8. Performance | 1 week | Load testing and optimization |
| 9. Docker/Deploy | 1-2 weeks | Container setup and rollout prep |
| 10. CI/CD & Rollout | 1-2 weeks | Production deployment |
| 11. Cleanup | 1 week | Remove Django/Next.js code |

**Total: 12-17 weeks (3-4 months)**

---

## Next Steps

1. **Review & Approval:** Validate plan with team/stakeholders
2. **Phase 1 Start:** Set up Pocketbase development environment
3. **Incremental Execution:** Complete each phase with validation gates
4. **Regular Check-ins:** Report progress and adjust timeline as needed
5. **Documentation:** Keep POCKETBASE.md updated as implementation proceeds

---

*This plan is designed for incremental execution with clear validation gates at each phase, allowing course corrections without committing the entire system until confidence is high.*
