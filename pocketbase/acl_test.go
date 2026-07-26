package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks"
)

// Phase 2 (#123) access-rule suite.
//
// These run against the generated record endpoints over real HTTP (via
// tests.ApiScenario) with real auth tokens, so what is asserted is what a
// client actually gets — not what the rule expression looks like.
//
// Three PocketBase behaviours to keep in mind when reading the expectations:
//
//   - A *list* rule is applied as a filter, not as a gate. A caller who may see
//     nothing gets 200 with an empty page, never 403. So "denied" for list means
//     `"totalItems":0` plus the absence of the ids that were hidden.
//   - A failing *view/update/delete* rule returns 404, so a response cannot be
//     used to probe whether a record id exists.
//   - A failing *create* rule returns 400.
//
// 403 is reserved for `nil` rules (superuser-only), which after this phase none
// of the six collections have — see TestEveryCollectionHasExplicitRules.

// fixture is the seeded world each scenario runs against: two regular users
// with a round apiece, an admin, a superuser, and one course.
type fixture struct {
	app *tests.TestApp

	owner     *core.Record // regular user; owns the idOwner* records
	other     *core.Record // regular user; owns the idOther* records
	admin     *core.Record // users.role = ADMIN
	superuser *core.Record
}

func newFixture(t testing.TB) *fixture {
	t.Helper()

	app := newTestApp(t)

	f := &fixture{app: app}
	f.owner = createUser(t, app, idOwner, "owner@golftrack.test", hooks.UserRoleUser)
	f.other = createUser(t, app, idOther, "other@golftrack.test", hooks.UserRoleUser)
	f.admin = createUser(t, app, idAdmin, "admin@golftrack.test", hooks.UserRoleAdmin)
	f.superuser = createSuperuser(t, app, "super@golftrack.test")

	course := createCourse(t, app, idCourse, "Fixture Links", 18)
	createCourseHole(t, app, idCourseHole, course, 1, 4)

	ownerRound := createRound(t, app, idOwnerRound, f.owner, course, hooks.RoundStatusInProgress)
	ownerHole := createRoundHole(t, app, idOwnerHole, ownerRound, 1, 4)
	createShot(t, app, idOwnerShot, ownerHole, 1, "Driver")

	otherRound := createRound(t, app, idOtherRound, f.other, course, hooks.RoundStatusInProgress)
	otherHole := createRoundHole(t, app, idOtherHole, otherRound, 1, 4)
	createShot(t, app, idOtherShot, otherHole, 1, "7 Iron")

	return f
}

// actor picks the record a scenario authenticates as. anonymous returns nil.
type actor func(*fixture) *core.Record

func anonymous(*fixture) *core.Record     { return nil }
func asOwner(f *fixture) *core.Record     { return f.owner }
func asOther(f *fixture) *core.Record     { return f.other }
func asAdmin(f *fixture) *core.Record     { return f.admin }
func asSuperuser(f *fixture) *core.Record { return f.superuser }

// run seeds a fresh app for the scenario and authenticates it as who.
//
// The app cannot be shared between scenarios: tests.ApiScenario calls
// apis.NewRouter and triggers OnServe per scenario, and PocketBase's UI
// extension routes re-register on every trigger, which panics the second time
// around on the same app. Seeding inside the factory is why the fixture ids are
// fixed — the URLs and expectations are written before the app exists.
func run(t *testing.T, who actor, s tests.ApiScenario) {
	t.Helper()

	s.DisableTestAppCleanup = true // newTestApp already registers t.Cleanup
	s.TestAppFactory = func(tb testing.TB) *tests.TestApp {
		f := newFixture(tb)
		s.Headers = authHeader(tb, who(f))
		return f.app
	}
	s.Test(t)
}

func body(json string) *strings.Reader { return strings.NewReader(json) }

func recordsURL(collection string) string {
	return "/api/collections/" + collection + "/records"
}

func recordURL(collection, id string) string {
	return recordsURL(collection) + "/" + id
}

// -----------------------------------------------------------------------------
// courses / course_holes — readable by any authenticated user, writable by admins
// -----------------------------------------------------------------------------

func TestCoursesAreReadableByAnyAuthenticatedUser(t *testing.T) {
	for name, who := range map[string]actor{
		"regular user": asOwner,
		"admin":        asAdmin,
		"superuser":    asSuperuser,
	} {
		run(t, who, tests.ApiScenario{
			Name:            "list courses as " + name,
			Method:          http.MethodGet,
			URL:             recordsURL(hooks.NameCourses),
			ExpectedStatus:  200,
			ExpectedContent: []string{`"totalItems":1`, `"id":"` + idCourse + `"`},
		})
		run(t, who, tests.ApiScenario{
			Name:            "view course hole as " + name,
			Method:          http.MethodGet,
			URL:             recordURL(hooks.NameCourseHoles, idCourseHole),
			ExpectedStatus:  200,
			ExpectedContent: []string{`"id":"` + idCourseHole + `"`},
		})
	}
}

func TestCoursesAreHiddenFromAnonymousCallers(t *testing.T) {
	run(t, anonymous, tests.ApiScenario{
		Name:               "anonymous course list is empty",
		Method:             http.MethodGet,
		URL:                recordsURL(hooks.NameCourses),
		ExpectedStatus:     200,
		ExpectedContent:    []string{`"totalItems":0`},
		NotExpectedContent: []string{idCourse},
	})
	run(t, anonymous, tests.ApiScenario{
		Name:            "anonymous course view is 404",
		Method:          http.MethodGet,
		URL:             recordURL(hooks.NameCourses, idCourse),
		ExpectedStatus:  404,
		ExpectedContent: []string{`"status":404`},
	})
}

func TestOnlyAdminsCanWriteCourses(t *testing.T) {
	const newCourse = `{"name":"Written Course","hole_count":9}`

	run(t, asOwner, tests.ApiScenario{
		Name:            "regular user cannot create a course",
		Method:          http.MethodPost,
		URL:             recordsURL(hooks.NameCourses),
		Body:            body(newCourse),
		ExpectedStatus:  400,
		ExpectedContent: []string{`"status":400`},
	})
	run(t, anonymous, tests.ApiScenario{
		Name:            "anonymous caller cannot create a course",
		Method:          http.MethodPost,
		URL:             recordsURL(hooks.NameCourses),
		Body:            body(newCourse),
		ExpectedStatus:  400,
		ExpectedContent: []string{`"status":400`},
	})
	run(t, asOwner, tests.ApiScenario{
		Name:            "regular user cannot rename a course",
		Method:          http.MethodPatch,
		URL:             recordURL(hooks.NameCourses, idCourse),
		Body:            body(`{"name":"Renamed"}`),
		ExpectedStatus:  404,
		ExpectedContent: []string{`"status":404`},
	})
	run(t, asOwner, tests.ApiScenario{
		Name:            "regular user cannot delete a course hole",
		Method:          http.MethodDelete,
		URL:             recordURL(hooks.NameCourseHoles, idCourseHole),
		ExpectedStatus:  404,
		ExpectedContent: []string{`"status":404`},
	})
	run(t, asAdmin, tests.ApiScenario{
		Name:            "admin can create a course",
		Method:          http.MethodPost,
		URL:             recordsURL(hooks.NameCourses),
		Body:            body(newCourse),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"name":"Written Course"`},
	})
	run(t, asAdmin, tests.ApiScenario{
		Name:            "admin can rename a course",
		Method:          http.MethodPatch,
		URL:             recordURL(hooks.NameCourses, idCourse),
		Body:            body(`{"name":"Renamed By Admin"}`),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"name":"Renamed By Admin"`},
	})
	run(t, asAdmin, tests.ApiScenario{
		Name:           "admin can delete a course hole",
		Method:         http.MethodDelete,
		URL:            recordURL(hooks.NameCourseHoles, idCourseHole),
		ExpectedStatus: 204,
	})
}

// -----------------------------------------------------------------------------
// rounds — own rounds only; admins read all
// -----------------------------------------------------------------------------

func TestRoundsAreScopedToTheirOwner(t *testing.T) {
	run(t, asOwner, tests.ApiScenario{
		Name:               "owner lists only their own round",
		Method:             http.MethodGet,
		URL:                recordsURL(hooks.NameRounds),
		ExpectedStatus:     200,
		ExpectedContent:    []string{`"totalItems":1`, `"id":"` + idOwnerRound + `"`},
		NotExpectedContent: []string{idOtherRound},
	})
	run(t, asOther, tests.ApiScenario{
		Name:            "another user cannot view someone else's round",
		Method:          http.MethodGet,
		URL:             recordURL(hooks.NameRounds, idOwnerRound),
		ExpectedStatus:  404,
		ExpectedContent: []string{`"status":404`},
	})
	run(t, asOther, tests.ApiScenario{
		Name:            "another user cannot delete someone else's round",
		Method:          http.MethodDelete,
		URL:             recordURL(hooks.NameRounds, idOwnerRound),
		ExpectedStatus:  404,
		ExpectedContent: []string{`"status":404`},
	})
	run(t, anonymous, tests.ApiScenario{
		Name:               "anonymous round list is empty",
		Method:             http.MethodGet,
		URL:                recordsURL(hooks.NameRounds),
		ExpectedStatus:     200,
		ExpectedContent:    []string{`"totalItems":0`},
		NotExpectedContent: []string{idOwnerRound, idOtherRound},
	})
}

func TestAdminsReadEveryRound(t *testing.T) {
	run(t, asAdmin, tests.ApiScenario{
		Name:            "admin lists every round",
		Method:          http.MethodGet,
		URL:             recordsURL(hooks.NameRounds),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"totalItems":2`, `"id":"` + idOwnerRound + `"`, `"id":"` + idOtherRound + `"`},
	})
	run(t, asAdmin, tests.ApiScenario{
		Name:            "admin views another user's round",
		Method:          http.MethodGet,
		URL:             recordURL(hooks.NameRounds, idOwnerRound),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"id":"` + idOwnerRound + `"`},
	})
}

// TestAdminsCannotWriteAnotherUsersRound pins the read/write split: the issue
// grants admins visibility into every round, not the ability to score one.
func TestAdminsCannotWriteAnotherUsersRound(t *testing.T) {
	run(t, asAdmin, tests.ApiScenario{
		Name:            "admin cannot update another user's round",
		Method:          http.MethodPatch,
		URL:             recordURL(hooks.NameRounds, idOwnerRound),
		Body:            body(`{"current_hole":7}`),
		ExpectedStatus:  404,
		ExpectedContent: []string{`"status":404`},
	})
	run(t, asAdmin, tests.ApiScenario{
		Name:            "admin cannot delete another user's round",
		Method:          http.MethodDelete,
		URL:             recordURL(hooks.NameRounds, idOwnerRound),
		ExpectedStatus:  404,
		ExpectedContent: []string{`"status":404`},
	})
	run(t, asAdmin, tests.ApiScenario{
		Name:            "admin cannot delete another user's shot",
		Method:          http.MethodDelete,
		URL:             recordURL(hooks.NameShots, idOwnerShot),
		ExpectedStatus:  404,
		ExpectedContent: []string{`"status":404`},
	})
}

func TestRoundsCannotBeCreatedOrReassignedForAnotherUser(t *testing.T) {
	run(t, asOwner, tests.ApiScenario{
		Name:   "cannot create a round owned by someone else",
		Method: http.MethodPost,
		URL:    recordsURL(hooks.NameRounds),
		Body: body(`{"user":"` + idOther + `","course":"` + idCourse +
			`","status":"in_progress","play_mode":"full","started_at":"2026-07-26 10:00:00.000Z","current_hole":1}`),
		ExpectedStatus:  400,
		ExpectedContent: []string{`"status":400`},
	})
	run(t, asOwner, tests.ApiScenario{
		Name:            "cannot hand an existing round to another user",
		Method:          http.MethodPatch,
		URL:             recordURL(hooks.NameRounds, idOwnerRound),
		Body:            body(`{"user":"` + idOther + `"}`),
		ExpectedStatus:  404,
		ExpectedContent: []string{`"status":404`},
	})
	run(t, asOwner, tests.ApiScenario{
		Name:            "owner can still update their own round",
		Method:          http.MethodPatch,
		URL:             recordURL(hooks.NameRounds, idOwnerRound),
		Body:            body(`{"current_hole":5}`),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"current_hole":5`},
	})
}

// -----------------------------------------------------------------------------
// round_holes / shots — reached through the owning round
// -----------------------------------------------------------------------------

func TestRoundHolesFollowTheirRound(t *testing.T) {
	run(t, asOwner, tests.ApiScenario{
		Name:               "owner lists only their own holes",
		Method:             http.MethodGet,
		URL:                recordsURL(hooks.NameRoundHoles),
		ExpectedStatus:     200,
		ExpectedContent:    []string{`"totalItems":1`, `"id":"` + idOwnerHole + `"`},
		NotExpectedContent: []string{idOtherHole},
	})
	run(t, asOther, tests.ApiScenario{
		Name:            "another user cannot view someone else's hole",
		Method:          http.MethodGet,
		URL:             recordURL(hooks.NameRoundHoles, idOwnerHole),
		ExpectedStatus:  404,
		ExpectedContent: []string{`"status":404`},
	})
	run(t, asOther, tests.ApiScenario{
		Name:            "another user cannot add a hole to someone else's round",
		Method:          http.MethodPost,
		URL:             recordsURL(hooks.NameRoundHoles),
		Body:            body(`{"round":"` + idOwnerRound + `","hole_number":2,"par":4,"strokes":0}`),
		ExpectedStatus:  400,
		ExpectedContent: []string{`"status":400`},
	})
	run(t, asOwner, tests.ApiScenario{
		Name:            "owner can add a hole to their own round",
		Method:          http.MethodPost,
		URL:             recordsURL(hooks.NameRoundHoles),
		Body:            body(`{"round":"` + idOwnerRound + `","hole_number":2,"par":4,"strokes":0}`),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"hole_number":2`},
	})
	run(t, asAdmin, tests.ApiScenario{
		Name:            "admin reads every hole",
		Method:          http.MethodGet,
		URL:             recordsURL(hooks.NameRoundHoles),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"totalItems":2`},
	})
}

// TestShotsFollowTheirRoundThroughTwoRelations exercises the deepest rule in the
// schema: shots reach their owner across round_hole -> round -> user.
func TestShotsFollowTheirRoundThroughTwoRelations(t *testing.T) {
	run(t, asOwner, tests.ApiScenario{
		Name:               "owner lists only their own shots",
		Method:             http.MethodGet,
		URL:                recordsURL(hooks.NameShots),
		ExpectedStatus:     200,
		ExpectedContent:    []string{`"totalItems":1`, `"id":"` + idOwnerShot + `"`},
		NotExpectedContent: []string{idOtherShot},
	})
	run(t, asOther, tests.ApiScenario{
		Name:            "another user cannot view someone else's shot",
		Method:          http.MethodGet,
		URL:             recordURL(hooks.NameShots, idOwnerShot),
		ExpectedStatus:  404,
		ExpectedContent: []string{`"status":404`},
	})
	run(t, asOther, tests.ApiScenario{
		Name:            "another user cannot re-club someone else's shot",
		Method:          http.MethodPatch,
		URL:             recordURL(hooks.NameShots, idOwnerShot),
		Body:            body(`{"club":"Putter"}`),
		ExpectedStatus:  404,
		ExpectedContent: []string{`"status":404`},
	})
	run(t, asOther, tests.ApiScenario{
		Name:            "another user cannot add a shot to someone else's hole",
		Method:          http.MethodPost,
		URL:             recordsURL(hooks.NameShots),
		Body:            body(`{"round_hole":"` + idOwnerHole + `","shot_number":2,"club":"Wedge"}`),
		ExpectedStatus:  400,
		ExpectedContent: []string{`"status":400`},
	})
	run(t, asOwner, tests.ApiScenario{
		Name:            "owner can re-club their own shot",
		Method:          http.MethodPatch,
		URL:             recordURL(hooks.NameShots, idOwnerShot),
		Body:            body(`{"club":"Putter"}`),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"club":"Putter"`},
	})
	run(t, asOwner, tests.ApiScenario{
		Name:           "owner can delete their own shot",
		Method:         http.MethodDelete,
		URL:            recordURL(hooks.NameShots, idOwnerShot),
		ExpectedStatus: 204,
	})
	run(t, asAdmin, tests.ApiScenario{
		Name:            "admin reads every shot",
		Method:          http.MethodGet,
		URL:             recordsURL(hooks.NameShots),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"totalItems":2`},
	})
}

// -----------------------------------------------------------------------------
// users
// -----------------------------------------------------------------------------

func TestUsersSeeOnlyThemselvesUnlessAdmin(t *testing.T) {
	run(t, asOwner, tests.ApiScenario{
		Name:               "user lists only their own record",
		Method:             http.MethodGet,
		URL:                recordsURL(hooks.NameUsers),
		ExpectedStatus:     200,
		ExpectedContent:    []string{`"totalItems":1`, `"id":"` + idOwner + `"`},
		NotExpectedContent: []string{idOther, idAdmin},
	})
	run(t, asOwner, tests.ApiScenario{
		Name:            "user cannot view another user",
		Method:          http.MethodGet,
		URL:             recordURL(hooks.NameUsers, idOther),
		ExpectedStatus:  404,
		ExpectedContent: []string{`"status":404`},
	})
	run(t, asOwner, tests.ApiScenario{
		Name:            "user cannot delete another user",
		Method:          http.MethodDelete,
		URL:             recordURL(hooks.NameUsers, idOther),
		ExpectedStatus:  404,
		ExpectedContent: []string{`"status":404`},
	})
	run(t, asAdmin, tests.ApiScenario{
		Name:            "admin lists every user",
		Method:          http.MethodGet,
		URL:             recordsURL(hooks.NameUsers),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"totalItems":3`, `"id":"` + idOwner + `"`, `"id":"` + idOther + `"`},
	})
	run(t, anonymous, tests.ApiScenario{
		Name:               "anonymous user list is empty",
		Method:             http.MethodGet,
		URL:                recordsURL(hooks.NameUsers),
		ExpectedStatus:     200,
		ExpectedContent:    []string{`"totalItems":0`},
		NotExpectedContent: []string{idOwner, idAdmin},
	})
}

// TestRoleIsNotSelfAssignable is the privilege-escalation guard: `role` is an
// ordinary field on a collection users can update, so without the
// `@request.body.role` clause in the update rule any user could grant
// themselves ADMIN and inherit read access to every round.
func TestRoleIsNotSelfAssignable(t *testing.T) {
	run(t, asOwner, tests.ApiScenario{
		Name:            "user cannot promote themselves to ADMIN",
		Method:          http.MethodPatch,
		URL:             recordURL(hooks.NameUsers, idOwner),
		Body:            body(`{"role":"ADMIN"}`),
		ExpectedStatus:  404,
		ExpectedContent: []string{`"status":404`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
			record, err := app.FindRecordById(hooks.NameUsers, idOwner)
			if err != nil {
				t.Fatalf("reload owner: %v", err)
			}
			if got := record.GetString(hooks.FieldRole); got != hooks.UserRoleUser {
				t.Fatalf("owner role = %q, want %q", got, hooks.UserRoleUser)
			}
		},
	})
	run(t, asOwner, tests.ApiScenario{
		Name:            "user cannot demote an admin",
		Method:          http.MethodPatch,
		URL:             recordURL(hooks.NameUsers, idAdmin),
		Body:            body(`{"role":"USER"}`),
		ExpectedStatus:  404,
		ExpectedContent: []string{`"status":404`},
	})
	run(t, asOwner, tests.ApiScenario{
		Name:            "user can still edit their own profile",
		Method:          http.MethodPatch,
		URL:             recordURL(hooks.NameUsers, idOwner),
		Body:            body(`{"display_name":"Owner McOwnerface"}`),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"display_name":"Owner McOwnerface"`},
	})
	run(t, asOwner, tests.ApiScenario{
		Name:            "resending the unchanged role is accepted",
		Method:          http.MethodPatch,
		URL:             recordURL(hooks.NameUsers, idOwner),
		Body:            body(`{"role":"USER","display_name":"Owner"}`),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"display_name":"Owner"`},
	})
}

// TestSignupIsOAuth2Only pins the users create rule. Sign-up is OAuth2-only
// (Phase 4, #125), so the rule is `@request.context = "oauth2"` — a context no
// direct API create can produce. Closing password sign-up also closes the
// second half of the escalation path: a walk-in account created with
// `"role":"ADMIN"`.
func TestSignupIsOAuth2Only(t *testing.T) {
	run(t, anonymous, tests.ApiScenario{
		Name:            "anonymous password sign-up is rejected",
		Method:          http.MethodPost,
		URL:             recordsURL(hooks.NameUsers),
		Body:            body(`{"email":"walkin@golftrack.test","password":"averylongpassword","passwordConfirm":"averylongpassword"}`),
		ExpectedStatus:  400,
		ExpectedContent: []string{`"status":400`},
	})
	run(t, asOwner, tests.ApiScenario{
		Name:            "an authenticated user cannot mint an admin account",
		Method:          http.MethodPost,
		URL:             recordsURL(hooks.NameUsers),
		Body:            body(`{"email":"minted@golftrack.test","password":"averylongpassword","passwordConfirm":"averylongpassword","role":"ADMIN"}`),
		ExpectedStatus:  400,
		ExpectedContent: []string{`"status":400`},
	})
}

// -----------------------------------------------------------------------------
// superusers
// -----------------------------------------------------------------------------

func TestSuperusersBypassEveryRule(t *testing.T) {
	for _, tc := range []struct {
		collection string
		totalItems int
	}{
		{hooks.NameUsers, 3},
		{hooks.NameCourses, 1},
		{hooks.NameCourseHoles, 1},
		{hooks.NameRounds, 2},
		{hooks.NameRoundHoles, 2},
		{hooks.NameShots, 2},
	} {
		run(t, asSuperuser, tests.ApiScenario{
			Name:            "superuser lists " + tc.collection,
			Method:          http.MethodGet,
			URL:             recordsURL(tc.collection),
			ExpectedStatus:  200,
			ExpectedContent: []string{`"totalItems":` + itoa(tc.totalItems)},
		})
	}

	run(t, asSuperuser, tests.ApiScenario{
		Name:            "superuser views another user's round",
		Method:          http.MethodGet,
		URL:             recordURL(hooks.NameRounds, idOwnerRound),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"id":"` + idOwnerRound + `"`},
	})
	// The only path that can grant ADMIN over the API, and the reason the
	// Admin UI stays usable for role changes until Phase 4 automates them.
	run(t, asSuperuser, tests.ApiScenario{
		Name:            "superuser can set a role",
		Method:          http.MethodPatch,
		URL:             recordURL(hooks.NameUsers, idOther),
		Body:            body(`{"role":"ADMIN"}`),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"role":"ADMIN"`},
	})
}

// -----------------------------------------------------------------------------
// the rules themselves
// -----------------------------------------------------------------------------

// TestEveryCollectionHasExplicitRules guards the two failure modes a rule set
// can silently fall into: `nil`, which makes an endpoint superuser-only and 403s
// every real caller, and `""`, which makes it public.
func TestEveryCollectionHasExplicitRules(t *testing.T) {
	app := newTestApp(t)

	for _, name := range hooks.CollectionNames {
		collection, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			t.Fatalf("find collection %q: %v", name, err)
		}

		for ruleName, rule := range map[string]*string{
			"listRule":   collection.ListRule,
			"viewRule":   collection.ViewRule,
			"createRule": collection.CreateRule,
			"updateRule": collection.UpdateRule,
			"deleteRule": collection.DeleteRule,
		} {
			switch {
			case rule == nil:
				t.Errorf("%s.%s is nil (superuser-only)", name, ruleName)
			case *rule == "":
				t.Errorf("%s.%s is empty (public)", name, ruleName)
			}
		}
	}
}
