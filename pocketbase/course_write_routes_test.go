package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/courses"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/records"
)

// Phase 7B backend-dependency suite: POST /api/courses/ and
// PUT /api/courses/{id}.
//
// These are the two writes the migration plan calls out as blocking the
// frontend rewrite. A course form built on generated CRUD would issue 19
// non-atomic requests for an 18-hole course and could leave a course with a
// partial hole set — which rounds.Create then refuses at the next round, far
// from the request that caused it. So the routes are tested for two things the
// generated endpoints cannot give: the whole course committing at once, and the
// contract's admin gating (courses/api.py opens create and update with
// require_admin).
//
// The HTTP half runs against the play fixture the Phase 3 routes use. The
// domain half calls courses.Update directly, because hole *reconciliation* is
// about which records survive an edit — a question that needs the record ids
// from before the request.

// coursePayload builds a CourseIn body: holeCount holes of the same par,
// numbered 1..holeCount.
func coursePayload(name string, holeCount, par int) string {
	return coursePayloadWithTimeZone(name, holeCount, par, "")
}

func coursePayloadWithTimeZone(name string, holeCount, par int, timeZone string) string {
	holes := make([]string, 0, holeCount)
	for number := 1; number <= holeCount; number++ {
		holes = append(holes, fmt.Sprintf(`{"holeNumber":%d,"par":%d}`, number, par))
	}
	return fmt.Sprintf(`{"name":%q,"holeCount":%d,"timeZone":%q,"holes":[%s]}`,
		name, holeCount, timeZone, strings.Join(holes, ","))
}

// courseByName reads back a course the request under test created.
func courseByName(t testing.TB, app core.App, name string) *core.Record {
	t.Helper()

	course, err := app.FindFirstRecordByData(collections.NameCourses, collections.FieldName, name)
	if err != nil {
		t.Fatalf("find course %q: %v", name, err)
	}
	return course
}

// courseState reads a course's hole set back as (hole number, par) pairs, in
// hole order — the shape every assertion below is written against.
func courseState(t testing.TB, app core.App, courseID string) (holeNumbers []int, pars []int) {
	t.Helper()

	holes, err := records.CourseHoles(app, courseID)
	if err != nil {
		t.Fatalf("list holes of course %q: %v", courseID, err)
	}

	for _, hole := range holes {
		holeNumbers = append(holeNumbers, hole.GetInt(collections.FieldHoleNumber))
		pars = append(pars, hole.GetInt(collections.FieldPar))
	}

	return holeNumbers, pars
}

// assertNoNewCourse is the atomicity assertion for a rejected create: the
// fixture's two courses and their 27 holes, and nothing else. A create that
// saved the course before validating its holes would leave a third course here
// with an empty hole set.
func assertNoNewCourse(t testing.TB, app *tests.TestApp, _ *http.Response) {
	t.Helper()

	courseCount, err := app.CountRecords(collections.NameCourses)
	if err != nil {
		t.Fatalf("count courses: %v", err)
	}
	if courseCount != 2 {
		t.Errorf("course count = %d, want the fixture's 2", courseCount)
	}

	holeCount, err := app.CountRecords(collections.NameCourseHoles)
	if err != nil {
		t.Fatalf("count course holes: %v", err)
	}
	if holeCount != 27 {
		t.Errorf("course hole count = %d, want the fixture's 27", holeCount)
	}
}

// assertNineAcresUntouched is the same guard for a rejected update: the course
// keeps its name and its nine par-3 holes.
func assertNineAcresUntouched(t testing.TB, app *tests.TestApp, _ *http.Response) {
	t.Helper()

	course, err := app.FindRecordById(collections.NameCourses, idPlayCourse9)
	if err != nil {
		t.Fatalf("find the fixture's 9-hole course: %v", err)
	}
	if got := course.GetString(collections.FieldName); got != "Nine Acres" {
		t.Errorf("course name = %q, want %q", got, "Nine Acres")
	}
	if got := course.GetInt(collections.FieldHoleCount); got != 9 {
		t.Errorf("hole_count = %d, want 9", got)
	}

	holeNumbers, pars := courseState(t, app, idPlayCourse9)
	if len(holeNumbers) != 9 {
		t.Fatalf("hole count = %d, want 9", len(holeNumbers))
	}
	for i, par := range pars {
		if par != 3 {
			t.Errorf("hole %d par = %d, want the fixture's 3", holeNumbers[i], par)
		}
	}
}

// -----------------------------------------------------------------------------
// POST /api/courses/
// -----------------------------------------------------------------------------

// TestCreateCourseRouteCreatesCourseAndItsHoles is the route's reason to exist:
// one request, one transaction, a course with a complete hole set.
func TestCreateCourseRouteCreatesCourseAndItsHoles(t *testing.T) {
	runPlay(t, playAdmin, tests.ApiScenario{
		Name:            "an admin creates an 18-hole course",
		Method:          http.MethodPost,
		URL:             "/api/courses/",
		Body:            body(coursePayloadWithTimeZone("Sample Ridge", 18, 5, "America/New_York")),
		ExpectedStatus:  201,
		ExpectedContent: []string{`{"id":"`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
			course := courseByName(t, app, "Sample Ridge")
			if got := course.GetInt(collections.FieldHoleCount); got != 18 {
				t.Errorf("hole_count = %d, want 18", got)
			}
			if got := course.GetString(collections.FieldTimeZone); got != "America/New_York" {
				t.Errorf("time_zone = %q, want %q", got, "America/New_York")
			}

			holeNumbers, pars := courseState(t, app, course.Id)
			if len(holeNumbers) != 18 {
				t.Fatalf("hole count = %d, want 18", len(holeNumbers))
			}
			for i, number := range holeNumbers {
				if number != i+1 {
					t.Errorf("hole %d is numbered %d", i+1, number)
				}
				if pars[i] != 5 {
					t.Errorf("hole %d par = %d, want 5", number, pars[i])
				}
			}
		},
	})

	runPlay(t, playAdmin, tests.ApiScenario{
		Name:            "the trailing-slash-free path is bound too",
		Method:          http.MethodPost,
		URL:             "/api/courses",
		Body:            body(coursePayload("Sample Ridge", 9, 4)),
		ExpectedStatus:  201,
		ExpectedContent: []string{`{"id":"`},
	})
}

// TestCreateCourseRouteIsAdminOnly ports courses/api.py's require_admin: a
// signed-in player gets the contract's 403 with its body, an anonymous caller
// the 401 apis.RequireAuth() gives, and a PocketBase superuser is let through —
// it already bypasses the collection rules the generated endpoints enforce.
func TestCreateCourseRouteIsAdminOnly(t *testing.T) {
	runPlay(t, nil, tests.ApiScenario{
		Name:           "an anonymous caller cannot create a course",
		Method:         http.MethodPost,
		URL:            "/api/courses/",
		Body:           body(coursePayload("Sample Ridge", 9, 4)),
		ExpectedStatus: 401,
		// The status is the contract's; the body is PocketBase's, because the
		// 401 comes from apis.RequireAuth() rather than internal/apierr. Every
		// custom route has behaved this way since Phase 3 — API.md gap 5.
		ExpectedContent: []string{`"status":401`},
		AfterTestFunc:   assertNoNewCourse,
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "a signed-in non-admin cannot create a course",
		Method:          http.MethodPost,
		URL:             "/api/courses/",
		Body:            body(coursePayload("Sample Ridge", 9, 4)),
		ExpectedStatus:  403,
		ExpectedContent: []string{`{"error":"Forbidden"}`},
		AfterTestFunc:   assertNoNewCourse,
	})

	runPlay(t, playSuperuser, tests.ApiScenario{
		Name:            "a superuser can create a course",
		Method:          http.MethodPost,
		URL:             "/api/courses/",
		Body:            body(coursePayload("Sample Ridge", 9, 4)),
		ExpectedStatus:  201,
		ExpectedContent: []string{`{"id":"`},
	})
}

// TestCreateCourseRouteRejectsInvalidPayloads walks CourseIn's validators. Each
// message is the one the current API returns, so a form rendering the API's
// error text keeps rendering the same sentence; each response is the contract's
// 400 with the `{"error": …}` body rather than PocketBase's validation shape.
//
// Every case also asserts that nothing was written. That is the ordering
// guarantee the route depends on — validate the whole payload, then open the
// transaction — and the reason a rejected 18-hole course cannot leave a
// nameless, holeless course behind.
func TestCreateCourseRouteRejectsInvalidPayloads(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		message string
	}{
		{
			"a blank name",
			`{"name":"   ","holeCount":9,"holes":[]}`,
			"Course name is required",
		},
		{
			"a name over 100 characters",
			fmt.Sprintf(`{"name":%q,"holeCount":9,"holes":[]}`, strings.Repeat("a", 101)),
			"Course name must be 100 characters or fewer",
		},
		{
			"a hole count that is neither 9 nor 18",
			coursePayload("Twelve Oaks", 12, 4),
			"Hole count must be 9 or 18",
		},
		{
			"fewer holes than the hole count",
			`{"name":"Short","holeCount":18,"holes":` + holesJSON(9, 4) + `}`,
			"Expected 18 holes",
		},
		{
			"a duplicate hole number",
			`{"name":"Doubled","holeCount":9,"holes":[{"holeNumber":1,"par":4},{"holeNumber":1,"par":4},` +
				`{"holeNumber":3,"par":4},{"holeNumber":4,"par":4},{"holeNumber":5,"par":4},{"holeNumber":6,"par":4},` +
				`{"holeNumber":7,"par":4},{"holeNumber":8,"par":4},{"holeNumber":9,"par":4}]}`,
			"Hole numbers must be unique",
		},
		{
			"a gap in the hole numbers",
			`{"name":"Gapped","holeCount":9,"holes":[{"holeNumber":1,"par":4},{"holeNumber":2,"par":4},` +
				`{"holeNumber":3,"par":4},{"holeNumber":4,"par":4},{"holeNumber":5,"par":4},{"holeNumber":6,"par":4},` +
				`{"holeNumber":7,"par":4},{"holeNumber":8,"par":4},{"holeNumber":10,"par":4}]}`,
			"Hole numbers must be sequential starting at 1",
		},
		{
			"a par above 6",
			`{"name":"Long","holeCount":9,"holes":[{"holeNumber":1,"par":7},{"holeNumber":2,"par":4},` +
				`{"holeNumber":3,"par":4},{"holeNumber":4,"par":4},{"holeNumber":5,"par":4},{"holeNumber":6,"par":4},` +
				`{"holeNumber":7,"par":4},{"holeNumber":8,"par":4},{"holeNumber":9,"par":4}]}`,
			"Par must be between 3 and 6",
		},
		{
			"a par below 3",
			`{"name":"Short holes","holeCount":9,"holes":[{"holeNumber":1,"par":2},{"holeNumber":2,"par":4},` +
				`{"holeNumber":3,"par":4},{"holeNumber":4,"par":4},{"holeNumber":5,"par":4},{"holeNumber":6,"par":4},` +
				`{"holeNumber":7,"par":4},{"holeNumber":8,"par":4},{"holeNumber":9,"par":4}]}`,
			"Par must be between 3 and 6",
		},
		{
			"a hole numbered past 18",
			`{"name":"Nineteen","holeCount":9,"holes":[{"holeNumber":19,"par":4},{"holeNumber":2,"par":4},` +
				`{"holeNumber":3,"par":4},{"holeNumber":4,"par":4},{"holeNumber":5,"par":4},{"holeNumber":6,"par":4},` +
				`{"holeNumber":7,"par":4},{"holeNumber":8,"par":4},{"holeNumber":9,"par":4}]}`,
			"Hole number must be between 1 and 18",
		},
		{
			"an invalid time zone",
			`{"name":"Bad TZ","holeCount":9,"timeZone":"Mars/OlympusMons","holes":` + holesJSON(9, 4) + `}`,
			"Time zone must be a valid IANA identifier",
		},
	} {
		runPlay(t, playAdmin, tests.ApiScenario{
			Name:            "create rejects " + tc.name,
			Method:          http.MethodPost,
			URL:             "/api/courses/",
			Body:            body(tc.body),
			ExpectedStatus:  400,
			ExpectedContent: []string{`{"error":"` + tc.message + `"}`},
			AfterTestFunc:   assertNoNewCourse,
		})
	}
}

// holesJSON is coursePayload's hole array on its own, for the cases that pair a
// valid hole set with a mismatched hole count.
func holesJSON(count, par int) string {
	holes := make([]string, 0, count)
	for number := 1; number <= count; number++ {
		holes = append(holes, fmt.Sprintf(`{"holeNumber":%d,"par":%d}`, number, par))
	}
	return "[" + strings.Join(holes, ",") + "]"
}

// -----------------------------------------------------------------------------
// PUT /api/courses/{id}
// -----------------------------------------------------------------------------

// TestUpdateCourseRouteEditsTheCourse is the edit form's request: a rename and
// a new set of pars, answered with the contract's IdOut at 200.
func TestUpdateCourseRouteEditsTheCourse(t *testing.T) {
	runPlay(t, playAdmin, tests.ApiScenario{
		Name:            "an admin renames and re-pars a course",
		Method:          http.MethodPut,
		URL:             "/api/courses/" + idPlayCourse9,
		Body:            body(coursePayloadWithTimeZone("Nine Acres Renamed", 9, 4, "America/Chicago")),
		ExpectedStatus:  200,
		ExpectedContent: []string{`{"id":"` + idPlayCourse9 + `"}`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
			course, err := app.FindRecordById(collections.NameCourses, idPlayCourse9)
			if err != nil {
				t.Fatalf("find the updated course: %v", err)
			}
			if got := course.GetString(collections.FieldName); got != "Nine Acres Renamed" {
				t.Errorf("name = %q, want %q", got, "Nine Acres Renamed")
			}
			if got := course.GetString(collections.FieldTimeZone); got != "America/Chicago" {
				t.Errorf("time_zone = %q, want %q", got, "America/Chicago")
			}

			holeNumbers, pars := courseState(t, app, idPlayCourse9)
			if len(holeNumbers) != 9 {
				t.Fatalf("hole count = %d, want 9", len(holeNumbers))
			}
			for i, par := range pars {
				if par != 4 {
					t.Errorf("hole %d par = %d, want the submitted 4", holeNumbers[i], par)
				}
			}
		},
	})
}

// TestUpdateCourseRouteIsAdminOnly is create's gating, on the other verb.
func TestUpdateCourseRouteIsAdminOnly(t *testing.T) {
	runPlay(t, nil, tests.ApiScenario{
		Name:            "an anonymous caller cannot edit a course",
		Method:          http.MethodPut,
		URL:             "/api/courses/" + idPlayCourse9,
		Body:            body(coursePayload("Hijacked", 9, 4)),
		ExpectedStatus:  401,
		ExpectedContent: []string{`"status":401`},
		AfterTestFunc:   assertNineAcresUntouched,
	})

	runPlay(t, playOwner, tests.ApiScenario{
		Name:            "a signed-in non-admin cannot edit a course",
		Method:          http.MethodPut,
		URL:             "/api/courses/" + idPlayCourse9,
		Body:            body(coursePayload("Hijacked", 9, 4)),
		ExpectedStatus:  403,
		ExpectedContent: []string{`{"error":"Forbidden"}`},
		AfterTestFunc:   assertNineAcresUntouched,
	})
}

// TestUpdateCourseRouteRejectsAnUnknownCourse is the contract's 404, message
// included — the same one GET /api/courses/{id} returns.
func TestUpdateCourseRouteRejectsAnUnknownCourse(t *testing.T) {
	runPlay(t, playAdmin, tests.ApiScenario{
		Name:            "an unknown course id is a 404",
		Method:          http.MethodPut,
		URL:             "/api/courses/" + fixedID("nocourse"),
		Body:            body(coursePayload("Ghost Links", 9, 4)),
		ExpectedStatus:  404,
		ExpectedContent: []string{`{"error":"Course not found"}`},
	})
}

// TestUpdateCourseRouteRejectsInvalidPayloads is the rollback guarantee on the
// edit path: an update that fails validation leaves the course exactly as it
// was, rather than renaming it and then choking on its holes.
func TestUpdateCourseRouteRejectsInvalidPayloads(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		message string
	}{
		{
			"a blank name",
			`{"name":"","holeCount":9,"holes":` + holesJSON(9, 4) + `}`,
			"Course name is required",
		},
		{
			"a hole set that does not match the hole count",
			`{"name":"Nine Acres","holeCount":18,"holes":` + holesJSON(9, 4) + `}`,
			"Expected 18 holes",
		},
	} {
		runPlay(t, playAdmin, tests.ApiScenario{
			Name:            "update rejects " + tc.name,
			Method:          http.MethodPut,
			URL:             "/api/courses/" + idPlayCourse9,
			Body:            body(tc.body),
			ExpectedStatus:  400,
			ExpectedContent: []string{`{"error":"` + tc.message + `"}`},
			AfterTestFunc:   assertNineAcresUntouched,
		})
	}
}

// -----------------------------------------------------------------------------
// hole reconciliation (domain level)
// -----------------------------------------------------------------------------

// holeIDsByNumber snapshots which record holds each hole number, so a later
// read can tell an edited hole from a deleted-and-recreated one.
func holeIDsByNumber(t testing.TB, app core.App, courseID string) map[int]string {
	t.Helper()

	holes, err := records.CourseHoles(app, courseID)
	if err != nil {
		t.Fatalf("list holes of course %q: %v", courseID, err)
	}

	ids := make(map[int]string, len(holes))
	for _, hole := range holes {
		ids[hole.GetInt(collections.FieldHoleNumber)] = hole.Id
	}
	return ids
}

// TestUpdateCourseKeepsTheHoleRecordsItCanReuse ports update_course's
// reconciliation: a hole that is still in the payload is updated in place, not
// replaced. Django keeps the row (it filters and updates by id), and so does
// this — the ids are what a client holds between a read and an edit.
func TestUpdateCourseKeepsTheHoleRecordsItCanReuse(t *testing.T) {
	f := newPlayFixture(t)
	before := holeIDsByNumber(t, f.app, idPlayCourse9)

	if _, err := courses.Update(f.app, idPlayCourse9, courses.In{
		Name:      "Nine Acres",
		HoleCount: 9,
		Holes:     holeSet(9, 5),
	}); err != nil {
		t.Fatalf("update course: %v", err)
	}

	after := holeIDsByNumber(t, f.app, idPlayCourse9)
	if len(after) != 9 {
		t.Fatalf("hole count = %d, want 9", len(after))
	}
	for number, id := range before {
		if after[number] != id {
			t.Errorf("hole %d is now record %q, was %q — the row was replaced, not updated",
				number, after[number], id)
		}
	}

	_, pars := courseState(t, f.app, idPlayCourse9)
	for i, par := range pars {
		if par != 5 {
			t.Errorf("hole %d par = %d, want the submitted 5", i+1, par)
		}
	}
}

// TestUpdateCourseGrowsTheHoleSet is a 9 → 18 resize: nine new holes, the
// original nine untouched. It also pins the ordering inside the transaction —
// the course's new hole_count has to be saved before hole 10 is created, or
// registerHoleNumberRule rejects it against the old count of 9.
func TestUpdateCourseGrowsTheHoleSet(t *testing.T) {
	f := newPlayFixture(t)
	before := holeIDsByNumber(t, f.app, idPlayCourse9)

	if _, err := courses.Update(f.app, idPlayCourse9, courses.In{
		Name:      "Eighteen Acres",
		HoleCount: 18,
		Holes:     holeSet(18, 3),
	}); err != nil {
		t.Fatalf("grow course to 18 holes: %v", err)
	}

	holeNumbers, _ := courseState(t, f.app, idPlayCourse9)
	if len(holeNumbers) != 18 {
		t.Fatalf("hole count = %d, want 18", len(holeNumbers))
	}
	for i, number := range holeNumbers {
		if number != i+1 {
			t.Errorf("hole %d of the set is numbered %d", i+1, number)
		}
	}

	after := holeIDsByNumber(t, f.app, idPlayCourse9)
	for number, id := range before {
		if after[number] != id {
			t.Errorf("hole %d was replaced during the resize", number)
		}
	}
}

// TestUpdateCourseShrinksTheHoleSet is the other direction: the holes the
// payload drops are deleted, which is the behaviour Django documents on
// update_course ("holes absent from the incoming list are deleted").
func TestUpdateCourseShrinksTheHoleSet(t *testing.T) {
	f := newPlayFixture(t)

	if _, err := courses.Update(f.app, idPlayCourse, courses.In{
		Name:      "Play Links Front",
		HoleCount: 9,
		Holes:     holeSet(9, 4),
	}); err != nil {
		t.Fatalf("shrink course to 9 holes: %v", err)
	}

	holeNumbers, _ := courseState(t, f.app, idPlayCourse)
	if len(holeNumbers) != 9 {
		t.Fatalf("hole count = %d, want 9", len(holeNumbers))
	}
	for i, number := range holeNumbers {
		if number != i+1 {
			t.Errorf("hole %d of the set is numbered %d", i+1, number)
		}
	}

	course, err := f.app.FindRecordById(collections.NameCourses, idPlayCourse)
	if err != nil {
		t.Fatalf("find the shrunk course: %v", err)
	}
	if got := course.GetInt(collections.FieldHoleCount); got != 9 {
		t.Errorf("hole_count = %d, want 9", got)
	}
}

// TestUpdateCourseDoesNotRescoreRoundsAlreadyPlayed is course snapshotting seen
// from the edit side: the round holding a snapshot of the course keeps its par
// values when the course is re-pared underneath it.
func TestUpdateCourseDoesNotRescoreRoundsAlreadyPlayed(t *testing.T) {
	f := newPlayFixture(t)

	if _, err := courses.Update(f.app, idPlayCourse, courses.In{
		Name:      "Play Links",
		HoleCount: 18,
		Holes:     holeSet(18, 5),
	}); err != nil {
		t.Fatalf("re-par the course: %v", err)
	}

	holes, err := records.RoundHoles(f.app, idPlayRound)
	if err != nil {
		t.Fatalf("list the round's holes: %v", err)
	}
	for _, hole := range holes {
		if got := hole.GetInt(collections.FieldPar); got != 4 {
			t.Errorf("round hole %d par = %d, want the snapshotted 4",
				hole.GetInt(collections.FieldHoleNumber), got)
		}
	}
}

// holeSet builds the hole half of a courses.In: count holes of the same par,
// numbered 1..count.
func holeSet(count, par int) []courses.HoleIn {
	holes := make([]courses.HoleIn, 0, count)
	for number := 1; number <= count; number++ {
		holes = append(holes, courses.HoleIn{HoleNumber: number, Par: par})
	}
	return holes
}
