package main

import (
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
)

// Phase 2 (#123) schema gate: the constraints that only show up at write time.
//
// verify_schema.py checks the same ground over HTTP against a running instance;
// these run in `go test` with no server, so CI catches a regression in the
// committed schema without a deploy.

// saveShouldFail asserts that a save is rejected and that the message names the
// field at fault, so a test cannot pass on an unrelated error.
func saveShouldFail(t *testing.T, app core.App, record *core.Record, wantField string) {
	t.Helper()

	err := app.Save(record)
	if err == nil {
		t.Fatalf("saving %s record succeeded, expected a %q failure", record.Collection().Name, wantField)
	}
	if !strings.Contains(err.Error(), wantField) {
		t.Fatalf("saving %s record failed with %v, expected the error to name %q",
			record.Collection().Name, err, wantField)
	}
}

// world is the minimum set of records the write-time constraints need.
type world struct {
	app    core.App
	user   *core.Record
	course *core.Record
	round  *core.Record
	hole   *core.Record
}

func newWorld(t *testing.T) *world {
	t.Helper()

	app := newTestApp(t)
	user := createUser(t, app, "", "player@golftrack.test", collections.UserRoleUser)
	course := createCourse(t, app, "", "Constraint Country Club", 18)
	createCourseHole(t, app, "", course, 1, 4)
	round := createRound(t, app, "", user, course, collections.RoundStatusInProgress)
	hole := createRoundHole(t, app, "", round, 1, 4)
	createShot(t, app, "", hole, 1, "Driver")

	return &world{app: app, user: user, course: course, round: round, hole: hole}
}

// -----------------------------------------------------------------------------
// users.role
// -----------------------------------------------------------------------------

// TestUserRoleDefaults covers internal/hooks/users.go. `role` is required and
// PocketBase select fields carry no default, so without the hook an OAuth2
// sign-up — which never supplies a role — would fail validation.
func TestUserRoleDefaults(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)

	blank := createUser(t, app, "", "blank@golftrack.test", "")
	if got := blank.GetString(collections.FieldRole); got != collections.UserRoleUser {
		t.Errorf("role defaulted to %q, want %q", got, collections.UserRoleUser)
	}

	admin := createUser(t, app, "", "admin@golftrack.test", collections.UserRoleAdmin)
	if got := admin.GetString(collections.FieldRole); got != collections.UserRoleAdmin {
		t.Errorf("explicit role became %q, want %q", got, collections.UserRoleAdmin)
	}
}

// -----------------------------------------------------------------------------
// unique indexes
// -----------------------------------------------------------------------------

func TestCompositeUniqueIndexes(t *testing.T) {
	t.Parallel()
	t.Run("one row per (course, hole_number)", func(t *testing.T) {
		w := newWorld(t)
		duplicate := newRecord(t, w.app, collections.NameCourseHoles, "", map[string]any{
			"course": w.course.Id, "hole_number": 1, "par": 5,
		})
		saveShouldFail(t, w.app, duplicate, "hole_number")
	})

	t.Run("one row per (round, hole_number)", func(t *testing.T) {
		w := newWorld(t)
		duplicate := newRecord(t, w.app, collections.NameRoundHoles, "", map[string]any{
			"round": w.round.Id, "hole_number": 1, "par": 4, "strokes": 0,
		})
		saveShouldFail(t, w.app, duplicate, "hole_number")
	})

	t.Run("one row per (round_hole, shot_number)", func(t *testing.T) {
		w := newWorld(t)
		duplicate := newRecord(t, w.app, collections.NameShots, "", map[string]any{
			"round_hole": w.hole.Id, "shot_number": 1, "club": "Wedge",
		})
		saveShouldFail(t, w.app, duplicate, "shot_number")
	})
}

// TestOnlyOneInProgressRoundPerUser covers the partial unique index — the
// database-level expression of the rule Django enforces with
// uq_user_one_in_progress_round. Both halves matter: a second in-progress round
// is rejected, a completed one is not.
func TestOnlyOneInProgressRoundPerUser(t *testing.T) {
	t.Parallel()
	w := newWorld(t)

	second := newRecord(t, w.app, collections.NameRounds, "", map[string]any{
		"user": w.user.Id, "course": w.course.Id,
		"status": collections.RoundStatusInProgress, "play_mode": collections.PlayModeFull,
		"started_at": time.Now().UTC(), "current_hole": 1,
	})
	saveShouldFail(t, w.app, second, "user")

	// A completed round for the same user sits outside the index predicate.
	createRound(t, w.app, "", w.user, w.course, collections.RoundStatusCompleted)
}

// -----------------------------------------------------------------------------
// field validation
// -----------------------------------------------------------------------------

func TestFieldValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		collection string
		field      string
		// build returns the record data, given the world's ids.
		build func(*world) map[string]any
	}{
		{"par below 3", collections.NameCourseHoles, "par", func(w *world) map[string]any {
			return map[string]any{"course": w.course.Id, "hole_number": 2, "par": 2}
		}},
		{"par above 6", collections.NameCourseHoles, "par", func(w *world) map[string]any {
			return map[string]any{"course": w.course.Id, "hole_number": 2, "par": 7}
		}},
		{"hole number below 1", collections.NameCourseHoles, "hole_number", func(w *world) map[string]any {
			return map[string]any{"course": w.course.Id, "hole_number": 0, "par": 4}
		}},
		{"hole number above 18", collections.NameCourseHoles, "hole_number", func(w *world) map[string]any {
			return map[string]any{"course": w.course.Id, "hole_number": 19, "par": 4}
		}},
		{"hole count below 9", collections.NameCourses, "hole_count", func(*world) map[string]any {
			return map[string]any{"name": "Too Short", "hole_count": 8}
		}},
		{"hole count above 18", collections.NameCourses, "hole_count", func(*world) map[string]any {
			return map[string]any{"name": "Too Long", "hole_count": 19}
		}},
		{"club longer than 32 characters", collections.NameShots, "club", func(w *world) map[string]any {
			return map[string]any{
				"round_hole": w.hole.Id, "shot_number": 2,
				"club": strings.Repeat("x", 33),
			}
		}},
		{"unknown round status", collections.NameRounds, "status", func(w *world) map[string]any {
			return map[string]any{
				"user": w.user.Id, "course": w.course.Id,
				"status": "abandoned", "play_mode": collections.PlayModeFull,
				"started_at": time.Now().UTC(), "current_hole": 1,
			}
		}},
		{"unknown play mode", collections.NameRounds, "play_mode", func(w *world) map[string]any {
			return map[string]any{
				"user": w.user.Id, "course": w.course.Id,
				"status": collections.RoundStatusCompleted, "play_mode": "middle9",
				"started_at": time.Now().UTC(), "current_hole": 1,
			}
		}},
		{"unknown user role", collections.NameUsers, collections.FieldRole, func(*world) map[string]any {
			return map[string]any{"email": "super@golftrack.test", collections.FieldRole: "SUPERADMIN"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			record := newRecord(t, w.app, tc.collection, "", tc.build(w))
			if tc.collection == collections.NameUsers {
				record.SetPassword(testPassword)
			}
			saveShouldFail(t, w.app, record, tc.field)
		})
	}
}

// TestRoleValuesMatchTheAccessRules pins the `role` value set to the strings the
// ACL rules compare against. Renaming a select value would not break the schema
// import, it would just make `@request.auth.role = "ADMIN"` match nothing —
// silently revoking every admin's read access.
func TestRoleValuesMatchTheAccessRules(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)

	collection, err := app.FindCollectionByNameOrId(collections.NameUsers)
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}

	field, ok := collection.Fields.GetByName(collections.FieldRole).(*core.SelectField)
	if !ok {
		t.Fatalf("users.%s is not a select field", collections.FieldRole)
	}

	want := []string{collections.UserRoleUser, collections.UserRoleAdmin}
	if len(field.Values) != len(want) {
		t.Fatalf("users.%s values = %v, want %v", collections.FieldRole, field.Values, want)
	}
	for _, value := range want {
		if !slicesContains(field.Values, value) {
			t.Errorf("users.%s is missing %q (values: %v)", collections.FieldRole, value, field.Values)
		}
	}
}

func slicesContains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------------
// relation delete behaviour
// -----------------------------------------------------------------------------

func TestDeletingARoundCascadesToHolesAndShots(t *testing.T) {
	t.Parallel()
	w := newWorld(t)

	if err := w.app.Delete(w.round); err != nil {
		t.Fatalf("delete round: %v", err)
	}

	for _, collection := range []string{collections.NameRoundHoles, collections.NameShots} {
		records, err := w.app.FindAllRecords(collection)
		if err != nil {
			t.Fatalf("list %s: %v", collection, err)
		}
		if len(records) != 0 {
			t.Errorf("%s still has %d record(s) after the round was deleted", collection, len(records))
		}
	}
}

// TestCourseWithRoundsCannotBeDeleted is PocketBase's stand-in for Django's
// on_delete=PROTECT: cascadeDelete=false on a *required* relation rejects the
// delete rather than orphaning the round.
func TestCourseWithRoundsCannotBeDeleted(t *testing.T) {
	t.Parallel()
	w := newWorld(t)

	if err := w.app.Delete(w.course); err == nil {
		t.Fatal("deleting a course that still has rounds succeeded, expected it to be rejected")
	}

	if _, err := w.app.FindRecordById(collections.NameCourses, w.course.Id); err != nil {
		t.Fatalf("course should still exist: %v", err)
	}
}
