package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/records"
)

// Phase 3 (#124) custom-route suite: the endpoints the generated CRUD cannot
// cover, over real HTTP with real auth tokens.
//
// The paths, request field names and status codes asserted here are the current
// contract's (rounds/api.py). Where a Django code cannot be reproduced it is
// called out in the scenario; the full parity pass is Phase 5 (#126).

// Fixed ids for the play fixture, for the same reason the Phase 2 ones are
// fixed: a scenario's URL is written before its app exists.
var (
	idPlayCourse  = fixedID("plycrs18")
	idPlayCourse9 = fixedID("plycrs09")

	idPlayRound = fixedID("plyrnd")
	idPlayHole1 = fixedID("plyhole1")
	idPlayHole3 = fixedID("plyhole3")
	idPlayShot1 = fixedID("plysht1")
	idPlayShot2 = fixedID("plysht2")
	idPlayShot3 = fixedID("plysht3")

	idDoneRound = fixedID("donrnd")
	idDoneHole  = fixedID("donhole")
)

// playFixture is a round mid-play: an 18-hole course, an in-progress round with
// every hole snapshotted, one empty hole, one hole three shots deep, and a
// completed round to aim the 409s at. `other` owns nothing, so they can start a
// round of their own.
// admin and superuser carry no rounds of their own. Phase 5 (#126) uses them to
// assert that the custom routes scope by ownership alone, the way Django's
// `.get(pk=…, user=user)` does — a role that can *read* another player's round
// through the generated endpoints still cannot score it.
type playFixture struct {
	app       *tests.TestApp
	owner     *core.Record
	other     *core.Record
	admin     *core.Record
	superuser *core.Record
}

func newPlayFixture(t testing.TB) *playFixture {
	t.Helper()

	app := newTestApp(t)

	f := &playFixture{app: app}
	f.owner = createUser(t, app, idOwner, "owner@golftrack.test", collections.UserRoleUser)
	f.other = createUser(t, app, idOther, "other@golftrack.test", collections.UserRoleUser)
	f.admin = createUser(t, app, idAdmin, "admin@golftrack.test", collections.UserRoleAdmin)
	f.superuser = createSuperuser(t, app, "super@golftrack.test")

	course := seedCourse(t, app, idPlayCourse, "Play Links", 18, 4)
	seedCourse(t, app, idPlayCourse9, "Nine Acres", 9, 3)

	// A completed round first: the partial unique index only counts
	// in-progress rounds, so the owner can hold both. Its totals are set at
	// creation, the way rounds.Complete would leave them, rather than by a
	// later update — the "round is already completed" hook
	// (registerCompletedRoundIsImmutable) blocks exactly that second save.
	done := saveAs(t, app, newRecord(t, app, collections.NameRounds, idDoneRound, map[string]any{
		"user":            f.owner.Id,
		"course":          course.Id,
		"status":          collections.RoundStatusCompleted,
		"play_mode":       collections.PlayModeFull,
		"started_at":      time.Now().UTC(),
		"finished_at":     time.Now().UTC(),
		"current_hole":    1,
		"total_strokes":   4,
		"total_par":       4,
		"relative_to_par": 0,
	}))
	createRoundHole(t, app, idDoneHole, done, 1, 4)

	round := createRound(t, app, idPlayRound, f.owner, course, collections.RoundStatusInProgress)
	for number := 1; number <= 18; number++ {
		id := ""
		switch number {
		case 1:
			id = idPlayHole1
		case 3:
			id = idPlayHole3
		}
		createRoundHole(t, app, id, round, number, 4)
	}

	// Hole 3 is three shots in. The stroke cache is left to the hooks rather
	// than seeded, which is also a check that they run on a plain record save.
	hole3, err := app.FindRecordById(collections.NameRoundHoles, idPlayHole3)
	if err != nil {
		t.Fatalf("find hole 3: %v", err)
	}
	createShot(t, app, idPlayShot1, hole3, 1, "Driver")
	createShot(t, app, idPlayShot2, hole3, 2, "7 Iron")
	createShot(t, app, idPlayShot3, hole3, 3, "Wedge")

	return f
}

// runPlay seeds a fresh play fixture per scenario, for the reason documented on
// run in acl_test.go: tests.ApiScenario triggers OnServe per scenario and
// PocketBase's UI extension routes panic when re-registered on a shared app.
func runPlay(t *testing.T, who func(*playFixture) *core.Record, s tests.ApiScenario) {
	t.Helper()

	s.DisableTestAppCleanup = true // newTestApp already registers t.Cleanup
	s.TestAppFactory = func(tb testing.TB) *tests.TestApp {
		f := newPlayFixture(tb)
		if who != nil {
			s.Headers = authHeader(tb, who(f))
		}
		return f.app
	}
	s.Test(t)
}

func playOwner(f *playFixture) *core.Record { return f.owner }
func playOther(f *playFixture) *core.Record { return f.other }

// -----------------------------------------------------------------------------
// authentication
// -----------------------------------------------------------------------------

// TestCustomRoutesRequireAuth is what apis.RequireAuth() buys: the generated
// endpoints answer an anonymous caller with 200-and-an-empty-list or 404,
// because a list rule filters rather than gates. The custom routes restore the
// 401 the current contract returns (API.md, seventh parity gap).
func TestCustomRoutesRequireAuth(t *testing.T) {
	for _, tc := range []struct {
		method string
		url    string
	}{
		{http.MethodPost, "/api/rounds/"},
		{http.MethodPost, "/api/rounds/" + idPlayRound + "/complete"},
		{http.MethodPost, "/api/rounds/" + idPlayRound + "/cancel"},
		{http.MethodPatch, "/api/rounds/" + idPlayRound + "/current-hole"},
		{http.MethodPost, "/api/rounds/" + idPlayRound + "/holes/1/shots"},
		{http.MethodPost, "/api/rounds/" + idPlayRound + "/holes/1/undo"},
		{http.MethodPatch, "/api/rounds/" + idPlayRound + "/holes/3/shots/" + idPlayShot1},
		{http.MethodDelete, "/api/rounds/" + idPlayRound + "/holes/3/shots/" + idPlayShot1},
		{http.MethodGet, "/api/rounds/"},
		{http.MethodGet, "/api/rounds/in-progress"},
		{http.MethodGet, "/api/rounds/" + idPlayRound},
	} {
		runPlay(t, nil, tests.ApiScenario{
			Name:            "anonymous " + tc.method + " " + tc.url,
			Method:          tc.method,
			URL:             tc.url,
			Body:            body(`{"courseId":"` + idPlayCourse + `","club":"Driver","currentHole":2}`),
			ExpectedStatus:  401,
			ExpectedContent: []string{`"status":401`},
		})
	}
}

// -----------------------------------------------------------------------------
// POST /api/rounds/
// -----------------------------------------------------------------------------

func TestCreateRoundRoute(t *testing.T) {
	runPlay(t, playOther, tests.ApiScenario{
		Name:            "start a round",
		Method:          http.MethodPost,
		URL:             "/api/rounds/",
		Body:            body(`{"courseId":"` + idPlayCourse + `","playMode":"back9"}`),
		ExpectedStatus:  201,
		ExpectedContent: []string{`"id":"`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
			round, err := records.InProgressRound(app, idOther)
			if err != nil || round == nil {
				t.Fatalf("in-progress round of the new player: %v", err)
			}
			holes, err := records.RoundHoles(app, round.Id)
			if err != nil {
				t.Fatalf("list holes: %v", err)
			}
			if len(holes) != 9 {
				t.Errorf("back9 round has %d holes, want 9", len(holes))
			}
			if got := round.GetInt(collections.FieldCurrentHole); got != 10 {
				t.Errorf("current_hole = %d, want 10", got)
			}
		},
	})

	// The path without the trailing slash is bound too, so a client that
	// normalises it away still reaches the route rather than a 404.
	runPlay(t, playOther, tests.ApiScenario{
		Name:            "start a round without the trailing slash",
		Method:          http.MethodPost,
		URL:             "/api/rounds",
		Body:            body(`{"courseId":"` + idPlayCourse + `"}`),
		ExpectedStatus:  201,
		ExpectedContent: []string{`"id":"`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "a second in-progress round is a conflict",
		Method:          http.MethodPost,
		URL:             "/api/rounds/",
		Body:            body(`{"courseId":"` + idPlayCourse + `"}`),
		ExpectedStatus:  409,
		ExpectedContent: []string{`{"error":"A round is already in progress"}`},
	})

	runPlay(t, playOther, tests.ApiScenario{
		Name:            "front9 on a nine-hole course",
		Method:          http.MethodPost,
		URL:             "/api/rounds/",
		Body:            body(`{"courseId":"` + idPlayCourse9 + `","playMode":"front9"}`),
		ExpectedStatus:  400,
		ExpectedContent: []string{`{"error":"Front 9 or back 9 is only available on 18-hole courses"}`},
	})

	runPlay(t, playOther, tests.ApiScenario{
		Name:            "unknown play mode",
		Method:          http.MethodPost,
		URL:             "/api/rounds/",
		Body:            body(`{"courseId":"` + idPlayCourse + `","playMode":"middle9"}`),
		ExpectedStatus:  400,
		ExpectedContent: []string{`{"error":"Invalid play mode: middle9"}`},
	})

	runPlay(t, playOther, tests.ApiScenario{
		Name:            "unknown course",
		Method:          http.MethodPost,
		URL:             "/api/rounds/",
		Body:            body(`{"courseId":"` + fixedID("nosuchcourse") + `"}`),
		ExpectedStatus:  404,
		ExpectedContent: []string{`{"error":"Course not found"}`},
	})
}

// -----------------------------------------------------------------------------
// POST /api/rounds/{id}/complete, /cancel and PATCH /current-hole
// -----------------------------------------------------------------------------

func TestCompleteRoundRoute(t *testing.T) {
	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "complete the round",
		Method:          http.MethodPost,
		URL:             "/api/rounds/" + idPlayRound + "/complete",
		Body:            body(`{"note":"Great round"}`),
		ExpectedStatus:  200,
		ExpectedContent: []string{`{"id":"` + idPlayRound + `"}`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
			round, err := app.FindRecordById(collections.NameRounds, idPlayRound)
			if err != nil {
				t.Fatalf("reload round: %v", err)
			}
			// 18 holes at par 4, three shots played on hole 3.
			if got := round.GetInt(collections.FieldTotalPar); got != 72 {
				t.Errorf("total_par = %d, want 72", got)
			}
			if got := round.GetInt(collections.FieldTotalStrokes); got != 3 {
				t.Errorf("total_strokes = %d, want 3", got)
			}
			if got := round.GetInt(collections.FieldRelativeToPar); got != 3-72 {
				t.Errorf("relative_to_par = %d, want %d", got, 3-72)
			}
			if got := round.GetString(collections.FieldNote); got != "Great round" {
				t.Errorf("note = %q, want %q", got, "Great round")
			}
		},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "complete the round with edited times",
		Method:          http.MethodPost,
		URL:             "/api/rounds/" + idPlayRound + "/complete",
		Body:            body(`{"startedAt":"2026-05-04T06:07","finishedAt":"2026-05-04T10:15"}`),
		ExpectedStatus:  200,
		ExpectedContent: []string{`{"id":"` + idPlayRound + `"}`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
			round, err := app.FindRecordById(collections.NameRounds, idPlayRound)
			if err != nil {
				t.Fatalf("reload round: %v", err)
			}
			if got := round.GetDateTime(collections.FieldStartedAt).String(); got != "2026-05-04 06:07:00.000Z" {
				t.Errorf("started_at = %q, want %q", got, "2026-05-04 06:07:00.000Z")
			}
			if got := round.GetDateTime(collections.FieldFinishedAt).String(); got != "2026-05-04 10:15:00.000Z" {
				t.Errorf("finished_at = %q, want %q", got, "2026-05-04 10:15:00.000Z")
			}
		},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "completing a finished round is a conflict",
		Method:          http.MethodPost,
		URL:             "/api/rounds/" + idDoneRound + "/complete",
		ExpectedStatus:  409,
		ExpectedContent: []string{`{"error":"Round is already completed"}`},
	})

	runPlay(t, playOther, tests.ApiScenario{
		Name:            "another player's round is not found",
		Method:          http.MethodPost,
		URL:             "/api/rounds/" + idPlayRound + "/complete",
		ExpectedStatus:  404,
		ExpectedContent: []string{`{"error":"Round not found"}`},
	})
}

func TestCancelRoundRoute(t *testing.T) {
	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "cancel the round",
		Method:          http.MethodPost,
		URL:             "/api/rounds/" + idPlayRound + "/cancel",
		ExpectedStatus:  200,
		ExpectedContent: []string{`"id":"` + idPlayRound + `"`, `"cancelled":true`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
			if _, err := app.FindRecordById(collections.NameRounds, idPlayRound); err == nil {
				t.Error("the cancelled round still exists")
			}
			shots, err := app.FindAllRecords(collections.NameShots)
			if err != nil {
				t.Fatalf("list shots: %v", err)
			}
			if len(shots) != 0 {
				t.Errorf("%d shot(s) survived the cancel", len(shots))
			}
		},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "a finished round cannot be cancelled",
		Method:          http.MethodPost,
		URL:             "/api/rounds/" + idDoneRound + "/cancel",
		ExpectedStatus:  409,
		ExpectedContent: []string{`{"error":"Only in-progress rounds can be cancelled"}`},
	})
}

func TestCurrentHoleRoute(t *testing.T) {
	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "move to another hole",
		Method:          http.MethodPatch,
		URL:             "/api/rounds/" + idPlayRound + "/current-hole",
		Body:            body(`{"currentHole":5}`),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"id":"` + idPlayRound + `"`, `"currentHole":5`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "a hole this round is not playing",
		Method:          http.MethodPatch,
		URL:             "/api/rounds/" + idPlayRound + "/current-hole",
		Body:            body(`{"currentHole":99}`),
		ExpectedStatus:  400,
		ExpectedContent: []string{`{"error":"Invalid hole number"}`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "moving hole on a finished round is a conflict",
		Method:          http.MethodPatch,
		URL:             "/api/rounds/" + idDoneRound + "/current-hole",
		Body:            body(`{"currentHole":1}`),
		ExpectedStatus:  409,
		ExpectedContent: []string{`{"error":"Round is already completed"}`},
	})
}

// -----------------------------------------------------------------------------
// the nested shot routes
// -----------------------------------------------------------------------------

func TestAddShotRoute(t *testing.T) {
	runPlay(t, playOwner, tests.ApiScenario{
		Name:   "add the first shot on an empty hole",
		Method: http.MethodPost,
		URL:    "/api/rounds/" + idPlayRound + "/holes/1/shots",
		Body:   body(`{"club":"Driver"}`),
		// The response is the hole, with its shots — Django's RoundHoleOut.
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"id":"` + idPlayHole1 + `"`, `"holeNumber":1`, `"par":4`, `"strokes":1`,
			`"shotNumber":1`, `"club":"Driver"`,
		},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "add a shot to a hole already in play",
		Method:          http.MethodPost,
		URL:             "/api/rounds/" + idPlayRound + "/holes/3/shots",
		Body:            body(`{"club":"Putter"}`),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"strokes":4`, `"shotNumber":4`, `"club":"Putter"`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "an empty club is rejected",
		Method:          http.MethodPost,
		URL:             "/api/rounds/" + idPlayRound + "/holes/1/shots",
		Body:            body(`{"club":"   "}`),
		ExpectedStatus:  400,
		ExpectedContent: []string{`{"error":"Club is required"}`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "a hole this round is not playing",
		Method:          http.MethodPost,
		URL:             "/api/rounds/" + idPlayRound + "/holes/99/shots",
		Body:            body(`{"club":"Driver"}`),
		ExpectedStatus:  404,
		ExpectedContent: []string{`{"error":"Round hole not found"}`},
	})

	runPlay(t, playOther, tests.ApiScenario{
		Name:            "another player cannot score this round",
		Method:          http.MethodPost,
		URL:             "/api/rounds/" + idPlayRound + "/holes/1/shots",
		Body:            body(`{"club":"Driver"}`),
		ExpectedStatus:  404,
		ExpectedContent: []string{`{"error":"Round hole not found"}`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "a finished round cannot be scored",
		Method:          http.MethodPost,
		URL:             "/api/rounds/" + idDoneRound + "/holes/1/shots",
		Body:            body(`{"club":"Driver"}`),
		ExpectedStatus:  409,
		ExpectedContent: []string{`{"error":"Round is already completed"}`},
	})
}

func TestUndoRoute(t *testing.T) {
	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "undo the last shot",
		Method:          http.MethodPost,
		URL:             "/api/rounds/" + idPlayRound + "/holes/3/undo",
		ExpectedStatus:  200,
		ExpectedContent: []string{`"strokes":2`, `"club":"Driver"`, `"club":"7 Iron"`},
		// Only the highest-numbered shot goes.
		NotExpectedContent: []string{`"club":"Wedge"`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "undo on an untouched hole",
		Method:          http.MethodPost,
		URL:             "/api/rounds/" + idPlayRound + "/holes/1/undo",
		ExpectedStatus:  200,
		ExpectedContent: []string{`"strokes":0`, `"shots":[]`},
	})
}

func TestUpdateShotRoute(t *testing.T) {
	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "re-club a shot",
		Method:          http.MethodPatch,
		URL:             "/api/rounds/" + idPlayRound + "/holes/3/shots/" + idPlayShot2,
		Body:            body(`{"club":"5 Iron"}`),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"id":"` + idPlayShot2 + `"`, `"shotNumber":2`, `"club":"5 Iron"`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "a shot on another hole is not found",
		Method:          http.MethodPatch,
		URL:             "/api/rounds/" + idPlayRound + "/holes/1/shots/" + idPlayShot2,
		Body:            body(`{"club":"5 Iron"}`),
		ExpectedStatus:  404,
		ExpectedContent: []string{`{"error":"Shot not found"}`},
	})
}

// TestDeleteShotRouteRenumbers is the endpoint-level view of the renumbering
// rule: deleting shot 2 of 3 leaves 1 and 2, in their original order.
func TestDeleteShotRouteRenumbers(t *testing.T) {
	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "delete a mid-hole shot",
		Method:          http.MethodDelete,
		URL:             "/api/rounds/" + idPlayRound + "/holes/3/shots/" + idPlayShot2,
		ExpectedStatus:  200,
		ExpectedContent: []string{`"strokes":2`, `"club":"Driver"`, `"club":"Wedge"`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
			strokes, numbers, clubs := holeState(t, app, idPlayHole3)
			if strokes != 2 {
				t.Errorf("strokes = %d, want 2", strokes)
			}
			if !equalInts(numbers, []int{1, 2}) {
				t.Errorf("shot numbers = %v, want [1 2]", numbers)
			}
			if clubs[0] != "Driver" || clubs[1] != "Wedge" {
				t.Errorf("clubs = %v, want [Driver Wedge]", clubs)
			}
		},
	})
}

// -----------------------------------------------------------------------------
// derived course total
// -----------------------------------------------------------------------------

// TestCourseTotalParIsDerivedPerResponse covers the rule that has no column
// behind it. OnRecordEnrich runs on serialization, so the generated endpoints
// carry it too — which is the only reason a Phase 3 rule can be observed on an
// endpoint Phase 3 does not own.
func TestCourseTotalParIsDerivedPerResponse(t *testing.T) {
	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "view an 18-hole course",
		Method:          http.MethodGet,
		URL:             recordURL(collections.NameCourses, idPlayCourse),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"total_par":72`},
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "view a nine-hole course",
		Method:          http.MethodGet,
		URL:             recordURL(collections.NameCourses, idPlayCourse9),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"total_par":27`},
	})
}
