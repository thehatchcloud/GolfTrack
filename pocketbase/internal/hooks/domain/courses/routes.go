package courses

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/apierr"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
)

// The course read routes. Neither one needs apis.RequireAuth(): Django's
// GET /api/courses/ and GET /api/courses/{id} take no `require_user` call
// (courses/api.py), so an anonymous caller reads them today, and the
// generated PocketBase endpoints already agree — the `courses` collection's
// list/view rule is open to everyone (README.md § "Access rules"). What these
// routes add is the response shape: camelCase, holes nested inline, and
// `totalPar` derived, matching CourseOut instead of the generated endpoint's
// snake_case columns.
func registerRoutes(app core.App) {
	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		se.Router.GET("/api/courses", apierr.Handler(list))
		se.Router.GET("/api/courses/{$}", apierr.Handler(list))
		se.Router.GET("/api/courses/{id}", apierr.Handler(detail))

		return se.Next()
	})
}

// GET /api/courses/ — Django `list_all`, response list[CourseOut]. Archived
// courses are excluded, and the list is ordered by name, matching
// `list_courses`'s default (`include_archived=False`).
func list(e *core.RequestEvent) error {
	var courseRecords []*core.Record
	err := e.App.RecordQuery(collections.NameCourses).
		AndWhere(dbx.HashExp{collections.FieldIsArchived: false}).
		OrderBy(collections.FieldName + " ASC").
		All(&courseRecords)
	if err != nil {
		return fmt.Errorf("list courses: %w", err)
	}

	out := make([]*Out, 0, len(courseRecords))
	for _, course := range courseRecords {
		courseOut, err := NewOut(e.App, course)
		if err != nil {
			return err
		}
		out = append(out, courseOut)
	}

	return e.JSON(http.StatusOK, out)
}

// GET /api/courses/{id} — Django `detail`, response CourseOut.
func detail(e *core.RequestEvent) error {
	course, err := e.App.FindRecordById(collections.NameCourses, e.Request.PathValue("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apierr.NotFound("Course not found")
		}
		return fmt.Errorf("find course %q: %w", e.Request.PathValue("id"), err)
	}

	out, err := NewOut(e.App, course)
	if err != nil {
		return err
	}

	return e.JSON(http.StatusOK, out)
}
