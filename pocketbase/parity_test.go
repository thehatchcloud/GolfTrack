package main

import (
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
)

// Phase 5 (#126) API parity suite.
//
// The three earlier suites each cover one slice: acl_test.go the access rules on
// the generated endpoints, routes_test.go the happy paths and domain errors of
// the Phase 3 custom routes, auth_test.go sign-in. What none of them cover is
// the parity question itself — given the same request, does a caller get what
// the Django contract (config/api.py, rounds/api.py, courses/api.py) returns?
//
// So this file is deliberately two kinds of test:
//
//   - Assertions of behaviour that *does* match, and must keep matching:
//     ownership on the custom routes, the status codes, pagination/filtering/
//     sorting.
//   - Characterisation tests of the seven deviations recorded in API.md. Those
//     assert the PocketBase behaviour as it is, so that closing a gap in Phase 7
//     (#128) is a deliberate act with a failing test attached, rather than a
//     silent change nobody notices until the frontend renders "even par" on an
//     unfinished round.
//
// Every characterisation test names the gap it pins in its doc comment. If you
// are here because one failed, the fix is to update API.md's gap table in the
// same change.

func playAdmin(f *playFixture) *core.Record     { return f.admin }
func playSuperuser(f *playFixture) *core.Record { return f.superuser }

// -----------------------------------------------------------------------------
// ACL on the custom routes
// -----------------------------------------------------------------------------

// customRoute is one entry in the custom-route surface: the method, the URL
// under the owner's round, and the 404 message the route uses when the caller
// cannot reach the record the path names.
type customRoute struct {
	method   string
	url      string
	body     string
	notFound string
}

// customRoutes is every route Phase 3 registered, aimed at the *owner's*
// records. TestCustomRoutesRequireAuth walks the same surface anonymously; these
// scenarios walk it as somebody else.
//
// POST /api/rounds/ is absent: it has no target record to be denied, and
// routes_test.go covers its conflict path.
var customRoutes = []customRoute{
	{http.MethodPost, "/api/rounds/" + idPlayRound + "/complete", `{}`, "Round not found"},
	{http.MethodPost, "/api/rounds/" + idPlayRound + "/cancel", "", "Round not found"},
	{http.MethodPatch, "/api/rounds/" + idPlayRound + "/current-hole", `{"currentHole":2}`, "Round not found"},
	{http.MethodPost, "/api/rounds/" + idPlayRound + "/holes/1/shots", `{"club":"Driver"}`, "Round hole not found"},
	{http.MethodPost, "/api/rounds/" + idPlayRound + "/holes/1/undo", "", "Round hole not found"},
	{http.MethodPatch, "/api/rounds/" + idPlayRound + "/holes/3/shots/" + idPlayShot1, `{"club":"Driver"}`, "Round hole not found"},
	{http.MethodDelete, "/api/rounds/" + idPlayRound + "/holes/3/shots/" + idPlayShot1, "", "Round hole not found"},
	{http.MethodGet, "/api/rounds/" + idPlayRound, "", "Round not found"},
}

// TestCustomRoutesEnforceOwnership is this phase's ACL work: collection API
// rules do not apply to hook code, so every custom route has to scope by owner
// itself. It does it the way Django does — `.get(pk=…, user=user)` raising
// NotFound — so another player's round is 404 with the contract's error body,
// never 403 and never a leak of whether the id exists.
func TestCustomRoutesEnforceOwnership(t *testing.T) {
	for _, r := range customRoutes {
		runPlay(t, playOther, tests.ApiScenario{
			Name:            "another player " + r.method + " " + r.url,
			Method:          r.method,
			URL:             r.url,
			Body:            body(r.body),
			ExpectedStatus:  404,
			ExpectedContent: []string{`{"error":"` + r.notFound + `"}`},
			AfterTestFunc:   assertOwnerRoundUntouched,
		})
	}
}

// TestCustomRoutesAreNotOpenToAdmins is the half of ownership a role could
// plausibly override and does not. `users.role = ADMIN` widens the *read* rules
// on rounds, round_holes and shots (acl_test.go), and a superuser bypasses API
// rules altogether — but neither can score another player's round, because
// Django's services scope on `user=user` with no role branch at all.
//
// Worth stating as a test rather than leaving implicit: "admin" here means
// course administration, not impersonation.
func TestCustomRoutesAreNotOpenToAdmins(t *testing.T) {
	for name, who := range map[string]func(*playFixture) *core.Record{
		"admin":     playAdmin,
		"superuser": playSuperuser,
	} {
		for _, r := range customRoutes {
			runPlay(t, who, tests.ApiScenario{
				Name:            name + " " + r.method + " " + r.url,
				Method:          r.method,
				URL:             r.url,
				Body:            body(r.body),
				ExpectedStatus:  404,
				ExpectedContent: []string{`{"error":"` + r.notFound + `"}`},
				AfterTestFunc:   assertOwnerRoundUntouched,
			})
		}
	}
}

// TestCustomRoutesRejectUnknownIds covers the other 404: the caller is the
// owner, but the round, hole or shot the path names does not exist. The message
// identifies which part of the path failed, as the contract's does.
func TestCustomRoutesRejectUnknownIds(t *testing.T) {
	missingRound := fixedID("norounds")

	for _, tc := range []struct {
		name    string
		method  string
		url     string
		body    string
		message string
	}{
		{"unknown round on complete", http.MethodPost, "/api/rounds/" + missingRound + "/complete", `{}`, "Round not found"},
		{"unknown round on cancel", http.MethodPost, "/api/rounds/" + missingRound + "/cancel", "", "Round not found"},
		{"unknown round on current-hole", http.MethodPatch, "/api/rounds/" + missingRound + "/current-hole", `{"currentHole":1}`, "Round not found"},
		{"unknown round on add shot", http.MethodPost, "/api/rounds/" + missingRound + "/holes/1/shots", `{"club":"Driver"}`, "Round hole not found"},
		{"unplayed hole on undo", http.MethodPost, "/api/rounds/" + idPlayRound + "/holes/99/undo", "", "Round hole not found"},
		{"unknown shot on update", http.MethodPatch, "/api/rounds/" + idPlayRound + "/holes/3/shots/" + fixedID("noshot"), `{"club":"Driver"}`, "Shot not found"},
		{"unknown shot on delete", http.MethodDelete, "/api/rounds/" + idPlayRound + "/holes/3/shots/" + fixedID("noshot"), "", "Shot not found"},
		// A non-numeric hole segment is a 404 rather than a 400: the current
		// contract routes on {int:hole_number}, so the path simply does not
		// exist there either.
		{"non-numeric hole number", http.MethodPost, "/api/rounds/" + idPlayRound + "/holes/abc/shots", `{"club":"Driver"}`, "Round hole not found"},
	} {
		runPlay(t, playOwner, tests.ApiScenario{
			Name:            tc.name,
			Method:          tc.method,
			URL:             tc.url,
			Body:            body(tc.body),
			ExpectedStatus:  404,
			ExpectedContent: []string{`{"error":"` + tc.message + `"}`},
		})
	}
}

// assertOwnerRoundUntouched checks that a denied request changed nothing — a
// 404 that still wrote is worse than a 200.
func assertOwnerRoundUntouched(t testing.TB, app *tests.TestApp, _ *http.Response) {
	t.Helper()

	round, err := app.FindRecordById(collections.NameRounds, idPlayRound)
	if err != nil {
		t.Fatalf("the owner's round is gone: %v", err)
	}
	if got := round.GetString(collections.FieldStatus); got != collections.RoundStatusInProgress {
		t.Errorf("round status = %q, want %q", got, collections.RoundStatusInProgress)
	}
	if got := round.GetInt(collections.FieldCurrentHole); got != 1 {
		t.Errorf("current_hole = %d, want 1", got)
	}

	strokes, numbers, _ := holeState(t, app, idPlayHole3)
	if strokes != 3 || !equalInts(numbers, []int{1, 2, 3}) {
		t.Errorf("hole 3 = %d strokes %v, want 3 [1 2 3]", strokes, numbers)
	}
}

// -----------------------------------------------------------------------------
// pagination, filtering, sorting
// -----------------------------------------------------------------------------

// TestListPagination covers the page envelope. The owner's round has 18
// snapshotted holes plus the completed round's one, so 19 records paginate to
// four pages of five.
//
// The contract's own list endpoint pages differently — `GET /api/rounds/` takes
// `limit`/`page` and returns a bare array — so this is not a parity assertion so
// much as a record of what the generated endpoints offer in its place. Phase 7
// (#128) decides which the frontend uses.
func TestListPagination(t *testing.T) {
	roundHoles := recordsURL(collections.NameRoundHoles)

	runPlay(t, playOwner, tests.ApiScenario{
		Name:           "first page",
		Method:         http.MethodGet,
		URL:            roundHoles + "?perPage=5&sort=hole_number&filter=(round='" + idPlayRound + "')",
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"page":1`, `"perPage":5`, `"totalItems":18`, `"totalPages":4`,
			`"hole_number":1`, `"hole_number":5`,
		},
		NotExpectedContent: []string{`"hole_number":6`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:           "a later page",
		Method:         http.MethodGet,
		URL:            roundHoles + "?perPage=5&page=4&sort=hole_number&filter=(round='" + idPlayRound + "')",
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"page":4`, `"totalItems":18`, `"hole_number":16`, `"hole_number":18`,
		},
		NotExpectedContent: []string{`"hole_number":15`},
	})

	// Past the end is an empty page, not a 404 — the totals still come back so
	// a client can correct itself.
	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "past the last page",
		Method:          http.MethodGet,
		URL:             roundHoles + "?perPage=5&page=99&filter=(round='" + idPlayRound + "')",
		ExpectedStatus:  200,
		ExpectedContent: []string{`"items":[]`, `"totalItems":18`},
	})

	// skipTotal drops the counting query; the totals come back as -1.
	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "skipTotal",
		Method:          http.MethodGet,
		URL:             roundHoles + "?perPage=5&skipTotal=1&filter=(round='" + idPlayRound + "')",
		ExpectedStatus:  200,
		ExpectedContent: []string{`"totalItems":-1`, `"totalPages":-1`},
	})
}

// TestListFiltering covers the filter expressions that stand in for the
// contract's fixed endpoints: `GET /api/rounds/` is
// `filter=(status='completed')` and `GET /api/rounds/in-progress` is
// `filter=(status='in_progress')&perPage=1`, both of which the rounds list rule
// has already narrowed to the caller.
func TestListFiltering(t *testing.T) {
	rounds := recordsURL(collections.NameRounds)

	runPlay(t, playOwner, tests.ApiScenario{
		Name:               "completed rounds",
		Method:             http.MethodGet,
		URL:                rounds + "?filter=(status='completed')",
		ExpectedStatus:     200,
		ExpectedContent:    []string{`"totalItems":1`, `"id":"` + idDoneRound + `"`},
		NotExpectedContent: []string{`"id":"` + idPlayRound + `"`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:               "the in-progress round",
		Method:             http.MethodGet,
		URL:                rounds + "?filter=(status='in_progress')&perPage=1",
		ExpectedStatus:     200,
		ExpectedContent:    []string{`"totalItems":1`, `"id":"` + idPlayRound + `"`},
		NotExpectedContent: []string{`"id":"` + idDoneRound + `"`},
	})

	// A filter cannot widen a list rule: `other` has no rounds, and asking for
	// the owner's by id still returns nothing.
	runPlay(t, playOther, tests.ApiScenario{
		Name:               "a filter cannot reach another player's round",
		Method:             http.MethodGet,
		URL:                rounds + "?filter=(id='" + idPlayRound + "')",
		ExpectedStatus:     200,
		ExpectedContent:    []string{`"totalItems":0`},
		NotExpectedContent: []string{`"id":"` + idPlayRound + `"`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:               "numeric comparison",
		Method:             http.MethodGet,
		URL:                recordsURL(collections.NameCourses) + "?filter=(hole_count=9)",
		ExpectedStatus:     200,
		ExpectedContent:    []string{`"totalItems":1`, `"id":"` + idPlayCourse9 + `"`},
		NotExpectedContent: []string{`"id":"` + idPlayCourse + `"`},
	})

	// A malformed filter is a 400 — and in PocketBase's error shape, not the
	// contract's (API.md gap 5).
	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "a malformed filter is rejected",
		Method:          http.MethodGet,
		URL:             recordsURL(collections.NameCourses) + "?filter=(hole_count~~~9)",
		ExpectedStatus:  400,
		ExpectedContent: []string{`"status":400`},
	})
}

// TestListSorting covers ordering, including the descending form the contract's
// round list uses (`-started_at`) and multi-key sorts.
func TestListSorting(t *testing.T) {
	shots := recordsURL(collections.NameShots)

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "descending shot order",
		Method:          http.MethodGet,
		URL:             shots + "?sort=-shot_number",
		ExpectedStatus:  200,
		ExpectedContent: []string{`"id":"` + idPlayShot3 + `"`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:           "the first shot sorts first ascending",
		Method:         http.MethodGet,
		URL:            shots + "?sort=shot_number&perPage=1",
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"id":"` + idPlayShot1 + `"`, `"shot_number":1`, `"totalItems":3`,
		},
		NotExpectedContent: []string{`"id":"` + idPlayShot3 + `"`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "multi-key sort",
		Method:          http.MethodGet,
		URL:             recordsURL(collections.NameRoundHoles) + "?sort=round,-hole_number&perPage=1&filter=(round='" + idPlayRound + "')",
		ExpectedStatus:  200,
		ExpectedContent: []string{`"hole_number":18`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "sorting on an unknown field is rejected",
		Method:          http.MethodGet,
		URL:             shots + "?sort=nosuchfield",
		ExpectedStatus:  400,
		ExpectedContent: []string{`"status":400`},
	})
}

// -----------------------------------------------------------------------------
// gap 7 — anonymous and unauthorised callers
// -----------------------------------------------------------------------------

// TestAnonymousCallerStatusCodes pins the table in API.md's "Access rules"
// section. Django answers an unauthenticated caller with 401 everywhere; the
// generated endpoints answer 200-with-an-empty-page on a list and 404 on a view,
// because a list rule filters rather than gates.
//
// Nothing leaks either way. The consequence is for the *frontend*: a client that
// keys off 401 to redirect to sign-in sees an empty list instead, so it has to
// key off the token it holds. Recorded as gap 7 for Phase 7 (#128).
func TestAnonymousCallerStatusCodes(t *testing.T) {
	runPlay(t, nil, tests.ApiScenario{
		Name:               "unauthenticated list is 200 and empty, not 401",
		Method:             http.MethodGet,
		URL:                recordsURL(collections.NameRounds),
		ExpectedStatus:     200,
		ExpectedContent:    []string{`"items":[]`, `"totalItems":0`},
		NotExpectedContent: []string{`"id":"` + idPlayRound + `"`},
	})

	runPlay(t, nil, tests.ApiScenario{
		Name:               "unauthenticated view of an existing record is 404, not 401",
		Method:             http.MethodGet,
		URL:                recordURL(collections.NameRounds, idPlayRound),
		ExpectedStatus:     404,
		ExpectedContent:    []string{`"status":404`},
		NotExpectedContent: []string{`"status":401`},
	})
}

// TestUnauthorisedCallerStatusCodes is the second half of gap 7: an
// authenticated caller who is not permitted. Django returns 403; PocketBase
// returns 404 on update and delete and 400 on create.
func TestUnauthorisedCallerStatusCodes(t *testing.T) {
	runPlay(t, playOther, tests.ApiScenario{
		Name:            "creating a course as a non-admin is 400, not 403",
		Method:          http.MethodPost,
		URL:             recordsURL(collections.NameCourses),
		Body:            body(`{"name":"Sneaky Links","hole_count":9}`),
		ExpectedStatus:  400,
		ExpectedContent: []string{`"status":400`},
	})

	runPlay(t, playOther, tests.ApiScenario{
		Name:            "updating another player's round is 404, not 403",
		Method:          http.MethodPatch,
		URL:             recordURL(collections.NameRounds, idPlayRound),
		Body:            body(`{"current_hole":7}`),
		ExpectedStatus:  404,
		ExpectedContent: []string{`"status":404`},
	})

	runPlay(t, playOther, tests.ApiScenario{
		Name:            "deleting another player's round is 404, not 403",
		Method:          http.MethodDelete,
		URL:             recordURL(collections.NameRounds, idPlayRound),
		ExpectedStatus:  404,
		ExpectedContent: []string{`"status":404`},
	})

	// The custom routes are the exception, and the reason they are worth
	// routing the frontend through: apis.RequireAuth() restores the 401.
	runPlay(t, nil, tests.ApiScenario{
		Name:            "the custom routes still answer 401",
		Method:          http.MethodPost,
		URL:             "/api/rounds/" + idPlayRound + "/cancel",
		ExpectedStatus:  401,
		ExpectedContent: []string{`"status":401`},
	})
}

// -----------------------------------------------------------------------------
// gaps 1–5 — response shape
// -----------------------------------------------------------------------------

// TestGeneratedEndpointsReturnSnakeCase pins gap 1. The contract serialises
// camelCase (`holeCount`, `isArchived`, `totalPar`); the generated endpoints
// return the column names, which mirror the Django *model* fields and so are
// snake_case.
//
// Closing this means either renaming the schema fields — giving up the model
// parity Phase 1 was briefed on — or fronting the reads with custom routes that
// transform, which is what Phase 3 did for the routes it built.
func TestGeneratedEndpointsReturnSnakeCase(t *testing.T) {
	runPlay(t, playOwner, tests.ApiScenario{
		Name:           "a course lists its columns, not the contract's names",
		Method:         http.MethodGet,
		URL:            recordURL(collections.NameCourses, idPlayCourse),
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"hole_count":18`, `"is_archived":false`, `"total_par":72`,
		},
		NotExpectedContent: []string{`"holeCount"`, `"isArchived"`, `"totalPar"`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:               "a round lists its columns too",
		Method:             http.MethodGet,
		URL:                recordURL(collections.NameRounds, idPlayRound),
		ExpectedStatus:     200,
		ExpectedContent:    []string{`"play_mode":"full"`, `"current_hole":1`},
		NotExpectedContent: []string{`"playMode"`, `"currentHole"`},
	})
}

// TestCustomRoutesReturnCamelCase is the other side of gap 1, and the reason it
// is "closed on the custom routes": every Phase 3 route reads and writes the
// contract's names.
func TestCustomRoutesReturnCamelCase(t *testing.T) {
	runPlay(t, playOwner, tests.ApiScenario{
		Name:               "the hole payload uses the contract's names",
		Method:             http.MethodPost,
		URL:                "/api/rounds/" + idPlayRound + "/holes/1/shots",
		Body:               body(`{"club":"Driver"}`),
		ExpectedStatus:     200,
		ExpectedContent:    []string{`"holeNumber":1`, `"shotNumber":1`},
		NotExpectedContent: []string{`"hole_number"`, `"shot_number"`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:               "so does current-hole",
		Method:             http.MethodPatch,
		URL:                "/api/rounds/" + idPlayRound + "/current-hole",
		Body:               body(`{"currentHole":4}`),
		ExpectedStatus:     200,
		ExpectedContent:    []string{`"currentHole":4`},
		NotExpectedContent: []string{`"current_hole"`},
	})
}

// TestUnsetNumbersComeBackAsZero pins gap 2, the one with a user-visible bug
// behind it. `RoundOut` types `total_strokes`, `total_par` and
// `relative_to_par` as `int | None` and a round in progress has them null;
// PocketBase has no null for a number field, so they serialise as 0 — and a
// frontend that renders "relativeToPar == 0" as "E" would show an unfinished
// round as even par. `finished_at` is the same shape: `""`, not null.
//
// This one cannot be closed by renaming. It needs the read path to go through a
// route that can emit null, which is the argument for assembling RoundOut in a
// custom route in Phase 7 (#128).
func TestUnsetNumbersComeBackAsZero(t *testing.T) {
	runPlay(t, playOwner, tests.ApiScenario{
		Name:           "an in-progress round has zeroes where the contract has nulls",
		Method:         http.MethodGet,
		URL:            recordURL(collections.NameRounds, idPlayRound),
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"total_strokes":0`, `"total_par":0`, `"relative_to_par":0`, `"finished_at":""`,
		},
		NotExpectedContent: []string{`null`},
	})
}

// TestRecordIdsAreStrings pins gap 3. `IdOut` is typed `id: int` and the
// frontend builds `/rounds/12/play`; a PocketBase id is 15 characters of
// [a-z0-9]. Phase 6 (#127) decides between carrying the Django integer ids
// across as a column and accepting string ids everywhere.
//
// The custom routes already return the string form, so the decision is not
// deferrable past the data migration.
func TestRecordIdsAreStrings(t *testing.T) {
	if len(idPlayRound) != 15 {
		t.Fatalf("fixture id %q is %d characters, want 15", idPlayRound, len(idPlayRound))
	}

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "a generated response quotes its id",
		Method:          http.MethodGet,
		URL:             recordURL(collections.NameRounds, idPlayRound),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"id":"` + idPlayRound + `"`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "so does a custom route",
		Method:          http.MethodPost,
		URL:             "/api/rounds/" + idPlayRound + "/complete",
		Body:            body(`{}`),
		ExpectedStatus:  200,
		ExpectedContent: []string{`{"id":"` + idPlayRound + `"}`},
	})
}

// TestRelationsAreIdsPlusExpand pins gap 4. `RoundDetailOut` nests the course
// inline, with its holes; PocketBase returns the relation as an id and nests the
// record under `expand` only when asked.
//
// The last scenario is the part that decides the Phase 7 (#128) design, and it
// corrects what API.md assumed before this suite measured it: the round → holes
// → shots chain the detail page needs runs *away* from the relation fields, so
// it is a back-relation expand — and 0.39 does serve the whole two-level chain
// in one request. Depth is not the obstacle (the cap is six).
//
// What remains is shape: the data arrives under `expand.round_holes_via_round`
// keyed by back-relation name and in snake_case, where `RoundDetailOut` has
// `holes` and `shots` inline in camelCase. So the detail payload is a
// transformation away from the contract, not a second round trip away.
func TestRelationsAreIdsPlusExpand(t *testing.T) {
	runPlay(t, playOwner, tests.ApiScenario{
		Name:               "a relation is an id by default",
		Method:             http.MethodGet,
		URL:                recordURL(collections.NameRounds, idPlayRound),
		ExpectedStatus:     200,
		ExpectedContent:    []string{`"course":"` + idPlayCourse + `"`},
		NotExpectedContent: []string{`"expand"`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "expand nests the related record",
		Method:          http.MethodGet,
		URL:             recordURL(collections.NameRounds, idPlayRound) + "?expand=course",
		ExpectedStatus:  200,
		ExpectedContent: []string{`"expand":{`, `"name":"Play Links"`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:           "the round → holes → shots chain arrives, under back-relation keys",
		Method:         http.MethodGet,
		URL:            recordURL(collections.NameRounds, idPlayRound) + "?expand=round_holes_via_round.shots_via_round_hole",
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"round_holes_via_round":[`, `"hole_number":3`,
			`"shots_via_round_hole":[`, `"club":"Driver"`, `"shot_number":3`,
		},
		// …but not under the contract's names.
		NotExpectedContent: []string{`"holes":[`, `"shots":[`, `"holeNumber"`},
	})
}

// TestErrorBodyShapes pins gap 5. The contract is `{"error": "<message>"}` for
// every failure; PocketBase's built-in errors are
// `{"data": {…}, "message": …, "status": …}`. Custom routes go through
// internal/apierr and so match; the generated endpoints do not.
func TestErrorBodyShapes(t *testing.T) {
	runPlay(t, playOwner, tests.ApiScenario{
		Name:               "a generated endpoint uses PocketBase's shape",
		Method:             http.MethodGet,
		URL:                recordURL(collections.NameRounds, fixedID("norounds")),
		ExpectedStatus:     404,
		ExpectedContent:    []string{`"status":404`, `"message":`, `"data":`},
		NotExpectedContent: []string{`{"error":`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:               "a custom route uses the contract's shape",
		Method:             http.MethodPost,
		URL:                "/api/rounds/" + fixedID("norounds") + "/cancel",
		ExpectedStatus:     404,
		ExpectedContent:    []string{`{"error":"Round not found"}`},
		NotExpectedContent: []string{`"data":`},
	})
}

// TestConstraintViolationStatusCodes pins gap 6 from the other end. The custom
// routes return the contract's 409 (routes_test.go asserts all three); the same
// conflict reached through the generated create endpoint surfaces as the 400
// PocketBase gives any failed save, because the partial unique index is a
// database constraint rather than a rule.
//
// This is the concrete argument for `POST /api/rounds/` staying a custom route
// rather than the frontend posting records directly.
func TestConstraintViolationStatusCodes(t *testing.T) {
	secondRound := `{"user":"` + idOwner + `","course":"` + idPlayCourse +
		`","status":"in_progress","play_mode":"full","current_hole":1,"started_at":"2026-01-01 10:00:00.000Z"}`

	runPlay(t, playOwner, tests.ApiScenario{
		Name:               "the generated create reports the conflict as 400",
		Method:             http.MethodPost,
		URL:                recordsURL(collections.NameRounds),
		Body:               body(secondRound),
		ExpectedStatus:     400,
		ExpectedContent:    []string{`"status":400`},
		NotExpectedContent: []string{`"status":409`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "the custom route reports it as 409",
		Method:          http.MethodPost,
		URL:             "/api/rounds/",
		Body:            body(`{"courseId":"` + idPlayCourse + `"}`),
		ExpectedStatus:  409,
		ExpectedContent: []string{`{"error":"A round is already in progress"}`},
	})
}

// TestHealthEndpointShape records the health-probe difference. The contract
// answers `{"status": "ok"}`; PocketBase answers its own envelope. Phase 9
// (#130) wires the deployment probe and has to match whichever it finds, so the
// shape is asserted here rather than left to be discovered by a failing
// container health check.
func TestHealthEndpointShape(t *testing.T) {
	runPlay(t, nil, tests.ApiScenario{
		Name:               "health is unauthenticated and uses PocketBase's envelope",
		Method:             http.MethodGet,
		URL:                "/api/health",
		ExpectedStatus:     200,
		ExpectedContent:    []string{`"code":200`, `"message":"API is healthy."`},
		NotExpectedContent: []string{`{"status":"ok"}`},
	})
}

// -----------------------------------------------------------------------------
// success status codes
// -----------------------------------------------------------------------------

// TestSuccessStatusCodes walks the 2xx half of the contract in one place, so a
// route that quietly starts answering 200 where the frontend expects 201 fails
// here rather than in the browser.
func TestSuccessStatusCodes(t *testing.T) {
	// 201 — only round creation returns it; everything else is 200.
	runPlay(t, playOther, tests.ApiScenario{
		Name:            "POST /api/rounds/ is 201",
		Method:          http.MethodPost,
		URL:             "/api/rounds/",
		Body:            body(`{"courseId":"` + idPlayCourse + `"}`),
		ExpectedStatus:  201,
		ExpectedContent: []string{`"id":"`},
	})

	for _, tc := range []struct {
		name   string
		method string
		url    string
		body   string
	}{
		{"complete is 200", http.MethodPost, "/api/rounds/" + idPlayRound + "/complete", `{}`},
		{"cancel is 200", http.MethodPost, "/api/rounds/" + idPlayRound + "/cancel", ""},
		{"current-hole is 200", http.MethodPatch, "/api/rounds/" + idPlayRound + "/current-hole", `{"currentHole":2}`},
		{"add shot is 200", http.MethodPost, "/api/rounds/" + idPlayRound + "/holes/1/shots", `{"club":"Driver"}`},
		{"undo is 200", http.MethodPost, "/api/rounds/" + idPlayRound + "/holes/3/undo", ""},
		{"update shot is 200", http.MethodPatch, "/api/rounds/" + idPlayRound + "/holes/3/shots/" + idPlayShot1, `{"club":"3 Wood"}`},
		{"delete shot is 200", http.MethodDelete, "/api/rounds/" + idPlayRound + "/holes/3/shots/" + idPlayShot1, ""},
	} {
		runPlay(t, playOwner, tests.ApiScenario{
			Name:            tc.name,
			Method:          tc.method,
			URL:             tc.url,
			Body:            body(tc.body),
			ExpectedStatus:  200,
			ExpectedContent: []string{`"id":"`},
		})
	}
}
