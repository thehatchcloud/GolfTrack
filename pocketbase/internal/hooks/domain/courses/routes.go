package courses

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/apierr"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
)

// The course routes.
//
// The reads need no apis.RequireAuth(): Django's GET /api/courses/ and
// GET /api/courses/{id} take no `require_user` call (courses/api.py), so an
// anonymous caller reads them today, and the generated PocketBase endpoints
// already agree — the `courses` collection's list/view rule is open to everyone
// (README.md § "Access rules"). What these routes add is the response shape:
// camelCase, holes nested inline, and `totalPar` derived, matching CourseOut
// instead of the generated endpoint's snake_case columns.
//
// The writes (Phase 7B) exist for the reason every other custom route does:
// each one spans two collections in a single request. They are admin-only, the
// way courses/api.py's create and update open with require_admin — 401 from
// apis.RequireAuth() for an anonymous caller, 403 from requireAdmin for a
// signed-in non-admin.
func registerRoutes(app core.App) {
	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		se.Router.GET("/api/courses", apierr.Handler(list))
		se.Router.GET("/api/courses/{$}", apierr.Handler(list))
		se.Router.GET("/api/courses/{id}", apierr.Handler(detail))

		// Both spellings of the collection path, as on /api/rounds: the current
		// contract posts to /api/courses/, and {$} keeps that an exact match
		// rather than a subtree.
		se.Router.POST("/api/courses", apierr.Handler(create)).Bind(apis.RequireAuth())
		se.Router.POST("/api/courses/{$}", apierr.Handler(create)).Bind(apis.RequireAuth())
		se.Router.PUT("/api/courses/{id}", apierr.Handler(update)).Bind(apis.RequireAuth())

		return se.Next()
	})
}

// requireAdmin ports core/api_auth.py's require_admin: an authenticated caller
// whose role is not ADMIN is forbidden, with the contract's message.
//
// A PocketBase superuser passes too. Superusers bypass collection API rules
// outright, so the generated endpoints these routes stand in front of — the
// archive/unarchive PATCH among them — already accept one; refusing it here
// would make the custom route the *stricter* path for no reason anyone
// operating the app would expect.
//
// The 401 half is left to apis.RequireAuth(), which answers in PocketBase's
// error shape rather than the contract's. That is the same deviation every
// custom route has carried since Phase 3 (API.md gap 5 covers the body, gap 7
// the code); only the status is contractual.
func requireAdmin(e *core.RequestEvent) error {
	if e.Auth == nil {
		return apierr.Unauthorized("")
	}
	if e.HasSuperuserAuth() {
		return nil
	}
	if e.Auth.GetString(collections.FieldRole) != collections.UserRoleAdmin {
		return apierr.Forbidden("")
	}

	return nil
}

// POST /api/courses/ — Django `create`, response IdOut with 201.
func create(e *core.RequestEvent) error {
	if err := requireAdmin(e); err != nil {
		return err
	}

	payload := In{}
	if err := e.BindBody(&payload); err != nil {
		return apierr.Validation("Invalid request body")
	}

	course, err := Create(e.App, payload)
	if err != nil {
		return err
	}

	return e.JSON(http.StatusCreated, map[string]any{"id": course.Id})
}

// PUT /api/courses/{id} — Django `update`, response IdOut.
func update(e *core.RequestEvent) error {
	if err := requireAdmin(e); err != nil {
		return err
	}

	payload := In{}
	if err := e.BindBody(&payload); err != nil {
		return apierr.Validation("Invalid request body")
	}

	course, err := Update(e.App, e.Request.PathValue("id"), payload)
	if err != nil {
		return err
	}

	return e.JSON(http.StatusOK, map[string]any{"id": course.Id})
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
