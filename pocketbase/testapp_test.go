package main

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/apierr"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/records"
)

// Shared harness for the Phase 2 (#123) suites.
//
// Every test app is built from an *empty* data directory and then seeded by
// syncSchema, so the collections, indexes and API rules under test are the ones
// committed in pb_schema.json — there is no second fixture copy of the schema
// that could drift from it. PocketBase's own tests/data fixtures are
// deliberately not used: they carry demo collections and a `users` collection
// with different fields.

const testPassword = "golftrack-test-pw"

// Record ids are fixed so that a scenario's URL and its expected response body
// can be written as constants, even though each scenario gets a freshly seeded
// app (see run). PocketBase requires exactly 15 characters matching [a-z0-9].
var (
	idOwner = fixedID("useowner")
	idOther = fixedID("useother")
	idAdmin = fixedID("useadmin")

	idCourse     = fixedID("crs")
	idCourseHole = fixedID("crshole")

	idOwnerRound = fixedID("rndowner")
	idOwnerHole  = fixedID("rhlowner")
	idOwnerShot  = fixedID("shtowner")

	idOtherRound = fixedID("rndother")
	idOtherHole  = fixedID("rhlother")
	idOtherShot  = fixedID("shtother")
)

// fixedID pads a readable prefix out to PocketBase's fixed 15-character record
// id length.
func fixedID(prefix string) string {
	return (prefix + strings.Repeat("0", 15))[:15]
}

// newTestApp returns a bootstrapped PocketBase app with the GolfTrack schema
// and hooks applied. Cleanup is registered with t.
func newTestApp(t testing.TB) *tests.TestApp {
	t.Helper()

	dataDir, err := os.MkdirTemp("", "golftrack_pb_*")
	if err != nil {
		t.Fatalf("temp data dir: %v", err)
	}

	app, err := tests.NewTestApp(dataDir)
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(app.Cleanup)
	t.Cleanup(func() { os.RemoveAll(dataDir) })

	if err := syncSchema(app); err != nil {
		t.Fatalf("sync schema: %v", err)
	}
	hooks.Register(app)

	return app
}

// saveAs persists a record as the app itself (bypassing API rules), the way a
// hook or the Admin UI would. Fixtures are built this way so a broken rule
// shows up in the assertion rather than in the setup.
func saveAs(t testing.TB, app core.App, record *core.Record) *core.Record {
	t.Helper()
	if err := app.Save(record); err != nil {
		t.Fatalf("save %s record: %v", record.Collection().Name, err)
	}
	return record
}

// newRecord builds an unsaved record. An empty id lets PocketBase generate one.
func newRecord(t testing.TB, app core.App, collection, id string, data map[string]any) *core.Record {
	t.Helper()

	c, err := app.FindCollectionByNameOrId(collection)
	if err != nil {
		t.Fatalf("find collection %q: %v", collection, err)
	}

	record := core.NewRecord(c)
	if id != "" {
		record.Id = id
	}
	for k, v := range data {
		record.Set(k, v)
	}
	return record
}

// createUser seeds a verified user. Pass an empty role to exercise the
// users.go default.
func createUser(t testing.TB, app core.App, id, email, role string) *core.Record {
	t.Helper()

	data := map[string]any{"email": email, "verified": true}
	if role != "" {
		data[collections.FieldRole] = role
	}
	record := newRecord(t, app, collections.NameUsers, id, data)
	record.SetPassword(testPassword)
	return saveAs(t, app, record)
}

func createSuperuser(t testing.TB, app core.App, email string) *core.Record {
	t.Helper()

	record := newRecord(t, app, core.CollectionNameSuperusers, "", map[string]any{"email": email})
	record.SetPassword(testPassword)
	return saveAs(t, app, record)
}

func createCourse(t testing.TB, app core.App, id, name string, holeCount int) *core.Record {
	t.Helper()
	return saveAs(t, app, newRecord(t, app, collections.NameCourses, id, map[string]any{
		"name":       name,
		"hole_count": holeCount,
	}))
}

func createCourseHole(t testing.TB, app core.App, id string, course *core.Record, holeNumber, par int) *core.Record {
	t.Helper()
	return saveAs(t, app, newRecord(t, app, collections.NameCourseHoles, id, map[string]any{
		"course":      course.Id,
		"hole_number": holeNumber,
		"par":         par,
	}))
}

func createRound(t testing.TB, app core.App, id string, user, course *core.Record, status string) *core.Record {
	t.Helper()
	return saveAs(t, app, newRecord(t, app, collections.NameRounds, id, map[string]any{
		"user":         user.Id,
		"course":       course.Id,
		"status":       status,
		"play_mode":    collections.PlayModeFull,
		"started_at":   time.Now().UTC(),
		"current_hole": 1,
	}))
}

func createRoundHole(t testing.TB, app core.App, id string, round *core.Record, holeNumber, par int) *core.Record {
	t.Helper()
	return saveAs(t, app, newRecord(t, app, collections.NameRoundHoles, id, map[string]any{
		"round":       round.Id,
		"hole_number": holeNumber,
		"par":         par,
		"strokes":     0,
	}))
}

func createShot(t testing.TB, app core.App, id string, roundHole *core.Record, shotNumber int, club string) *core.Record {
	t.Helper()
	return saveAs(t, app, newRecord(t, app, collections.NameShots, id, map[string]any{
		"round_hole":  roundHole.Id,
		"shot_number": shotNumber,
		"club":        club,
	}))
}

// authHeader returns an Authorization header map for the given auth record, or
// nil for a nil record (an unauthenticated request).
func authHeader(t testing.TB, record *core.Record) map[string]string {
	t.Helper()

	if record == nil {
		return nil
	}

	token, err := record.NewAuthToken()
	if err != nil {
		t.Fatalf("auth token for %s: %v", record.Id, err)
	}
	return map[string]string{"Authorization": token}
}

// itoa keeps the expected-content strings in the scenarios free of strconv
// noise.
func itoa(n int) string { return strconv.Itoa(n) }

// -----------------------------------------------------------------------------
// Phase 3 (#124) additions
// -----------------------------------------------------------------------------

// seedCourse creates a course with a complete hole set: holeCount holes of the
// given par, numbered 1..holeCount.
//
// Phase 2's fixture course carries a single hole, which is all an access rule
// needs. Phase 3 rules read the set as a whole — a round snapshots it, and
// create rejects a course that is missing holes — so its fixtures need a whole
// course.
func seedCourse(t testing.TB, app core.App, id, name string, holeCount, par int) *core.Record {
	t.Helper()

	course := createCourse(t, app, id, name, holeCount)
	for number := 1; number <= holeCount; number++ {
		createCourseHole(t, app, "", course, number, par)
	}

	return course
}

// wantAPIError asserts that err is the *apierr.Error a route would turn into a
// {"error": …} response with the given status.
func wantAPIError(t testing.TB, err error, status int, message string) {
	t.Helper()

	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v (%T), want an *apierr.Error with status %d", err, err, status)
	}
	if apiErr.Status != status {
		t.Errorf("error status = %d (%q), want %d", apiErr.Status, apiErr.Message, status)
	}
	if apiErr.Message != message {
		t.Errorf("error message = %q, want %q", apiErr.Message, message)
	}
}

// holeState reads back a round hole's cached stroke count and the shot numbers
// actually stored against it — the two halves of the stroke-cache invariant.
func holeState(t testing.TB, app core.App, roundHoleID string) (strokes int, shotNumbers []int, clubs []string) {
	t.Helper()

	hole, err := app.FindRecordById(collections.NameRoundHoles, roundHoleID)
	if err != nil {
		t.Fatalf("reload round hole %q: %v", roundHoleID, err)
	}

	shots, err := records.HoleShots(app, roundHoleID)
	if err != nil {
		t.Fatalf("list shots of round hole %q: %v", roundHoleID, err)
	}

	for _, shot := range shots {
		shotNumbers = append(shotNumbers, shot.GetInt(collections.FieldShotNumber))
		clubs = append(clubs, shot.GetString(collections.FieldClub))
	}

	return hole.GetInt(collections.FieldStrokes), shotNumbers, clubs
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
