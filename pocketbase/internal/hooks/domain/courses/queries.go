package courses

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/apierr"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
)

// The course reads, as functions rather than route handlers.
//
// Both the JSON routes in routes.go and the server-rendered pages in
// internal/web need the same three answers, in the same shape. Keeping them
// here means a page and the API endpoint behind it can never disagree about
// what a course list is — the page is not a second implementation of the
// query, it is the same one rendered differently.

// List returns the courses a player can start a round on: not archived, by
// name, as Django's list_courses does with its default include_archived=False.
func List(app core.App) ([]*Out, error) {
	return listWhere(app, dbx.HashExp{collections.FieldIsArchived: false})
}

// ListArchived returns only the archived courses — the admin-only page Django
// builds by filtering list_courses(include_archived=True).
func ListArchived(app core.App) ([]*Out, error) {
	return listWhere(app, dbx.HashExp{collections.FieldIsArchived: true})
}

// Detail returns one course, with the contract's 404 when it does not exist.
func Detail(app core.App, courseID string) (*Out, error) {
	course, err := app.FindRecordById(collections.NameCourses, courseID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apierr.NotFound("Course not found")
		}
		return nil, fmt.Errorf("find course %q: %w", courseID, err)
	}

	return NewOut(app, course)
}

func listWhere(app core.App, where dbx.Expression) ([]*Out, error) {
	var courseRecords []*core.Record
	err := app.RecordQuery(collections.NameCourses).
		AndWhere(where).
		OrderBy(collections.FieldName + " ASC").
		All(&courseRecords)
	if err != nil {
		return nil, fmt.Errorf("list courses: %w", err)
	}

	return NewOutList(app, courseRecords)
}
