package rounds

import (
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/records"
)

// The round reads, as functions rather than route handlers — the same split
// courses/queries.go makes, and for the same reason: internal/web renders the
// rounds pages from these, so a page and the endpoint behind it cannot drift
// into two different answers.

// completedLimit is Django's list_completed_rounds default, which its route
// never exposes a way to override either.
const completedLimit = 20

// ListCompleted returns the player's completed rounds, most recently finished
// first.
func ListCompleted(app core.App, userID string) ([]*Out, error) {
	var roundRecords []*core.Record
	err := app.RecordQuery(collections.NameRounds).
		AndWhere(dbx.HashExp{
			collections.FieldUser:   userID,
			collections.FieldStatus: collections.RoundStatusCompleted,
		}).
		OrderBy(collections.FieldFinishedAt+" DESC", collections.FieldStartedAt+" DESC").
		Limit(completedLimit).
		All(&roundRecords)
	if err != nil {
		return nil, fmt.Errorf("list completed rounds: %w", err)
	}

	return NewOutList(app, roundRecords)
}

// InProgress returns the player's in-progress round in the detail shape, or nil
// when they have none — the distinction the home page and the play page both
// turn on.
func InProgress(app core.App, userID string) (*DetailOut, error) {
	round, err := records.InProgressRound(app, userID)
	if err != nil {
		return nil, err
	}
	if round == nil {
		return nil, nil
	}

	return NewDetailOut(app, round)
}

// Detail returns one of the player's rounds, with the contract's 404 when the
// round does not exist or belongs to somebody else.
func Detail(app core.App, userID, roundID string) (*DetailOut, error) {
	round, err := records.FindRound(app, roundID, userID)
	if err != nil {
		return nil, err
	}

	return NewDetailOut(app, round)
}
