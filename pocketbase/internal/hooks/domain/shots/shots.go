// Package shots ports the shot half of rounds/services.py: add_shot,
// undo_last_shot, update_shot and delete_shot.
//
// Two invariants live here and they are the reason every operation is a
// transaction: shot_number is sequential and gap-free within a hole, and
// round_holes.strokes equals that hole's shot count.
//
// Both are maintained in record hooks rather than only in the service
// functions, so the stroke cache and the numbering stay correct whichever path
// wrote the shot — the custom routes below, the generated shots endpoints the
// round owner can also call, or the Admin UI.
package shots

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/apierr"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/roundholes"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/records"
)

// clubMaxLength mirrors the Django ShotIn validator and the `club` field bound
// in pb_schema.json.
const clubMaxLength = 32

// Register binds the shot hooks and custom routes. Called from
// internal/hooks.Register.
func Register(app core.App) {
	registerStrokeCache(app)
	registerRenumbering(app)
	registerRoutes(app)
}

// registerStrokeCache keeps round_holes.strokes equal to the hole's shot count,
// and refuses to score a round that is already finished.
//
// The recount runs after e.Next(), which is the write itself, so it counts the
// shot that was just inserted. When the caller is one of the service functions
// below, both statements are inside the same transaction and the cache can
// never be observed out of step with the shots it counts.
func registerStrokeCache(app core.App) {
	app.OnRecordCreate(collections.NameShots).BindFunc(func(e *core.RecordEvent) error {
		if err := requireInProgressRound(e.App, e.Record); err != nil {
			return err
		}

		if err := e.Next(); err != nil {
			return err
		}

		return roundholes.RefreshStrokes(e.App, e.Record.GetString(collections.FieldRoundHole))
	})

	app.OnRecordUpdate(collections.NameShots).BindFunc(func(e *core.RecordEvent) error {
		if err := requireInProgressRound(e.App, e.Record); err != nil {
			return err
		}
		return e.Next()
	})
}

// registerRenumbering closes the gap a mid-hole delete leaves behind.
//
// Django issues one `UPDATE ... SET shot_number = shot_number - 1 WHERE
// shot_number > n` and so does this: a per-record loop would rewrite the first
// later shot into the slot the delete had not yet vacated in its own index
// entry, and collide with the unique (round_hole, shot_number) index. One
// statement rewrites the rows in ascending order into slots that are free by
// the time each row reaches them.
func registerRenumbering(app core.App) {
	app.OnRecordDelete(collections.NameShots).BindFunc(func(e *core.RecordEvent) error {
		if err := requireInProgressRound(e.App, e.Record); err != nil {
			return err
		}

		roundHoleID := e.Record.GetString(collections.FieldRoundHole)
		deletedNumber := e.Record.GetInt(collections.FieldShotNumber)

		if err := e.Next(); err != nil {
			return err
		}

		if err := renumberAfter(e.App, roundHoleID, deletedNumber); err != nil {
			return err
		}

		return roundholes.RefreshStrokes(e.App, roundHoleID)
	})
}

func renumberAfter(app core.App, roundHoleID string, deletedNumber int) error {
	_, err := app.DB().
		NewQuery("UPDATE {{" + collections.NameShots + "}} SET [[shot_number]] = [[shot_number]] - 1 " +
			"WHERE [[round_hole]] = {:roundHole} AND [[shot_number]] > {:shotNumber}").
		Bind(dbx.Params{"roundHole": roundHoleID, "shotNumber": deletedNumber}).
		Execute()
	if err != nil {
		return fmt.Errorf("renumber shots after %d on round hole %q: %w", deletedNumber, roundHoleID, err)
	}

	return nil
}

// requireInProgressRound rejects a shot write once its round is completed.
//
// A round that cannot be found is not a rejection: cascade deletes remove a
// round before its holes and shots, so cancelling a round legitimately reaches
// this hook with the round row already gone.
func requireInProgressRound(app core.App, shot *core.Record) error {
	hole, err := findOrGone(app, collections.NameRoundHoles, shot.GetString(collections.FieldRoundHole))
	if err != nil || hole == nil {
		return err
	}

	round, err := findOrGone(app, collections.NameRounds, hole.GetString(collections.FieldRound))
	if err != nil || round == nil {
		return err
	}

	if round.GetString(collections.FieldStatus) != collections.RoundStatusInProgress {
		return validation.Errors{
			collections.FieldRoundHole: errors.New("round is already completed"),
		}
	}

	return nil
}

// findOrGone returns (nil, nil) when the record does not exist.
func findOrGone(app core.App, collection, id string) (*core.Record, error) {
	record, err := app.FindRecordById(collection, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load %s %q: %w", collection, id, err)
	}

	return record, nil
}

// Add ports add_shot: append a shot to a hole and leave the stroke cache
// matching. Returns the hole the shot landed on.
func Add(app core.App, userID, roundID string, holeNumber int, club string) (*core.Record, error) {
	club, err := validateClub(club)
	if err != nil {
		return nil, err
	}

	var hole *core.Record

	txErr := app.RunInTransaction(func(txApp core.App) error {
		hole, err = openHole(txApp, roundID, holeNumber, userID)
		if err != nil {
			return err
		}

		existing, err := records.HoleShots(txApp, hole.Id)
		if err != nil {
			return err
		}

		nextNumber := 1
		if len(existing) > 0 {
			nextNumber = existing[len(existing)-1].GetInt(collections.FieldShotNumber) + 1
		}

		shotsCollection, err := txApp.FindCollectionByNameOrId(collections.NameShots)
		if err != nil {
			return fmt.Errorf("find %s collection: %w", collections.NameShots, err)
		}

		shot := core.NewRecord(shotsCollection)
		shot.Set(collections.FieldRoundHole, hole.Id)
		shot.Set(collections.FieldShotNumber, nextNumber)
		shot.Set(collections.FieldClub, club)

		// The unique (round_hole, shot_number) index is the backstop here: if a
		// concurrent add read the same last shot, one of the two saves is
		// rejected rather than both landing on the same number.
		if err := txApp.Save(shot); err != nil {
			return fmt.Errorf("add shot to hole %d of round %q: %w", holeNumber, roundID, err)
		}

		return nil
	})
	if txErr != nil {
		return nil, txErr
	}

	return hole, nil
}

// Undo ports undo_last_shot: remove the highest-numbered shot on the hole, and
// do nothing at all when the hole has none.
func Undo(app core.App, userID, roundID string, holeNumber int) (*core.Record, error) {
	var hole *core.Record

	err := app.RunInTransaction(func(txApp core.App) error {
		var err error
		hole, err = openHole(txApp, roundID, holeNumber, userID)
		if err != nil {
			return err
		}

		shots, err := records.HoleShots(txApp, hole.Id)
		if err != nil {
			return err
		}
		if len(shots) == 0 {
			return nil
		}

		last := shots[len(shots)-1]
		if err := txApp.Delete(last); err != nil {
			return fmt.Errorf("undo shot %d on hole %d of round %q: %w",
				last.GetInt(collections.FieldShotNumber), holeNumber, roundID, err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return hole, nil
}

// UpdateClub ports update_shot. It is the one shot operation that touches
// neither the numbering nor the stroke cache.
func UpdateClub(app core.App, userID, roundID string, holeNumber int, shotID, club string) (*core.Record, error) {
	club, err := validateClub(club)
	if err != nil {
		return nil, err
	}

	var shot *core.Record

	txErr := app.RunInTransaction(func(txApp core.App) error {
		hole, err := openHole(txApp, roundID, holeNumber, userID)
		if err != nil {
			return err
		}

		shot, err = records.FindShot(txApp, hole.Id, shotID)
		if err != nil {
			return err
		}

		shot.Set(collections.FieldClub, club)
		if err := txApp.Save(shot); err != nil {
			return fmt.Errorf("update shot %q: %w", shotID, err)
		}

		return nil
	})
	if txErr != nil {
		return nil, txErr
	}

	return shot, nil
}

// Delete ports delete_shot: remove a shot from anywhere in the hole and close
// the gap it leaves. Returns the hole the shot was on.
func Delete(app core.App, userID, roundID string, holeNumber int, shotID string) (*core.Record, error) {
	var hole *core.Record

	err := app.RunInTransaction(func(txApp core.App) error {
		var err error
		hole, err = openHole(txApp, roundID, holeNumber, userID)
		if err != nil {
			return err
		}

		shot, err := records.FindShot(txApp, hole.Id, shotID)
		if err != nil {
			return err
		}

		if err := txApp.Delete(shot); err != nil {
			return fmt.Errorf("delete shot %q: %w", shotID, err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return hole, nil
}

// openHole resolves (round, hole_number) for an owner and refuses the write if
// the round is finished — the two lines every Django shot service opens with.
func openHole(app core.App, roundID string, holeNumber int, userID string) (*core.Record, error) {
	hole, err := records.FindRoundHole(app, roundID, holeNumber, userID)
	if err != nil {
		return nil, err
	}

	round, err := app.FindRecordById(collections.NameRounds, hole.GetString(collections.FieldRound))
	if err != nil {
		return nil, fmt.Errorf("load round of hole %q: %w", hole.Id, err)
	}
	if err := records.RequireInProgress(round); err != nil {
		return nil, err
	}

	return hole, nil
}

// validateClub ports ShotIn.clean_club.
func validateClub(club string) (string, error) {
	club = strings.TrimSpace(club)
	if club == "" {
		return "", apierr.Validation("Club is required")
	}
	if len(club) > clubMaxLength {
		return "", apierr.Validation("Club name is too long")
	}

	return club, nil
}
