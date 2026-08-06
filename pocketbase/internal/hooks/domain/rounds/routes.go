package rounds

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/apierr"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
)

// The round routes PocketBase's generated CRUD cannot cover, because each one
// spans more than one collection in a single request. Paths, request field
// names and status codes are the current contract's (rounds/api.py), so the
// frontend does not have to learn a second one.
//
// apis.RequireAuth() is what makes an anonymous call return 401 here rather than
// the 200-with-empty-list or 404 the rule-filtered generated endpoints give.
// Ownership is then enforced in the handlers, because collection API rules do
// not apply to hook code.
func registerRoutes(app core.App) {
	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		// Both spellings of the collection path are bound: the current contract
		// posts to /api/rounds/, and {$} keeps that an exact match rather than a
		// subtree that would swallow every unmatched /api/rounds/... request.
		se.Router.POST("/api/rounds", apierr.Handler(create)).Bind(apis.RequireAuth())
		se.Router.POST("/api/rounds/{$}", apierr.Handler(create)).Bind(apis.RequireAuth())

		se.Router.POST("/api/rounds/{id}/complete", apierr.Handler(complete)).Bind(apis.RequireAuth())
		se.Router.POST("/api/rounds/{id}/cancel", apierr.Handler(cancel)).Bind(apis.RequireAuth())
		se.Router.DELETE("/api/rounds/{id}", apierr.Handler(deleteRound)).Bind(apis.RequireAuth())
		se.Router.PATCH("/api/rounds/{id}/current-hole", apierr.Handler(patchCurrentHole)).Bind(apis.RequireAuth())

		// The read routes (Phase 7, #128): the generated CRUD reaches every
		// round already, but only in the shape gaps 1/2/4 describe. `GET
		// /api/rounds/in-progress` also has no generated equivalent at all —
		// it is "the one in-progress round", not a record by id.
		//
		// `/api/rounds/in-progress` is a literal path segment, so Go's
		// ServeMux (which this router is built on) matches it in preference to
		// the `{id}` wildcard below regardless of registration order.
		se.Router.GET("/api/rounds", apierr.Handler(list)).Bind(apis.RequireAuth())
		se.Router.GET("/api/rounds/{$}", apierr.Handler(list)).Bind(apis.RequireAuth())
		se.Router.GET("/api/rounds/in-progress", apierr.Handler(inProgress)).Bind(apis.RequireAuth())
		se.Router.GET("/api/rounds/{id}", apierr.Handler(detail)).Bind(apis.RequireAuth())

		// Export/import (GOL-1). Like `in-progress`, `export` and `import` are
		// literal segments the mux matches ahead of `{id}`.
		se.Router.GET("/api/rounds/export", apierr.Handler(exportRounds)).Bind(apis.RequireAuth())
		se.Router.POST("/api/rounds/import", apierr.Handler(importRounds)).Bind(apis.RequireAuth())
		se.Router.GET("/api/rounds/import/template", apierr.Handler(importTemplate)).Bind(apis.RequireAuth())

		return se.Next()
	})
}

// POST /api/rounds/ — Django StartRoundIn, response IdOut with 201.
func create(e *core.RequestEvent) error {
	payload := struct {
		CourseID string `json:"courseId" form:"courseId"`
		PlayMode string `json:"playMode" form:"playMode"`
	}{}
	if err := e.BindBody(&payload); err != nil {
		return apierr.Validation("Invalid request body")
	}

	round, err := Create(e.App, e.Auth.Id, payload.CourseID, payload.PlayMode)
	if err != nil {
		return err
	}

	return e.JSON(http.StatusCreated, map[string]any{"id": round.Id})
}

// POST /api/rounds/{id}/complete — Django CompleteRoundIn, response IdOut.
func complete(e *core.RequestEvent) error {
	payload := struct {
		Note       string  `json:"note" form:"note"`
		StartedAt  *string `json:"startedAt" form:"startedAt"`
		FinishedAt *string `json:"finishedAt" form:"finishedAt"`
	}{}
	if err := e.BindBody(&payload); err != nil {
		return apierr.Validation("Invalid request body")
	}

	round, err := Complete(e.App, e.Auth.Id, e.Request.PathValue("id"), payload.Note, payload.StartedAt, payload.FinishedAt)
	if err != nil {
		return err
	}

	return e.JSON(http.StatusOK, map[string]any{"id": round.Id})
}

// POST /api/rounds/{id}/cancel — response CancelOut.
func cancel(e *core.RequestEvent) error {
	roundID := e.Request.PathValue("id")

	if err := Cancel(e.App, e.Auth.Id, roundID); err != nil {
		return err
	}

	return e.JSON(http.StatusOK, map[string]any{"id": roundID, "cancelled": true})
}

// DELETE /api/rounds/{id} — response DeleteOut.
func deleteRound(e *core.RequestEvent) error {
	roundID := e.Request.PathValue("id")

	if err := Delete(e.App, e.Auth.Id, roundID); err != nil {
		return err
	}

	return e.JSON(http.StatusOK, map[string]any{"id": roundID, "deleted": true})
}

// PATCH /api/rounds/{id}/current-hole — Django SetCurrentHoleIn, response
// CurrentHoleOut.
func patchCurrentHole(e *core.RequestEvent) error {
	payload := struct {
		CurrentHole int `json:"currentHole" form:"currentHole"`
	}{}
	if err := e.BindBody(&payload); err != nil {
		return apierr.Validation("Invalid request body")
	}

	round, err := SetCurrentHole(e.App, e.Auth.Id, e.Request.PathValue("id"), payload.CurrentHole)
	if err != nil {
		return err
	}

	return e.JSON(http.StatusOK, map[string]any{
		"id":          round.Id,
		"currentHole": round.GetInt(collections.FieldCurrentHole),
	})
}

// GET /api/rounds/ — Django `list_completed`, response list[RoundOut]: the
// caller's completed rounds, most recently finished first, capped at 20 —
// `list_completed_rounds`'s defaults, which the Django route never exposes a
// way to override either.
func list(e *core.RequestEvent) error {
	out, err := ListCompleted(e.App, e.Auth.Id)
	if err != nil {
		return err
	}

	return e.JSON(http.StatusOK, out)
}

// GET /api/rounds/in-progress — Django `in_progress`, response
// RoundDetailOut or `null`, so a client resuming a round and a client with none
// in progress both get one, unambiguous request.
func inProgress(e *core.RequestEvent) error {
	out, err := InProgress(e.App, e.Auth.Id)
	if err != nil {
		return err
	}
	if out == nil {
		return e.JSON(http.StatusOK, nil)
	}

	return e.JSON(http.StatusOK, out)
}

// GET /api/rounds/{id} — Django `detail`, response RoundDetailOut.
func detail(e *core.RequestEvent) error {
	out, err := Detail(e.App, e.Auth.Id, e.Request.PathValue("id"))
	if err != nil {
		return err
	}

	return e.JSON(http.StatusOK, out)
}

// GET /api/rounds/export?format=json|csv — the caller's completed rounds as a
// downloadable file (GOL-1), defaulting to JSON.
func exportRounds(e *core.RequestEvent) error {
	format := e.Request.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "csv" {
		return apierr.Validation("Invalid export format: " + format)
	}

	file, err := BuildExport(e.App, e.Auth.Id)
	if err != nil {
		return err
	}

	filename := "golftrack-rounds-" + time.Now().UTC().Format("2006-01-02") + "." + format
	e.Response.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)

	if format == "csv" {
		var buf bytes.Buffer
		if err := WriteCSV(file, &buf); err != nil {
			return err
		}
		return e.Blob(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
	}

	return e.JSON(http.StatusOK, file)
}

// GET /api/rounds/import/template?format=json|csv — a filled-in example of the
// import format for a player building a file by hand, defaulting to JSON.
func importTemplate(e *core.RequestEvent) error {
	format := e.Request.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "csv" {
		return apierr.Validation("Invalid template format: " + format)
	}

	file := TemplateFile()
	e.Response.Header().Set("Content-Disposition", `attachment; filename="golftrack-import-template.`+format+`"`)

	if format == "csv" {
		var buf bytes.Buffer
		if err := WriteCSV(file, &buf); err != nil {
			return err
		}
		return e.Blob(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
	}

	return e.JSON(http.StatusOK, file)
}

// maxImportBytes bounds the import request body. The largest plausible export
// is a few megabytes of CSV; ten is generous without letting an arbitrary
// upload exhaust memory.
const maxImportBytes = 10 << 20

// POST /api/rounds/import — an export file (either format) in the request
// body, response ImportResult.
func importRounds(e *core.RequestEvent) error {
	body, err := io.ReadAll(io.LimitReader(e.Request.Body, maxImportBytes+1))
	if err != nil {
		return apierr.Validation("Could not read import file")
	}
	if len(body) > maxImportBytes {
		return apierr.Validation("Import file is too large")
	}

	file, err := ParseImport(body)
	if err != nil {
		return err
	}

	result, err := Import(e.App, e.Auth.Id, file)
	if err != nil {
		return err
	}

	return e.JSON(http.StatusOK, result)
}
