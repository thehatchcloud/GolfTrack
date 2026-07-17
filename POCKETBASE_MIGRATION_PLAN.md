# Pocketbase Migration Plan for GolfTrack

## Executive Summary

GolfTrack is transitioning from Django + Django Ninja backend to Pocketbase. This is a backend-replacement operation that preserves the domain model and business logic while adopting Pocketbase's simpler deployment and operational model. The migration is sequenced to maintain production availability and data integrity throughout.

**Key Strategic Decisions:**
- Use Pocketbase's built-in REST API and auth system (eliminating Django/django-allauth)
- Implement domain logic via Pocketbase's JavaScript/Go hooks
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
- `pocketbase/hooks/` directory structure
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
- `pocketbase/rules.json` (Pocketbase record ACL format)

#### Validation Gate:
- [ ] All 6 collections defined in Admin UI
- [ ] ACL rules enforced (permission denied tests pass)
- [ ] Unique indexes working
- [ ] Field validation working

---

### PHASE 3: Business Logic Hooks (2 weeks)

**Objective:** Implement domain rules via Pocketbase JavaScript hooks

#### Core Domain Rules to Implement:
1. **Course Snapshotting:** When a round is created, copy par values from CourseHole into RoundHole
2. **Stroke Cache:** RoundHole.strokes = number of shots; update in same transaction as shot add/delete
3. **Shot Numbering:** Sequential within hole; delete mid-round shot renumbers all subsequent shots
4. **Undo Semantics:** Remove only the last shot (highest shotNumber) on current hole
5. **One Active Round:** Partial unique index on (user, status=in_progress)
6. **Play Modes:** 18-hole courses support full/front9/back9; 9-hole courses only full

#### Hook Files to Create:
- `pocketbase/hooks/courses.js` — course creation/update validation
- `pocketbase/hooks/rounds.js` — round lifecycle (create, update, validate)
- `pocketbase/hooks/shots.js` — shot add/undo/delete with renumbering
- `pocketbase/hooks/round_holes.js` — minimal initialization
- `pocketbase/hooks/scoring.js` — calculateRoundTotals logic

#### Custom API Routes:
- `POST /api/rounds/{id}/complete` — calculate totals, set status
- `POST /api/rounds/{id}/cancel` — delete round
- `PATCH /api/rounds/{id}/current-hole` — update current hole
- `POST /api/rounds/{id}/holes/{n}/undo` — remove last shot
- `PATCH/DELETE /api/rounds/{id}/holes/{n}/shots/{id}` — edit/delete shot

#### Deliverables:
- All hook files in `pocketbase/hooks/`
- Custom route implementations (Go SDK or hook-based)
- Unit tests for scoring logic

#### Validation Gate:
- [ ] All hook files created and compiling
- [ ] Course lifecycle works end-to-end
- [ ] Shot lifecycle: add → edit → delete with correct strokes/numbering
- [ ] Concurrency tests pass (no race conditions)
- [ ] Custom API routes functional

---

### PHASE 4: Authentication & OAuth (1-2 weeks)

**Objective:** Integrate OAuth and adapt authentication to Pocketbase

#### Tasks:
1. Configure Pocketbase OAuth providers (Google, Microsoft Entra ID)
2. Extend Pocketbase user schema with `role` field
3. Implement admin detection hook (check `ADMIN_EMAILS` env var)
4. Create optional password-login toggle
5. Update frontend to use Pocketbase auth

#### Deliverables:
- `pocketbase/hooks/auth.js` — OAuth and admin role assignment
- Updated environment variables documentation
- Frontend auth integration

#### Validation Gate:
- [ ] OAuth flow completes end-to-end
- [ ] Users created/updated on successful OAuth
- [ ] Admin role assigned correctly
- [ ] Frontend auth cookies validated

---

### PHASE 5: API Parity Testing (1-2 weeks)

**Objective:** Validate all endpoints match Django contract

#### Tasks:
1. Document Pocketbase auto-generated endpoints and custom routes
2. Create comprehensive API test suite (port Django tests)
3. Test filtering, pagination, sorting
4. Verify ACL enforcement (users see own data only, admins see all)
5. Validate error responses (200/201/400/401/403/404/409)

#### Deliverables:
- `pocketbase/tests/api_test.go` (or JavaScript test suite)
- `pocketbase/api_contract.md` — all endpoints documented
- Performance baseline vs. Django

#### Validation Gate:
- [ ] All endpoints accessible and return expected status codes
- [ ] Response bodies match Django contract
- [ ] ACL enforcement tested
- [ ] Pagination/filtering working
- [ ] Error responses correct

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
3. `pocketbase/hooks/rounds.js` — Round lifecycle logic
4. `pocketbase/hooks/shots.js` — Shot creation, deletion, renumbering
5. `pocketbase/hooks/scoring.js` — Round total calculation
6. `pocketbase/main.go` — Custom API routes and Litestream integration
7. `Dockerfile` — Pocketbase container with Litestream
8. `migration/import.js` — Data migration from Django to Pocketbase
9. `templates/base.html` — Frontend updated for Pocketbase auth/API
10. `.github/workflows/deploy.yml` — CI/CD pipeline for Pocketbase

---

## Key Architectural Decisions

1. **Hook Language:** JavaScript for Phase 3-8 (faster iteration), migrate to Go if performance critical
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
