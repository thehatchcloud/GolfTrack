# Pocketbase Migration Plan for GolfTrack

## Executive Summary

GolfTrack is transitioning from Django + Django Ninja backend to Pocketbase. This is a backend-replacement operation that preserves the domain model and business logic while adopting Pocketbase's simpler deployment and operational model. The migration is sequenced to maintain production availability and data integrity throughout.

**Key Strategic Decisions:**
- Use Pocketbase's built-in REST API and auth system (eliminating Django/django-allauth)
- Implement domain logic as Go hooks — PocketBase is consumed as a Go framework, built into a single portable binary (decision revised in #122 review; originally JavaScript hooks on the prebuilt binary)
- Maintain SQLite as the database (leveraging Pocketbase's native support)
- Preserve Litestream S3 replication for production resilience
- Migrate data in-place (no "clean slate")
- ~~Keep the frontend unchanged initially (HTMX/Alpine.js or Next.js against new API)~~ — *the frontend cannot be kept unchanged: it is Django server-rendered (`rounds/views.py`, `courses/views.py` and eleven Django templates), and PocketBase serves an API and static assets rather than rendering Django templates. The UI is rewritten as a PocketBase-native frontend in Phase 7B, preserving behaviour rather than code.*

**Timeline: 15-21 weeks (4-5 months)** for full production migration

---

## Phase Breakdown

> **On the 7A/7B lettering:** the frontend rewrite was added after Phases 1-5
> shipped, and phase numbers are load-bearing here — they are cited throughout
> `pocketbase/*.md`, in test comments, and against issue numbers (#128 is
> "Phase 7", #130 is "Phase 9"). Renumbering 7-11 would invalidate all of that
> for no gain, so the new work is lettered into Phase 7 instead: **7A** is the
> API adaptation delivered in #128, **7B** is the rewrite. Phases 8-11 keep
> their numbers.

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

The vocabulary and error helpers these share moved out of `internal/hooks` into
`internal/collections` and `internal/apierr`, joined by `internal/records` for
the lookups more than one package needs. `internal/hooks` imports every domain
package in order to register it, so the shared pieces cannot live there.

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
- [x] All domain packages created and compiling (`go build ./...`)
- [x] Course lifecycle works end-to-end — `pocketbase/domain_test.go`, plus the derived `total_par` in `routes_test.go`
- [x] Shot lifecycle: add → edit → delete with correct strokes/numbering — `domain_test.go`, `routes_test.go`
- [x] Renumbering test covers the ordering explicitly — `TestDeleteShotRenumbersSubsequentShots` asserts the surviving *clubs*, not just the count
- [x] Concurrency tests pass — `pocketbase/concurrency_test.go`
- [x] Custom API routes functional — `pocketbase/routes_test.go`, every route including its anonymous 401
- [x] 409 returned where Django returns 409 — round already in progress, round already completed, cancelling a finished round
- [x] `make pb-test` green

*Delivered in #124. Two deviations from this section, both recorded in
`pocketbase/README.md` § "Deviations": the error helpers live in
`internal/apierr` rather than `internal/hooks/errors.go` (an import cycle
otherwise), and the "holes number exactly `hole_count`" half of rule 8 is
checked at round creation, because PocketBase creates a course and its holes as
separate records and that is the first point the set is read as a whole.*

---

### PHASE 4: Authentication & OAuth (1-2 weeks)

**Objective:** Integrate OAuth and adapt authentication to Pocketbase

#### Tasks:
1. [x] Configure Pocketbase OAuth providers (Google, Microsoft Entra ID) — from environment variables at startup, not from the committed schema; see below
2. ~~Extend Pocketbase user schema with `role` field~~ — *done in Phase 1; Phase 2 added the access rules over it*
3. [x] Implement admin detection (check `ADMIN_EMAILS` env var) via `OnRecordAuthWithOAuth2Request` — `pocketbase/internal/hooks/adminrole.go`
4. [x] Decide the password-login question — decided; see below
5. [x] Update frontend to use Pocketbase auth — the auth slice is specified in `pocketbase/AUTH.md`; Phase 7A (#128) made the token-storage decision, and Phase 7B built the sign-in/sign-out pages and the cookie handling against it

`pocketbase/internal/hooks/users.go` already defaults `users.role` to `USER` on create — the field default Django writes as `default=Role.USER`, needed because `role` is required and an OAuth2 sign-up supplies only email, name and avatar. This phase adds the *promotion* on top of it.

Password login is not purely additive: Phase 2 set the `users` create rule to `@request.context = "oauth2"`, which closes the account-minting half of a privilege-escalation path. Enabling password *sign-up* means relaxing that rule while keeping `role` unsettable by the client; password *authentication* for an already-provisioned account is governed by `authRule` and needs no change. Either way `pocketbase/acl_test.go` has to be updated to match — `TestSignupIsOAuth2Only` asserts the current behaviour.

**Decision (#125):** self-service registration stays closed in every environment, so the create rule is unchanged; password *authentication* for accounts that already exist is available behind `GOLFTRACK_ALLOW_PASSWORD_LOGIN`, off by default, mirroring Django's `DJANGO_ALLOW_PASSWORD_LOGIN`. `TestSignupIsOAuth2Only` gained a case proving the two switches are independent.

#### Deliverables:
- [x] Admin role assignment in `pocketbase/internal/hooks/` (Go), registered from `hooks.Register`
- [x] Updated environment variables documentation (`ADMIN_EMAILS`, provider client ids/secrets) — `pocketbase/AUTH.md`, summarised in `pocketbase/README.md`
- [x] Frontend auth integration — the contract and the token-storage trade-off, built against in Phase 7B
- [x] A recorded decision on password login, with `acl_test.go` matching it

#### Validation Gate:
- [x] OAuth flow completes end-to-end for both providers — `pocketbase/auth_test.go` drives the real endpoint with a fake provider; the live token exchange needs credentials and a browser and is owner-verified
- [x] Users created/updated on successful OAuth
- [x] A first-time sign-in lands with `role = USER` through the real OAuth path
- [x] Admin role assigned correctly from `ADMIN_EMAILS`, including a user who was already `USER` before their address was added
- [x] A non-admin still cannot self-assign `ADMIN` — `acl_test.go` still green
- [x] Frontend auth cookies validated — PocketBase's session is a token the client holds, so where it lives is a frontend decision. Phase 7A (#128) decided *where* (a `Secure`, `SameSite=Lax`, non-`HttpOnly` cookie); **Phase 7B built and validated it** (`internal/web/auth.go`, `web_test.go`), including that the client-writable record half of the cookie grants nothing

*Delivered in #125. Two deviations from this section, both recorded in
`pocketbase/README.md` § "Deviations": the OAuth providers are applied from the
environment at every startup rather than committed to `pb_schema.json` (client
secrets cannot be committed, and per-environment client ids would have to be
edited per deployment), and the sign-in hook additionally discards the
caller-supplied `createData` — enabling OAuth2 sign-up is what makes that map
reachable, and the create rule restricts only the context of an account
creation, not its contents.*

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

Phase 3 closed three of those *on the custom routes it built* — camelCase, the error body shape, and both the 409s and the anonymous 401. What is left for this phase is the generated endpoints, which none of those fixes reach, plus the four gaps that were never route-local (null vs. `0`, record ids, relation expansion, and the `GET` response shapes).

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

### PHASE 7A: Frontend API Adaptation (1-2 weeks)

**Objective:** Make the API serve the exact response shapes a GolfTrack frontend renders

#### Tasks:
1. [x] Update API client library to use Pocketbase auth cookies/tokens — token-storage decision made in #128 (a cookie, not `localStorage`; see `pocketbase/AUTH.md` § "For the frontend"), and implemented in Phase 7B as `internal/web/static/js/golftrack.js`
2. [x] Adjust API endpoint calls for Pocketbase format — the read-side format gap (camelCase, null totals, nested relations) is closed with new custom routes: `GET /api/courses`, `/api/courses/{id}`, `/api/rounds`, `/api/rounds/in-progress`, `/api/rounds/{id}`, pinned by `pocketbase/read_routes_test.go` and recorded in `pocketbase/API.md` § "Parity gaps" (gaps 1, 2, 4)
3. [x] Exercise every workflow below at the HTTP level, as the automated stand-in for a browser pass — `pocketbase/frontend_workflow_test.go`
4. [→] Test all frontend workflows in a browser — **moved to Phase 7B**, which builds the browser client; **done there**
5. [→] Verify CSS/Tailwind styling — **moved to Phase 7B**; **done there**

#### Workflows to Cover:
- Home page (resume in-progress round or list completed)
- Courses list/create/edit/delete — *list covered here; create/edit had no route
  until Phase 7B built the course write routes, and are covered from there by
  `TestFrontendWorkflowCourseAdmin`. "Delete" is archive: the contract has no
  course delete*
- Start round (pick course, play mode)
- Play round (add/undo/edit/delete shots)
- Complete round (calculate totals)
- Cancel round
- Review completed rounds

#### Validation Gate:
- [x] All API calls successful — every route above covered by HTTP-level tests
- [x] Round creation and play flow works — `routes_test.go`, `frontend_workflow_test.go`
- [x] Response shapes match what each page renders — `read_routes_test.go`
- [→] All page routes load — **moved to Phase 7B**, and met there
- [→] No JavaScript console errors — **moved to Phase 7B**, and met there
- [→] Styling looks correct — **moved to Phase 7B**, and met there

*Delivered in #128. The backend half — custom read routes returning the exact
shapes the frontend contract expects — is delivered and tested; see
`pocketbase/README.md` § "The Phase 7A (#128) gate" for the item-by-item status.
The three gate items marked → were written against a frontend that does not
exist on PocketBase; they are the Phase 7B gate now.*

---

### PHASE 7B: Frontend Rewrite (3-4 weeks)

**Objective:** Rebuild the GolfTrack UI as a PocketBase-native frontend that preserves every current behaviour

#### Why this phase exists

Phase 7A adapts *API calls*. It cannot adapt the pages, because the pages are
not a client that can be re-pointed — they are Django server-rendering:
`rounds/views.py` and `courses/views.py` render eleven Django templates, with
Alpine.js islands calling `fetch()` against markup Django has already produced
(`rounds/templates/rounds/play.html` bootstraps from a Django-rendered
`<script id="round-init">` blob), and django-allauth rendering the sign-in and
sign-out pages. PocketBase serves an API and static assets; it does not render
Django templates. Every page route, the template rendering, the per-page context,
the auth pages and the asset pipeline therefore have to be rebuilt, and no other
phase owns that work — Phase 11 in fact *deletes* it.

#### Framework decision — resolves Key Architectural Decision #2

**Server-rendered Go `html/template` pages inside the PocketBase binary, keeping
Alpine.js islands and Tailwind; PocketBase JS SDK for client-side auth and
writes.** Rationale:

- **It matches the token decision already made.** `AUTH.md` § "For the frontend"
  (#128) chose a cookie over `localStorage` precisely so "the server [is] able to
  gate a page render before any JavaScript runs". A pure client-side SPA has no
  server render to gate, so choosing one would reopen that decision.
- **It preserves the single-binary deployment** that motivated the migration
  (Decision #1, and the Phase 9 container). PocketBase's Go API supports this
  directly — `template.NewRegistry()` for rendering, `apis.Static` for assets,
  both served by the same process on the same origin, so there is no CORS
  surface and no second server.
- **It preserves the existing markup and behaviour.** Django templates and Go
  `html/template` are close enough that porting is largely mechanical, so the
  Alpine islands, the Tailwind classes and the mobile-first layout survive the
  move rather than being redesigned mid-migration.

*Alternatives rejected:* a static SPA in `pb_public` (Svelte/React) is the more
common PocketBase pattern and remains viable later, but it discards the existing
markup, adds a JS build toolchain, and contradicts the #128 cookie rationale.
Next.js (Decision #2's other branch) needs a Node server alongside the binary,
or a static export that gives up SSR — either way it works against the
single-container model.

#### Backend dependency — course writes — **built**

`API.md` § "Mapping to the current contract" mapped `POST /api/courses/` to
"`POST /api/collections/courses/records` + N hole creates" and
`PUT /api/courses/{id}` to "`PATCH` + hole reconciliation". A course form that
saved a name and eighteen pars through the generated endpoints would issue 19
non-atomic requests and could leave a course with a partial hole set — which
Phase 3 rule 8 then rejects at round creation, one request removed from the edit
that broke it. This phase needed the two custom write routes first, in the shape
Phase 3 established:

- [x] `POST /api/courses/` — create course and holes in one transaction
- [x] `PUT /api/courses/{id}` — update course and reconcile the hole set in one transaction

Both are admin-only (`courses/api.py` opens create and update with
`require_admin`): 401 anonymous, 403 for a signed-in player, and a PocketBase
superuser through, since it already bypasses the collection rules the generated
endpoints enforce. `CourseIn`'s validators are ported message for message and
run *before* the transaction opens, so a rejected payload writes nothing.
`PUT` reconciles rather than replaces — a hole still in the payload keeps its
record and its id, a hole absent from it is deleted, as `update_course` does.

Archive/unarchive stay on the generated `PATCH` (single field, already
admin-gated by the Phase 2 rules); there is no course *delete* in the contract.

Delivered with `pocketbase/internal/hooks/domain/courses/write.go`,
`pocketbase/course_write_routes_test.go`, and a courses leg added to
`pocketbase/frontend_workflow_test.go` — which also closes the one Phase 7A
workflow ("Courses list/create/edit/delete") that had no route to walk. See
`pocketbase/README.md` § "The Phase 7B backend dependency".

#### Page Routes to Rebuild:

| Current (Django) | Notes |
|---|---|
| `/` | home — resume in-progress round or list completed |
| `/courses/` | list |
| `/courses/new/`, `/courses/{id}/edit/` | admin-only; the Django form POST becomes an API call against the new course write routes |
| `/courses/{id}/` | detail |
| `/courses/archived/` | admin-only |
| `/courses/{id}/archive/`, `/courses/{id}/unarchive/` | admin-only POST actions |
| `/rounds/`, `/rounds/new/` | list, start-round form |
| `/rounds/{id}/`, `/rounds/{id}/play/`, `/rounds/{id}/review/` | detail, live play, review |
| `/accounts/login/`, `/accounts/logout/` | allauth pages replaced by our own, driven by `GET /api/collections/users/auth-methods` (which providers to render, whether to show a password form) |

#### Cross-cutting Work:
1. **Base layout and navigation** — port `templates/base.html`, including the auth-aware nav and admin-only links (`record.role == "ADMIN"`, advisory in the UI only; the server rules are the enforcement)
2. **Auth cookie handling** — write the SDK's `authStore` to the cookie per the #128 decision, read it server-side to gate a render, refresh via `POST /api/collections/users/auth-refresh`; sign-out discards the token
3. **Drop CSRF plumbing** — the `<meta name="csrf-token">` and its uses have no PocketBase equivalent; auth is a bearer token
4. **Vendor Alpine.js and htmx** — `base.html` loads both from unpkg under a Django CSP nonce. PocketBase emits no such nonce, and a PWA should not depend on a CDN at run time; serve them from the embedded static FS
5. **Asset pipeline** — retarget the Tailwind build (`bin/build-css.sh`, `make css`) at the new template tree and output into the embedded static FS
6. **PWA** — `manifest.webmanifest`, the icon set, `apple-touch-icon`, `theme-color`; `start_url` and icon paths change with the static mount point
7. **Error and not-found pages** — 404 and the admin-only 403, currently Django's

#### Deliverables:
- [x] `pocketbase/internal/web/` — page-route registration on `OnServe`, template registry, per-page handlers
- [x] `pocketbase/internal/web/templates/` — ported layouts and pages (`go:embed`)
- [x] `pocketbase/internal/web/static/` — Tailwind output, vendored Alpine and the PocketBase JS SDK, icons, manifest (`go:embed`). *htmx is not vendored: `base.html` loaded it, but no template ever used an `hx-` attribute*
- [x] `pocketbase/internal/web/auth.go` — cookie read implementing the `AUTH.md` contract. *Read, not read/write: the cookie is written by the SDK in the browser (`static/js/golftrack.js`), and only its token half is trusted server-side*
- [x] Custom course write routes (see "Backend dependency" above), with `API.md` updated
- [x] `pocketbase/web_test.go` — page-route tests: status codes, signed-out redirects, admin gating
- [x] `pocketbase/scripts/browser-walkthrough.mjs` — the seven workflows in a real browser, failing on any console error or third-party request. *Not in the original list; the gate asks for a browser pass, and a script makes it repeatable rather than a one-off*
- [x] `pocketbase/README.md` § "Frontend" — layout, how to run it, how the asset build works

#### Validation Gate:
- [x] Every page route in the table above loads for a signed-in user — `web_test.go`
- [x] A signed-out visitor is redirected to sign-in from each gated page (Django's `@login_required_view`), `?next=` included
- [x] Admin-only pages and actions refuse a non-admin (Django's `@admin_required`) — including a cookie that claims `role: ADMIN`
- [~] Sign-in works through both OAuth providers, and sign-out discards the token — the flow, the cookie and sign-out are exercised; **the live provider exchange stays owner-verified**, as in Phase 4, because it needs real credentials and a browser
- [x] Frontend auth cookie validated — closes the item deferred from Phase 4 (#125)
- [x] All seven Phase 7A workflows pass in a real browser, not just at HTTP level — `scripts/browser-walkthrough.mjs`
- [x] No JavaScript console errors — the walkthrough fails on any
- [x] Styling matches the current app; PWA still installable and icons served
- [x] No run-time requests to any external CDN — asserted on the markup *and* on every request the browser makes
- [x] `make pb-test` green

*Delivered. Three deviations from this section, all recorded in
`pocketbase/README.md` § "Frontend": htmx is not vendored (nothing used it), the
archive/unarchive and course-form POSTs became API calls rather than page routes
(so the cookie gates renders only and there is no CSRF surface to replace), and
the sign-in page reads the enabled providers from the users collection directly
instead of from `GET /api/collections/users/auth-methods` — same answer, one
fewer round trip, and it cannot disagree with the endpoint because it is the
same process.*

---

### PHASE 8: Performance & Optimization (1 week)

**Objective:** Ensure Pocketbase performance meets production requirements

#### Tasks:
1. [x] Load testing (10, 50, 100 concurrent users) — `pocketbase/loadtest_test.go`, `make pb-loadtest`
2. [x] Optimize hook performance (profile, batch operations, ~~caching~~) — the per-record reads in `internal/hooks/domain` are now set-at-a-time; **no cache was added**, and the report says why
3. [x] Review database indexing — `TestHotQueriesUseAnIndex` reads SQLite's plan for every hot query and fails on a scan; it found two, both now in `pb_schema.json`
4. [x] Monitor memory/resource usage — `TestSustainedTrafficDoesNotLeak`

#### Deliverables:
- [x] `pocketbase/performance_report.md` — benchmarks vs. Django
- [x] Optimized hook implementations — `internal/records` gained batched lookups; `courses`, `rounds` and `roundholes` build responses from them

#### Validation Gate:
- [x] Response times acceptable (<200ms p95 for most endpoints) — every read endpoint at 100 concurrent players; every endpoint including writes at a realistic offered load, where p95 is under 5 ms
- [x] No memory leaks — heap and goroutine count flat across a batch five times the warm-up's
- [x] Concurrency tests pass — `concurrency_test.go` unchanged and green; the new sweep runs 10/50/100 players with zero failed requests
- [x] Queries efficient — the round detail read went from 41 statements to 6 and no longer grows with the round; asserted at two data sizes

*Delivered in #129. The finding worth carrying past this phase:* ***the write
ceiling is the deployment's, not the code's.*** *Write throughput is flat at
~290 req/s from ten concurrent players onward, because PocketBase runs writes on
a one-connection pool over SQLite's single writer. That is some four orders of
magnitude beyond this application's real demand, but it is the number **Phase 9
(#130)** should have before choosing a container and volume shape.*

*One deviation from this section: it lists caching among the optimisation
techniques and none was added. The expensive reads are now 3–6 statements against
indexed SQLite in the same process, so a cache would add an invalidation problem
for no measured gain. See `pocketbase/performance_report.md` § "What was
deliberately not done".*

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
- [ ] Static assets served correctly — the Phase 7B embedded asset FS (Tailwind output, vendored Alpine and the PocketBase JS SDK, icons, manifest), not Django's `collectstatic`/WhiteNoise output

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
- ~~**Zero-downtime:** Run Django and Pocketbase side-by-side, switch proxy after validation~~ — *not available. There is no proxy layer to switch: exe.dev publishes one fixed host port per VM (3000 in production, 8000 on dev) straight to a container, so "both up, flip the route" has nowhere to live, and the two containers cannot both hold the port. Adding a reverse proxy purely to enable this was rejected — it would be new production infrastructure introduced during a cutover, for an app whose downtime budget is a container restart.*
- **Single-server:** ~~Maintenance window to migrate data and~~ switch containers — *the strategy used. No maintenance window and no data migration: #127 established that no data was ever entered in production, so the swap is `docker stop` + `docker run`, a few seconds of 502 while the new container boots.*

#### Rollback Plan:
- Keep Django image available in GHCR — `:django-latest`, copied aside by the `build` job once, before it first overwrites `:latest`
- ~~Point proxy back to Django if issues arise~~ — run `bin/rollback-prod.sh`, which swaps the container back (same reason as above: no proxy)
- Recovery time: <30 minutes — *in practice a pull plus a boot, because the cutover never touches Django's data directory or its `django` Litestream replica path; rollback is a container swap, not a restore*
- Test rollback procedure in staging first

#### Validation Gate:
- [x] CI/CD pipeline passes
- [x] Pocketbase image pushed to GHCR — `:latest` and `:pocketbase-<sha>` from `main`, `:pocketbase-dev` from branches
- [x] Dev server deployment succeeds
- [ ] ~~Staging deployment succeeds~~ — *there is no staging environment; the dev server is it. Every branch push deploys to `golftrack-dev.exe.xyz`, which runs the same image production does. What it cannot exercise is OAuth (dev has no client apps and uses password login), so "full staging test with real OAuth" in task 3 is necessarily a production check, listed in `DEPLOYMENT.md` § "After the deploy goes green".*
- [ ] Production deployment succeeds — *happens on the first push to `main` after the cutover merges*
- [ ] Users can log in and use app

*Delivered in #131.*

---

### PHASE 11: Decommissioning & Cleanup (1 week)

**Objective:** Remove Django code and finalize the migration

#### Tasks:
1. Remove Django code (`config/`, `accounts/`, `core/`, `courses/`, `rounds/`, `manage.py`, `conftest.py`, `pyproject.toml`)
2. Remove the Django frontend that Phase 7B replaced — root `templates/`, and the parts of `static/` now embedded in the binary (`static/js/round-play.js`, `static/src/input.css`, the compiled `static/css/app.css`, and the icons/manifest, all of which now live under `pocketbase/internal/web/static/`). Also `bin/build-css.sh`, `tailwind.config.js` and `make css`, whose PocketBase twins are `bin/build-pb-css.sh`, `tailwind.pocketbase.config.js` and `make pb-css`. *This was previously implicit: `courses/` and `rounds/` contain `views.py` and `templates/`, so step 1 deletes the UI, and the root `templates/`+`static/` trees were not listed at all.*
3. Remove Next.js legacy code (`app/`, `components/`, `lib/`, `prisma/`)
4. Reorganize repository structure
5. Update documentation (README, DEPLOYMENT.md, delete DJANGO.md)
6. Archive historical branches

#### Deliverables:
- Cleaned repository structure
- POCKETBASE.md architecture documentation
- Final version tags for Django and Next.js

#### Validation Gate:
- [ ] Django code removed
- [ ] Django templates and superseded static assets removed; no page or asset the app still serves lives outside `pocketbase/`
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
| Performance regression | Medium | ~~Load testing in Phase 8, optimize hooks, compare to Django baseline~~ — *done in #129, and the risk did materialise: four read paths were issuing a query per record, which the Django ORM's `prefetch_related` had been hiding. Fixed, and pinned by statement-count assertions that fail if a query is added back* |
| OAuth provider misconfiguration | Medium | Test with real credentials in staging, documented runbooks |
| Concurrent shot edits (race condition) | Medium | IMMEDIATE transaction mode, concurrency tests, Pocketbase DB locking |
| Litestream replication issues | Medium | Test restore flow, S3 bucket backups, monitor replication lag |
| Breaking frontend changes | Medium | Phase 7A pins the response shapes with tests and documents every deviation in `API.md`; Phase 7B rebuilds against those shapes and gates on a browser pass of all seven workflows |
| Frontend rewrite regresses behaviour the tests do not describe | Medium | Port page-for-page from the Django templates rather than redesigning; keep Alpine/Tailwind so markup and styling carry over; `web_test.go` covers page routes, auth redirects and admin gating |
| Phase 7B slips and blocks the cutover | Medium | It is the only phase with no shipped predecessor to lean on. Build the course write routes first (they are backend work with existing test patterns), then port pages in dependency order — layout, auth, courses, rounds — so a slip costs pages rather than the whole phase |
| Deployment/container issues | Low | Docker best practices, health checks, CI/CD validation |

---

## Migration Checklist

### Pre-Migration (Before Phase 1)
- [ ] Stakeholder approval of Pocketbase adoption
- [ ] Access to production VM and S3/DO Spaces
- [ ] GitHub Actions secrets ready (OAuth, deploy keys)
- [ ] Backup of current Django database

### Pre-Production (Before Phase 10)
- [ ] All phases 1-9 complete and tested, 7B included — the cutover has no UI without it
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
9. `pocketbase/internal/web/` — the rewritten frontend: page routes, templates, embedded static assets and the auth-cookie handling (Phase 7B). *Previously listed as `templates/base.html`, a Django template PocketBase cannot render.*
10. `.github/workflows/deploy.yml` — CI/CD pipeline for Pocketbase

---

## Key Architectural Decisions

1. **Hook Language:** Go from the start — PocketBase as a Go framework, one portable binary embedding server, schema and hooks. *(Revised during #122 review; the original decision was JavaScript hooks for Phase 3-8 with a possible later Go migration.)*
2. **Frontend Framework:** Server-rendered Go `html/template` pages inside the PocketBase binary, keeping Alpine.js islands and Tailwind, with the PocketBase JS SDK for client-side auth and writes. *(Resolved in Phase 7B; the original entry — "Keep HTMX + Alpine.js, or revert to Next.js (separate concern)" — was an open either/or, and treating the frontend as a separate concern is what left the rewrite unowned. Rationale and rejected alternatives are in Phase 7B.)*
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
| 7A. Frontend API | 1-2 weeks | API adaptation and read-route parity |
| 7B. Frontend Rewrite | 3-4 weeks | PocketBase-native UI replacing the Django templates |
| 8. Performance | 1 week | Load testing and optimization |
| 9. Docker/Deploy | 1-2 weeks | Container setup and rollout prep |
| 10. CI/CD & Rollout | 1-2 weeks | Production deployment |
| 11. Cleanup | 1 week | Remove Django/Next.js code |

**Total: 15-21 weeks (4-5 months)** — 3-4 weeks more than the original estimate,
which carried no frontend rewrite.

---

## Next Steps

1. **Review & Approval:** Validate plan with team/stakeholders
2. **Phase 1 Start:** Set up Pocketbase development environment
3. **Incremental Execution:** Complete each phase with validation gates
4. **Regular Check-ins:** Report progress and adjust timeline as needed
5. **Documentation:** Keep POCKETBASE.md updated as implementation proceeds

---

*This plan is designed for incremental execution with clear validation gates at each phase, allowing course corrections without committing the entire system until confidence is high.*
