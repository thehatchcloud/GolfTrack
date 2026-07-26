package shots

import (
	"net/http"
	"strconv"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/apierr"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/roundholes"
)

// The nested shot routes. Their paths carry a hole *number*, which the
// generated endpoints have no concept of — PocketBase addresses a shot by its
// round_hole id, the current contract addresses it by (round, hole_number) —
// and resolving that is half of what these routes are for. The other half is
// the stroke cache and the renumbering, which no single generated request can
// keep atomic.
func registerRoutes(app core.App) {
	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		se.Router.POST("/api/rounds/{id}/holes/{hole}/shots", apierr.Handler(addShot)).Bind(apis.RequireAuth())
		se.Router.POST("/api/rounds/{id}/holes/{hole}/undo", apierr.Handler(undoShot)).Bind(apis.RequireAuth())
		se.Router.PATCH("/api/rounds/{id}/holes/{hole}/shots/{shotId}", apierr.Handler(updateShot)).Bind(apis.RequireAuth())
		se.Router.DELETE("/api/rounds/{id}/holes/{hole}/shots/{shotId}", apierr.Handler(deleteShot)).Bind(apis.RequireAuth())

		return se.Next()
	})
}

// POST /api/rounds/{id}/holes/{n}/shots — Django ShotIn, response RoundHoleOut.
func addShot(e *core.RequestEvent) error {
	holeNumber, err := holeNumberOf(e)
	if err != nil {
		return err
	}

	club, err := bindClub(e)
	if err != nil {
		return err
	}

	hole, err := Add(e.App, e.Auth.Id, e.Request.PathValue("id"), holeNumber, club)
	if err != nil {
		return err
	}

	return writeHole(e, hole.Id)
}

// POST /api/rounds/{id}/holes/{n}/undo — response RoundHoleOut.
func undoShot(e *core.RequestEvent) error {
	holeNumber, err := holeNumberOf(e)
	if err != nil {
		return err
	}

	hole, err := Undo(e.App, e.Auth.Id, e.Request.PathValue("id"), holeNumber)
	if err != nil {
		return err
	}

	return writeHole(e, hole.Id)
}

// PATCH /api/rounds/{id}/holes/{n}/shots/{shotId} — Django ShotIn, response
// ShotOut.
func updateShot(e *core.RequestEvent) error {
	holeNumber, err := holeNumberOf(e)
	if err != nil {
		return err
	}

	club, err := bindClub(e)
	if err != nil {
		return err
	}

	shot, err := UpdateClub(e.App, e.Auth.Id, e.Request.PathValue("id"), holeNumber, e.Request.PathValue("shotId"), club)
	if err != nil {
		return err
	}

	return e.JSON(http.StatusOK, roundholes.ShotOut{
		ID:         shot.Id,
		ShotNumber: shot.GetInt(collections.FieldShotNumber),
		Club:       shot.GetString(collections.FieldClub),
	})
}

// DELETE /api/rounds/{id}/holes/{n}/shots/{shotId} — response RoundHoleOut, so
// the caller sees the renumbered shots and the refreshed stroke count.
func deleteShot(e *core.RequestEvent) error {
	holeNumber, err := holeNumberOf(e)
	if err != nil {
		return err
	}

	hole, err := Delete(e.App, e.Auth.Id, e.Request.PathValue("id"), holeNumber, e.Request.PathValue("shotId"))
	if err != nil {
		return err
	}

	return writeHole(e, hole.Id)
}

func writeHole(e *core.RequestEvent, roundHoleID string) error {
	out, err := roundholes.NewOut(e.App, roundHoleID)
	if err != nil {
		return err
	}

	return e.JSON(http.StatusOK, out)
}

func holeNumberOf(e *core.RequestEvent) (int, error) {
	holeNumber, err := strconv.Atoi(e.Request.PathValue("hole"))
	if err != nil {
		return 0, apierr.NotFound("Round hole not found")
	}

	return holeNumber, nil
}

func bindClub(e *core.RequestEvent) (string, error) {
	payload := struct {
		Club string `json:"club" form:"club"`
	}{}
	if err := e.BindBody(&payload); err != nil {
		return "", apierr.Validation("Invalid request body")
	}

	return payload.Club, nil
}
