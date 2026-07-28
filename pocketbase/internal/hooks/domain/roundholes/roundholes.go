// Package roundholes owns the round_holes record: its initialisation, the
// stroke cache kept on it, and the response shape the shot routes return.
//
// The response shape lives here rather than in the shots package because the
// payload is a hole with its shots nested inside it — Django's RoundHoleOut —
// and shots is the package that needs to build one.
package roundholes

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/records"
)

// Register binds the round hole hooks. Called from internal/hooks.Register.
func Register(app core.App) {
	app.OnRecordCreate(collections.NameRoundHoles).BindFunc(func(e *core.RecordEvent) error {
		// `strokes` is not a required field — a required number field would
		// reject the 0 a hole starts on, because PocketBase treats a zero
		// number as empty. Writing it explicitly keeps a freshly snapshotted
		// hole indistinguishable from one that has been played down to zero
		// shots by an undo.
		if e.Record.Get(collections.FieldStrokes) == nil {
			e.Record.Set(collections.FieldStrokes, 0)
		}
		return e.Next()
	})
}

// RefreshStrokes recomputes the stroke cache for one hole: strokes is defined
// as that hole's shot count, so it is recounted rather than incremented.
//
// Callers run it in the same transaction as the shot insert or delete that made
// it stale, which is what keeps the cache from being observed out of step with
// the shots it counts.
//
// A hole that no longer exists is not an error: cascade deletes remove a round
// before its holes and shots, so a shot delete hook can legitimately find the
// hole it was about to update already gone.
func RefreshStrokes(app core.App, roundHoleID string) error {
	hole, err := app.FindRecordById(collections.NameRoundHoles, roundHoleID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("load round hole %q: %w", roundHoleID, err)
	}

	// A count, not the shot list: this runs inside the transaction of the write
	// that made the cache stale, and the number is all it has ever needed.
	strokes, err := records.CountHoleShots(app, roundHoleID)
	if err != nil {
		return err
	}

	if hole.GetInt(collections.FieldStrokes) == strokes {
		return nil
	}

	hole.Set(collections.FieldStrokes, strokes)
	if err := app.Save(hole); err != nil {
		return fmt.Errorf("update stroke cache of round hole %q: %w", roundHoleID, err)
	}

	return nil
}

// ShotOut is Django's ShotOut, in the camelCase the current API returns.
type ShotOut struct {
	ID         string `json:"id"`
	ShotNumber int    `json:"shotNumber"`
	Club       string `json:"club"`
}

// Out is Django's RoundHoleOut: the hole with its shots nested inside it.
type Out struct {
	ID         string    `json:"id"`
	HoleNumber int       `json:"holeNumber"`
	Par        int       `json:"par"`
	Strokes    int       `json:"strokes"`
	Shots      []ShotOut `json:"shots"`
}

// NewOut builds the response payload for a hole, reloading it so the strokes it
// reports are the committed ones.
//
// The reload is the point: the shot routes call this straight after a
// transaction, and the hole record they hold is the pre-write one.
func NewOut(app core.App, roundHoleID string) (*Out, error) {
	hole, err := app.FindRecordById(collections.NameRoundHoles, roundHoleID)
	if err != nil {
		return nil, fmt.Errorf("load round hole %q: %w", roundHoleID, err)
	}

	shots, err := records.HoleShots(app, roundHoleID)
	if err != nil {
		return nil, err
	}

	return newOut(hole, shots), nil
}

// NewOutList is NewOut for a round's whole hole set, in one query for every
// hole's shots instead of two queries per hole (Phase 8, #129).
//
// It takes the hole records rather than their ids because its caller has just
// listed them, and — unlike the shot routes — is not reading back its own
// uncommitted write, so there is nothing to reload.
func NewOutList(app core.App, holes []*core.Record) ([]Out, error) {
	ids := make([]string, 0, len(holes))
	for _, hole := range holes {
		ids = append(ids, hole.Id)
	}

	shotsByHole, err := records.ShotsByRoundHole(app, ids)
	if err != nil {
		return nil, err
	}

	out := make([]Out, 0, len(holes))
	for _, hole := range holes {
		out = append(out, *newOut(hole, shotsByHole[hole.Id]))
	}

	return out, nil
}

// newOut assembles the payload from records already loaded.
func newOut(hole *core.Record, shots []*core.Record) *Out {
	out := &Out{
		ID:         hole.Id,
		HoleNumber: hole.GetInt(collections.FieldHoleNumber),
		Par:        hole.GetInt(collections.FieldPar),
		Strokes:    hole.GetInt(collections.FieldStrokes),
		Shots:      make([]ShotOut, 0, len(shots)),
	}
	for _, shot := range shots {
		out.Shots = append(out.Shots, ShotOut{
			ID:         shot.Id,
			ShotNumber: shot.GetInt(collections.FieldShotNumber),
			Club:       shot.GetString(collections.FieldClub),
		})
	}

	return out
}
