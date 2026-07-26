// Package records holds the record lookups the domain packages share.
//
// Every function takes a core.App so it can be handed the transaction app
// inside RunInTransaction — reads that decide a write must see the same
// snapshot as the write itself.
//
// The "not found" errors here are the ones Django raises from its service
// layer, message for message, and they collapse ownership into the lookup the
// same way Django's `.get(pk=..., user=user)` does: a round belonging to
// someone else is *not found*, never *forbidden*. Custom routes go through hook
// code, which the collection API rules do not apply to, so this is where round
// ownership is actually enforced for them.
package records

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/apierr"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
)

// FindRound returns the round with the given id owned by userID.
func FindRound(app core.App, roundID, userID string) (*core.Record, error) {
	round, err := app.FindRecordById(collections.NameRounds, roundID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apierr.NotFound("Round not found")
		}
		return nil, fmt.Errorf("find round %q: %w", roundID, err)
	}

	if round.GetString(collections.FieldUser) != userID {
		return nil, apierr.NotFound("Round not found")
	}

	return round, nil
}

// FindRoundHole returns the (round, hole_number) hole of a round owned by
// userID. It is the lookup the nested custom routes need and the generated
// endpoints have no concept of — they address a hole by its own id.
func FindRoundHole(app core.App, roundID string, holeNumber int, userID string) (*core.Record, error) {
	round, err := FindRound(app, roundID, userID)
	if err != nil {
		// A hole is only ever addressed through its round, and Django reports
		// the miss against the hole in both cases.
		return nil, apierr.NotFound("Round hole not found")
	}

	holes, err := app.FindAllRecords(collections.NameRoundHoles, dbx.HashExp{
		collections.FieldRound:      round.Id,
		collections.FieldHoleNumber: holeNumber,
	})
	if err != nil {
		return nil, fmt.Errorf("find round hole %d of round %q: %w", holeNumber, roundID, err)
	}
	if len(holes) == 0 {
		return nil, apierr.NotFound("Round hole not found")
	}

	return holes[0], nil
}

// FindShot returns a shot by id, scoped to the given round hole.
func FindShot(app core.App, roundHoleID, shotID string) (*core.Record, error) {
	shot, err := app.FindRecordById(collections.NameShots, shotID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apierr.NotFound("Shot not found")
		}
		return nil, fmt.Errorf("find shot %q: %w", shotID, err)
	}

	if shot.GetString(collections.FieldRoundHole) != roundHoleID {
		return nil, apierr.NotFound("Shot not found")
	}

	return shot, nil
}

// RoundHoles returns a round's holes, ordered by hole number.
func RoundHoles(app core.App, roundID string) ([]*core.Record, error) {
	var holes []*core.Record

	err := app.RecordQuery(collections.NameRoundHoles).
		AndWhere(dbx.HashExp{collections.FieldRound: roundID}).
		OrderBy(collections.FieldHoleNumber + " ASC").
		All(&holes)
	if err != nil {
		return nil, fmt.Errorf("list holes of round %q: %w", roundID, err)
	}

	return holes, nil
}

// CourseHoles returns a course's holes, ordered by hole number.
func CourseHoles(app core.App, courseID string) ([]*core.Record, error) {
	var holes []*core.Record

	err := app.RecordQuery(collections.NameCourseHoles).
		AndWhere(dbx.HashExp{collections.FieldCourse: courseID}).
		OrderBy(collections.FieldHoleNumber + " ASC").
		All(&holes)
	if err != nil {
		return nil, fmt.Errorf("list holes of course %q: %w", courseID, err)
	}

	return holes, nil
}

// HoleShots returns a round hole's shots, ordered by shot number.
func HoleShots(app core.App, roundHoleID string) ([]*core.Record, error) {
	var shots []*core.Record

	err := app.RecordQuery(collections.NameShots).
		AndWhere(dbx.HashExp{collections.FieldRoundHole: roundHoleID}).
		OrderBy(collections.FieldShotNumber + " ASC").
		All(&shots)
	if err != nil {
		return nil, fmt.Errorf("list shots of round hole %q: %w", roundHoleID, err)
	}

	return shots, nil
}

// InProgressRound returns the user's in-progress round, or nil if they have
// none. The partial unique index guarantees there is at most one.
func InProgressRound(app core.App, userID string) (*core.Record, error) {
	rounds, err := app.FindAllRecords(collections.NameRounds, dbx.HashExp{
		collections.FieldUser:   userID,
		collections.FieldStatus: collections.RoundStatusInProgress,
	})
	if err != nil {
		return nil, fmt.Errorf("find in-progress round of user %q: %w", userID, err)
	}
	if len(rounds) == 0 {
		return nil, nil
	}

	return rounds[0], nil
}

// RequireInProgress rejects a mutation of a round that has already been
// completed, the way every Django service function opens.
func RequireInProgress(round *core.Record) error {
	if round.GetString(collections.FieldStatus) != collections.RoundStatusInProgress {
		return apierr.Conflict("Round is already completed")
	}
	return nil
}
