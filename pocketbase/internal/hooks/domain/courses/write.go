package courses

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/apierr"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/records"
)

// The course write half, ported from courses/services.py (create_course,
// update_course) with courses/schemas.py's CourseIn for validation.
//
// A course and its holes are one payload in the current contract and two
// collections here, so neither operation can be a generated endpoint: creating
// an 18-hole course through generated CRUD is 19 separate requests, and a
// failure part-way through leaves a course whose hole set is incomplete — which
// rounds.Create then refuses at the next round, long after the request that
// caused it. Both functions therefore run inside RunInTransaction, so a course
// and its holes commit together or not at all.

// nameMaxLength mirrors CourseIn.clean_name and the `name` field bound in
// pb_schema.json.
const nameMaxLength = 100

// HoleIn is Django's HoleIn: one hole of a submitted course, in the camelCase
// the current contract posts.
type HoleIn struct {
	HoleNumber int `json:"holeNumber" form:"holeNumber"`
	Par        int `json:"par" form:"par"`
}

// In is Django's CourseIn — the body of POST /api/courses/ and
// PUT /api/courses/{id}.
type In struct {
	Name      string   `json:"name" form:"name"`
	HoleCount int      `json:"holeCount" form:"holeCount"`
	Holes     []HoleIn `json:"holes" form:"holes"`
}

// validate ports CourseIn's validators, in the order pydantic reports them:
// the field checks first, then validate_holes over the set as a whole. It also
// trims the name in place, the way clean_name does.
//
// Every message here is the contract's, so a form that renders the API's error
// text keeps rendering the same sentence. The par bound and the name length are
// also enforced by pb_schema.json and the hole count by registerHoleCountRule;
// checking them here is what turns a PocketBase-shaped validation failure into
// the `{"error": …}` body the contract promises.
func (in *In) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return apierr.Validation("Course name is required")
	}
	if len(in.Name) > nameMaxLength {
		return apierr.Validation("Course name must be 100 characters or fewer")
	}

	valid := false
	for _, allowed := range collections.HoleCounts {
		if in.HoleCount == allowed {
			valid = true
			break
		}
	}
	if !valid {
		return apierr.Validation("Hole count must be 9 or 18")
	}

	holeNumbers := make([]int, 0, len(in.Holes))
	for _, hole := range in.Holes {
		if hole.HoleNumber < 1 || hole.HoleNumber > 18 {
			return apierr.Validation("Hole number must be between 1 and 18")
		}
		if hole.Par < 3 || hole.Par > 6 {
			return apierr.Validation("Par must be between 3 and 6")
		}
		holeNumbers = append(holeNumbers, hole.HoleNumber)
	}

	// CourseIn caps the list at 18 entries before validate_holes runs; here the
	// same payload is caught by the count clause below ("Expected N holes"),
	// with the same 400.
	return ValidateHoleSet(holeNumbers, in.HoleCount)
}

// Create ports create_course: the course and its whole hole set in one
// transaction.
func Create(app core.App, in In) (*core.Record, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}

	var course *core.Record

	err := app.RunInTransaction(func(txApp core.App) error {
		coursesCollection, err := txApp.FindCollectionByNameOrId(collections.NameCourses)
		if err != nil {
			return fmt.Errorf("find %s collection: %w", collections.NameCourses, err)
		}

		course = core.NewRecord(coursesCollection)
		course.Set(collections.FieldName, in.Name)
		course.Set(collections.FieldHoleCount, in.HoleCount)
		if err := txApp.Save(course); err != nil {
			return fmt.Errorf("create course: %w", err)
		}

		return saveHoles(txApp, course, in.Holes)
	})
	if err != nil {
		return nil, err
	}

	return course, nil
}

// Update ports update_course: rename, resize, and reconcile the hole set —
// holes absent from the payload are deleted, the way the course edit form
// behaves.
func Update(app core.App, courseID string, in In) (*core.Record, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}

	var course *core.Record

	err := app.RunInTransaction(func(txApp core.App) error {
		var err error
		course, err = txApp.FindRecordById(collections.NameCourses, courseID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return apierr.NotFound("Course not found")
			}
			return fmt.Errorf("find course %q: %w", courseID, err)
		}

		// The course is saved before its holes so that a 9 → 18 resize has the
		// larger hole_count committed by the time hole 10 is created —
		// registerHoleNumberRule reads it off the course record. Shrinking is
		// safe in the same order: the holes that fall outside the new count are
		// deleted rather than updated, and the rule does not run on a delete.
		course.Set(collections.FieldName, in.Name)
		course.Set(collections.FieldHoleCount, in.HoleCount)
		if err := txApp.Save(course); err != nil {
			return fmt.Errorf("update course %q: %w", courseID, err)
		}

		existing, err := records.CourseHoles(txApp, course.Id)
		if err != nil {
			return err
		}

		existingByNumber := make(map[int]*core.Record, len(existing))
		for _, hole := range existing {
			existingByNumber[hole.GetInt(collections.FieldHoleNumber)] = hole
		}

		incoming := make(map[int]bool, len(in.Holes))
		var added []HoleIn
		for _, hole := range in.Holes {
			incoming[hole.HoleNumber] = true

			current, ok := existingByNumber[hole.HoleNumber]
			if !ok {
				added = append(added, hole)
				continue
			}
			if current.GetInt(collections.FieldPar) == hole.Par {
				continue
			}
			current.Set(collections.FieldPar, hole.Par)
			if err := txApp.Save(current); err != nil {
				return fmt.Errorf("update hole %d of course %q: %w", hole.HoleNumber, courseID, err)
			}
		}

		if err := saveHoles(txApp, course, added); err != nil {
			return err
		}

		// `existing` is ordered by hole number, so the deletes are issued in a
		// fixed order rather than a map's.
		for _, hole := range existing {
			if incoming[hole.GetInt(collections.FieldHoleNumber)] {
				continue
			}
			if err := txApp.Delete(hole); err != nil {
				return fmt.Errorf("delete hole %d of course %q: %w",
					hole.GetInt(collections.FieldHoleNumber), courseID, err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return course, nil
}

// saveHoles creates holes against a course, using the caller's app so it joins
// the caller's transaction.
func saveHoles(app core.App, course *core.Record, holes []HoleIn) error {
	if len(holes) == 0 {
		return nil
	}

	holesCollection, err := app.FindCollectionByNameOrId(collections.NameCourseHoles)
	if err != nil {
		return fmt.Errorf("find %s collection: %w", collections.NameCourseHoles, err)
	}

	for _, hole := range holes {
		record := core.NewRecord(holesCollection)
		record.Set(collections.FieldCourse, course.Id)
		record.Set(collections.FieldHoleNumber, hole.HoleNumber)
		record.Set(collections.FieldPar, hole.Par)
		if err := app.Save(record); err != nil {
			return fmt.Errorf("create hole %d of course %q: %w", hole.HoleNumber, course.Id, err)
		}
	}

	return nil
}
