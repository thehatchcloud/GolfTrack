package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks"
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
		data[hooks.FieldRole] = role
	}
	record := newRecord(t, app, hooks.NameUsers, id, data)
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
	return saveAs(t, app, newRecord(t, app, hooks.NameCourses, id, map[string]any{
		"name":       name,
		"hole_count": holeCount,
	}))
}

func createCourseHole(t testing.TB, app core.App, id string, course *core.Record, holeNumber, par int) *core.Record {
	t.Helper()
	return saveAs(t, app, newRecord(t, app, hooks.NameCourseHoles, id, map[string]any{
		"course":      course.Id,
		"hole_number": holeNumber,
		"par":         par,
	}))
}

func createRound(t testing.TB, app core.App, id string, user, course *core.Record, status string) *core.Record {
	t.Helper()
	return saveAs(t, app, newRecord(t, app, hooks.NameRounds, id, map[string]any{
		"user":         user.Id,
		"course":       course.Id,
		"status":       status,
		"play_mode":    hooks.PlayModeFull,
		"started_at":   time.Now().UTC(),
		"current_hole": 1,
	}))
}

func createRoundHole(t testing.TB, app core.App, id string, round *core.Record, holeNumber, par int) *core.Record {
	t.Helper()
	return saveAs(t, app, newRecord(t, app, hooks.NameRoundHoles, id, map[string]any{
		"round":       round.Id,
		"hole_number": holeNumber,
		"par":         par,
		"strokes":     0,
	}))
}

func createShot(t testing.TB, app core.App, id string, roundHole *core.Record, shotNumber int, club string) *core.Record {
	t.Helper()
	return saveAs(t, app, newRecord(t, app, hooks.NameShots, id, map[string]any{
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
