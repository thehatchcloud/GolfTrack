// Package courses ports the course half of the domain rules: courses/schemas.py
// (CourseIn) for validation and courses/models.py for the derived total.
//
// Django validates a course and its holes as one payload, because
// POST /api/courses/ carries both. PocketBase splits them across two
// collections, so the same rules are enforced per record here — and the one
// clause that genuinely needs the whole set, "the holes number exactly
// hole_count", is checked where the set is first read as a whole, at round
// creation (see ValidateHoleSet and internal/hooks/domain/rounds).
package courses

import (
	"fmt"

	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/apierr"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/records"
)

// Register binds the course hooks. Called from internal/hooks.Register.
func Register(app core.App) {
	registerHoleCountRule(app)
	registerHoleNumberRule(app)
	registerDerivedTotalPar(app)
	registerRoutes(app)
}

// registerHoleCountRule enforces Django's `hole_count: Literal[9, 18]`.
//
// pb_schema.json can only bound the field 9–18: PocketBase number fields have
// no value set. Everything between is rejected here instead, on create and on
// update, so no write path can produce a 12-hole course.
func registerHoleCountRule(app core.App) {
	check := func(e *core.RecordEvent) error {
		holeCount := e.Record.GetInt(collections.FieldHoleCount)
		for _, allowed := range collections.HoleCounts {
			if holeCount == allowed {
				return e.Next()
			}
		}
		return validation.Errors{
			collections.FieldHoleCount: fmt.Errorf("must be 9 or 18, got %d", holeCount),
		}
	}

	app.OnRecordCreate(collections.NameCourses).BindFunc(check)
	app.OnRecordUpdate(collections.NameCourses).BindFunc(check)
}

// registerHoleNumberRule keeps a hole inside its own course.
//
// Django gets this from CourseIn.validate_holes ("hole numbers must be
// sequential starting at 1") over the complete payload. Per record, the half
// that is checkable is the range: 1 ≤ hole_number ≤ course.hole_count. Combined
// with the unique (course, hole_number) index, that makes a full hole set
// exactly {1..hole_count} and nothing else — an 18-hole course can never grow a
// hole 19, and a 9-hole course can never grow a hole 10.
func registerHoleNumberRule(app core.App) {
	check := func(e *core.RecordEvent) error {
		holeNumber := e.Record.GetInt(collections.FieldHoleNumber)

		course, err := e.App.FindRecordById(collections.NameCourses, e.Record.GetString(collections.FieldCourse))
		if err != nil {
			// A missing or unreadable relation is the relation field's error to
			// report, not this hook's.
			return e.Next()
		}

		holeCount := course.GetInt(collections.FieldHoleCount)
		if holeNumber < 1 || holeNumber > holeCount {
			return validation.Errors{
				collections.FieldHoleNumber: fmt.Errorf(
					"must be between 1 and %d for a %d-hole course, got %d", holeCount, holeCount, holeNumber),
			}
		}

		return e.Next()
	}

	app.OnRecordCreate(collections.NameCourseHoles).BindFunc(check)
	app.OnRecordUpdate(collections.NameCourseHoles).BindFunc(check)
}

// registerDerivedTotalPar answers "derived total_par on courses": Django
// exposes it as a model property, so it is not a column here either.
//
// OnRecordEnrich runs whenever a record is serialized for a response, which
// makes the computation per response — including on the generated list and view
// endpoints, which no custom route would otherwise reach.
func registerDerivedTotalPar(app core.App) {
	app.OnRecordEnrich(collections.NameCourses).BindFunc(func(e *core.RecordEnrichEvent) error {
		holes, err := records.CourseHoles(e.App, e.Record.Id)
		if err != nil {
			return err
		}

		totalPar := 0
		for _, hole := range holes {
			totalPar += hole.GetInt(collections.FieldPar)
		}

		e.Record.WithCustomData(true)
		e.Record.Set(collections.FieldCourseTotalPar, totalPar)

		return e.Next()
	})
}

// ValidateHoleSet ports CourseIn.validate_holes: a course's holes must number
// exactly hole_count, be unique, and run sequentially from 1.
//
// Uniqueness and the upper bound are already guaranteed by the schema and
// registerHoleNumberRule; what only a whole-set check can catch is a course
// that is *missing* holes, which in PocketBase is reachable because a course
// and its holes are created by separate requests. Callers hold the set.
func ValidateHoleSet(holeNumbers []int, holeCount int) error {
	if len(holeNumbers) != holeCount {
		return apierr.Validation(fmt.Sprintf("Expected %d holes", holeCount))
	}

	seen := make(map[int]bool, len(holeNumbers))
	for _, number := range holeNumbers {
		if seen[number] {
			return apierr.Validation("Hole numbers must be unique")
		}
		seen[number] = true
	}

	for number := 1; number <= holeCount; number++ {
		if !seen[number] {
			return apierr.Validation("Hole numbers must be sequential starting at 1")
		}
	}

	return nil
}
