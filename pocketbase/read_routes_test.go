package main

import (
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/tests"
)

// Phase 7 (#128) read-route suite.
//
// Phase 3 (#124) built every custom *write* route with the current contract's
// shape; Phase 5 (#126) measured the remaining gaps and carried three of them —
// camelCase (gap 1), null instead of zero (gap 2), and relations nested inline
// instead of id-plus-expand (gap 4) — to "Phase 7 (#128) work" specifically for
// the read path, because none of the three can close on generated CRUD. This
// file is that read path: `GET /api/courses/`, `GET /api/courses/{id}`,
// `GET /api/rounds/`, `GET /api/rounds/in-progress` and `GET /api/rounds/{id}`,
// exercised the same way routes_test.go exercises the writes — over real HTTP,
// against the fixture in routes_test.go.

// -----------------------------------------------------------------------------
// GET /api/courses/, GET /api/courses/{id}
// -----------------------------------------------------------------------------

// TestCourseReadRoutesReturnCamelCase closes gap 1 on the read path: a course
// reads back with the contract's names, holes nested inline, and no auth
// required — courses/api.py's `list_all`/`detail` take no `require_user` call
// either, and the courses collection's read rule is already open to anyone
// (README.md § "Access rules").
func TestCourseReadRoutesReturnCamelCase(t *testing.T) {
	t.Parallel()
	runPlay(t, nil, tests.ApiScenario{
		Name:           "an anonymous caller can list courses",
		Method:         http.MethodGet,
		URL:            "/api/courses/",
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"holeCount":18`, `"timeZone":"America/New_York"`, `"isArchived":false`, `"totalPar":72`,
			`"holeNumber":1`, `"par":4`,
		},
		NotExpectedContent: []string{`"hole_count"`, `"is_archived"`, `"total_par"`},
	})

	runPlay(t, nil, tests.ApiScenario{
		Name:           "the trailing-slash-free path is bound too",
		Method:         http.MethodGet,
		URL:            "/api/courses",
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"holeCount":18`,
		},
	})

	runPlay(t, nil, tests.ApiScenario{
		Name:           "a course detail nests its holes the same way",
		Method:         http.MethodGet,
		URL:            "/api/courses/" + idPlayCourse9,
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"id":"` + idPlayCourse9 + `"`, `"holeCount":9`, `"totalPar":27`,
			`"holes":[`, `"holeNumber":1`, `"par":3`,
		},
		NotExpectedContent: []string{`"hole_count"`, `"expand"`},
	})

	runPlay(t, nil, tests.ApiScenario{
		Name:            "an unknown course id is the contract's 404",
		Method:          http.MethodGet,
		URL:             "/api/courses/" + fixedID("nocourse"),
		ExpectedStatus:  404,
		ExpectedContent: []string{`{"error":"Course not found"}`},
	})
}

// -----------------------------------------------------------------------------
// GET /api/rounds/{id}, GET /api/rounds/in-progress
// -----------------------------------------------------------------------------

// TestRoundDetailRouteReturnsCamelCaseAndNestedCourse closes gaps 1 and 4 on
// the read path: camelCase field names, and the course (with its own holes)
// nested inline rather than an id plus `?expand=`.
func TestRoundDetailRouteReturnsCamelCaseAndNestedCourse(t *testing.T) {
	t.Parallel()
	runPlay(t, playOwner, tests.ApiScenario{
		Name:           "a round detail nests its course and its own holes",
		Method:         http.MethodGet,
		URL:            "/api/rounds/" + idPlayRound,
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"id":"` + idPlayRound + `"`, `"playMode":"full"`, `"currentHole":1`,
			`"course":{"id":"` + idPlayCourse + `"`, `"name":"Play Links"`, `"timeZone":"America/New_York"`,
			`"holes":[`, `"holeNumber":3`, `"shots":[`, `"club":"Driver"`, `"shotNumber":1`,
		},
		NotExpectedContent: []string{
			`"play_mode"`, `"current_hole"`, `"expand"`, `"course":"` + idPlayCourse + `"`,
		},
	})
}

// TestRoundDetailRouteEmitsNullForUnsetTotals closes gap 2 on the read path.
// An in-progress round has not earned finishedAt or the three totals yet;
// PocketBase has no null for a number or an unset date, so a route that read
// them straight off the record would answer 0 and "" the way the generated
// endpoint does (parity_test.go's TestUnsetNumbersComeBackAsZero) — and a
// frontend that renders `relativeToPar === 0` as "E" would show an unfinished
// round as even par.
func TestRoundDetailRouteEmitsNullForUnsetTotals(t *testing.T) {
	t.Parallel()
	runPlay(t, playOwner, tests.ApiScenario{
		Name:           "an in-progress round has nulls, not zeroes",
		Method:         http.MethodGet,
		URL:            "/api/rounds/" + idPlayRound,
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"finishedAt":null`, `"totalStrokes":null`, `"totalPar":null`, `"relativeToPar":null`,
		},
		NotExpectedContent: []string{`"finishedAt":""`},
	})
}

// TestRoundDetailRouteReturnsRealTotalsWhenCompleted is the other half: once a
// round is completed the four fields are real values, not nulls plucked out of
// thin air.
func TestRoundDetailRouteReturnsRealTotalsWhenCompleted(t *testing.T) {
	t.Parallel()
	runPlay(t, playOwner, tests.ApiScenario{
		Name:           "a completed round reports its real totals",
		Method:         http.MethodGet,
		URL:            "/api/rounds/" + idDoneRound,
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"totalStrokes":4`, `"totalPar":4`, `"relativeToPar":0`,
		},
		NotExpectedContent: []string{`"finishedAt":null`, `"totalStrokes":null`},
	})
}

// TestRoundListRouteReturnsOnlyCompletedRounds is Django's list_completed:
// only the caller's completed rounds, never one still in progress.
func TestRoundListRouteReturnsOnlyCompletedRounds(t *testing.T) {
	t.Parallel()
	runPlay(t, playOwner, tests.ApiScenario{
		Name:           "the round list has the completed round, not the in-progress one",
		Method:         http.MethodGet,
		URL:            "/api/rounds/",
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"id":"` + idDoneRound + `"`,
		},
		NotExpectedContent: []string{`"id":"` + idPlayRound + `"`},
	})
}

// TestInProgressRouteReturnsNullWithNoRound is the shape a client with no
// resumable round needs: a request that succeeds and answers `null`, rather
// than the frontend inferring "no round" from a 404.
func TestInProgressRouteReturnsNullWithNoRound(t *testing.T) {
	t.Parallel()
	runPlay(t, playOther, tests.ApiScenario{
		Name:            "a player with no rounds sees null, not 404",
		Method:          http.MethodGet,
		URL:             "/api/rounds/in-progress",
		ExpectedStatus:  200,
		ExpectedContent: []string{`null`},
	})
}

// TestInProgressRouteReturnsTheDetailShape is the resume path: the same
// RoundDetailOut shape as GET /api/rounds/{id}.
func TestInProgressRouteReturnsTheDetailShape(t *testing.T) {
	t.Parallel()
	runPlay(t, playOwner, tests.ApiScenario{
		Name:           "the in-progress round is returned in the detail shape",
		Method:         http.MethodGet,
		URL:            "/api/rounds/in-progress",
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"id":"` + idPlayRound + `"`, `"holes":[`, `"holeNumber":3`, `"shots":[`,
		},
	})
}
