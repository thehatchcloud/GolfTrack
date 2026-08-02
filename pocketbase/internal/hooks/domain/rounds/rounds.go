// Package rounds ports the round lifecycle from rounds/services.py:
// create_round, complete_round, cancel_round and set_current_hole.
//
// Each operation runs inside RunInTransaction and does all of its reads and
// writes through the transaction app, because every one of them spans more than
// one record — a round and its snapshotted holes, a round and the totals summed
// from its holes.
package rounds

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/apierr"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/courses"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/scoring"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/records"
)

// noteMaxLength mirrors the Django CompleteRoundIn validator and the `note`
// field bound in pb_schema.json.
const noteMaxLength = 1000

var roundDateTimeLayouts = []string{
	"2006-01-02T15:04",
	"2006-01-02T15:04:05",
	time.RFC3339,
	"2006-01-02T15:04:05.000Z",
	"2006-01-02 15:04:05.000Z",
	"2006-01-02 15:04:05Z",
}

// Register binds the round hooks and custom routes. Called from
// internal/hooks.Register.
func Register(app core.App) {
	registerCompletedRoundIsImmutable(app)
	registerRoutes(app)
}

// registerCompletedRoundIsImmutable is "a round is mutable only while in
// progress", enforced on every write path rather than only on the custom
// routes.
//
// The custom routes reject the mutation with a 409 before reaching a save; this
// hook is what stops the *generated* PATCH endpoint — which the round owner is
// allowed to call — from re-opening or rescoring a finished round behind the
// stored totals. Completing a round is not caught by it: at that point the
// stored status is still in_progress.
func registerCompletedRoundIsImmutable(app core.App) {
	app.OnRecordUpdate(collections.NameRounds).BindFunc(func(e *core.RecordEvent) error {
		original := e.Record.Original()
		if original != nil &&
			original.GetString(collections.FieldStatus) == collections.RoundStatusCompleted {
			return validation.Errors{
				collections.FieldStatus: errors.New("round is already completed and cannot be modified"),
			}
		}
		return e.Next()
	})
}

// Create ports create_round: validate the play mode against the course, snapshot
// the selected holes' par into round_holes, and refuse a second in-progress
// round.
func Create(app core.App, userID, courseID, playMode string) (*core.Record, error) {
	if playMode == "" {
		playMode = collections.PlayModeFull
	}
	if !isPlayMode(playMode) {
		return nil, apierr.Validation("Invalid play mode: " + playMode)
	}

	course, err := app.FindRecordById(collections.NameCourses, courseID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apierr.NotFound("Course not found")
		}
		return nil, fmt.Errorf("find course %q: %w", courseID, err)
	}

	holeCount := course.GetInt(collections.FieldHoleCount)
	if holeCount != 18 && playMode != collections.PlayModeFull {
		return nil, apierr.Validation("Front 9 or back 9 is only available on 18-hole courses")
	}

	courseHoles, err := records.CourseHoles(app, course.Id)
	if err != nil {
		return nil, err
	}

	// Django validates the hole set when the course is submitted, as one
	// payload. Here a course and its holes are separate records, so an
	// incomplete set is reachable and this is the first point that sees the
	// whole thing — snapshotting 3 holes into an "18-hole" round would
	// otherwise go unnoticed until the scorecard was rendered.
	holeNumbers := make([]int, 0, len(courseHoles))
	for _, hole := range courseHoles {
		holeNumbers = append(holeNumbers, hole.GetInt(collections.FieldHoleNumber))
	}
	if err := courses.ValidateHoleSet(holeNumbers, holeCount); err != nil {
		return nil, err
	}

	selected := selectHoles(courseHoles, playMode)
	startingHole := 1
	if len(selected) > 0 {
		startingHole = selected[0].GetInt(collections.FieldHoleNumber)
	}

	var round *core.Record

	txErr := app.RunInTransaction(func(txApp core.App) error {
		existing, err := records.InProgressRound(txApp, userID)
		if err != nil {
			return err
		}
		if existing != nil {
			return apierr.Conflict("A round is already in progress")
		}

		roundsCollection, err := txApp.FindCollectionByNameOrId(collections.NameRounds)
		if err != nil {
			return fmt.Errorf("find %s collection: %w", collections.NameRounds, err)
		}

		round = core.NewRecord(roundsCollection)
		round.Set(collections.FieldUser, userID)
		round.Set(collections.FieldCourse, course.Id)
		round.Set(collections.FieldStatus, collections.RoundStatusInProgress)
		round.Set(collections.FieldPlayMode, playMode)
		round.Set(collections.FieldStartedAt, types.NowDateTime())
		round.Set(collections.FieldCurrentHole, startingHole)
		if err := txApp.Save(round); err != nil {
			return fmt.Errorf("create round: %w", err)
		}

		holesCollection, err := txApp.FindCollectionByNameOrId(collections.NameRoundHoles)
		if err != nil {
			return fmt.Errorf("find %s collection: %w", collections.NameRoundHoles, err)
		}

		for _, courseHole := range selected {
			hole := core.NewRecord(holesCollection)
			hole.Set(collections.FieldRound, round.Id)
			hole.Set(collections.FieldHoleNumber, courseHole.GetInt(collections.FieldHoleNumber))
			hole.Set(collections.FieldPar, courseHole.GetInt(collections.FieldPar))
			hole.Set(collections.FieldStrokes, 0)
			if err := txApp.Save(hole); err != nil {
				return fmt.Errorf("snapshot hole %d: %w", courseHole.GetInt(collections.FieldHoleNumber), err)
			}
		}

		return nil
	})
	if txErr != nil {
		return nil, asConflictIfRoundStarted(app, userID, txErr)
	}

	return round, nil
}

// asConflictIfRoundStarted turns a lost race into Django's 409.
//
// The check inside the transaction catches the ordinary case. When two creates
// interleave, the partial unique index on (user) where status = 'in_progress' is
// what actually rejects the second one — as a constraint violation, which on its
// own would surface as a 400. Django returns 409 for this, so a save failure
// that left the user with an in-progress round is reported as the conflict it is.
func asConflictIfRoundStarted(app core.App, userID string, err error) error {
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		return err
	}

	if existing, lookupErr := records.InProgressRound(app, userID); lookupErr == nil && existing != nil {
		return apierr.Conflict("A round is already in progress")
	}

	return err
}

// Complete ports complete_round: sum the round's holes into its stored totals,
// then mark it finished.
func Complete(app core.App, userID, roundID, note string, startedAtRaw, finishedAtRaw *string) (*core.Record, error) {
	note = strings.TrimSpace(note)
	if len(note) > noteMaxLength {
		return nil, apierr.Validation("Note is too long")
	}

	var round *core.Record

	err := app.RunInTransaction(func(txApp core.App) error {
		var err error
		round, err = records.FindRound(txApp, roundID, userID)
		if err != nil {
			return err
		}
		if err := records.RequireInProgress(round); err != nil {
			return err
		}

		holes, err := records.RoundHoles(txApp, round.Id)
		if err != nil {
			return err
		}
		totals := scoring.FromRecords(holes)
		startedAt, finishedAt, err := resolveRoundTimes(
			round.GetDateTime(collections.FieldStartedAt),
			types.NowDateTime(),
			startedAtRaw,
			finishedAtRaw,
		)
		if err != nil {
			return err
		}

		round.Set(collections.FieldStatus, collections.RoundStatusCompleted)
		round.Set(collections.FieldStartedAt, startedAt)
		round.Set(collections.FieldFinishedAt, finishedAt)
		round.Set(collections.FieldNote, note)
		round.Set(collections.FieldTotalStrokes, totals.TotalStrokes)
		round.Set(collections.FieldTotalPar, totals.TotalPar)
		round.Set(collections.FieldRelativeToPar, totals.RelativeToPar)

		if err := txApp.Save(round); err != nil {
			return fmt.Errorf("complete round %q: %w", roundID, err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return round, nil
}

// Cancel ports cancel_round: delete an in-progress round, taking its holes and
// shots with it through the cascade.
func Cancel(app core.App, userID, roundID string) error {
	return app.RunInTransaction(func(txApp core.App) error {
		round, err := records.FindRound(txApp, roundID, userID)
		if err != nil {
			return err
		}

		if round.GetString(collections.FieldStatus) != collections.RoundStatusInProgress {
			return apierr.Conflict("Only in-progress rounds can be cancelled")
		}

		if err := txApp.Delete(round); err != nil {
			return fmt.Errorf("cancel round %q: %w", roundID, err)
		}

		return nil
	})
}

// SetCurrentHole ports set_current_hole. The hole has to be one this round is
// actually playing — on a back9 round that is 10–18, not 1–9.
func SetCurrentHole(app core.App, userID, roundID string, currentHole int) (*core.Record, error) {
	var round *core.Record

	err := app.RunInTransaction(func(txApp core.App) error {
		var err error
		round, err = records.FindRound(txApp, roundID, userID)
		if err != nil {
			return err
		}
		if err := records.RequireInProgress(round); err != nil {
			return err
		}

		holes, err := records.RoundHoles(txApp, round.Id)
		if err != nil {
			return err
		}

		valid := false
		for _, hole := range holes {
			if hole.GetInt(collections.FieldHoleNumber) == currentHole {
				valid = true
				break
			}
		}
		if !valid {
			return apierr.Validation("Invalid hole number")
		}

		round.Set(collections.FieldCurrentHole, currentHole)
		if err := txApp.Save(round); err != nil {
			return fmt.Errorf("set current hole of round %q: %w", roundID, err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return round, nil
}

// selectHoles applies the play mode to a course's holes: front9 is 1–9, back9 is
// 10–18, full is everything. The input is already ordered by hole number.
func selectHoles(courseHoles []*core.Record, playMode string) []*core.Record {
	var low, high int

	switch playMode {
	case collections.PlayModeFront9:
		low, high = 1, 9
	case collections.PlayModeBack9:
		low, high = 10, 18
	default:
		return courseHoles
	}

	selected := make([]*core.Record, 0, len(courseHoles))
	for _, hole := range courseHoles {
		if number := hole.GetInt(collections.FieldHoleNumber); number >= low && number <= high {
			selected = append(selected, hole)
		}
	}

	return selected
}

func isPlayMode(value string) bool {
	switch value {
	case collections.PlayModeFull, collections.PlayModeFront9, collections.PlayModeBack9:
		return true
	default:
		return false
	}
}

func resolveRoundTimes(
	defaultStartedAt, defaultFinishedAt types.DateTime,
	startedAtRaw, finishedAtRaw *string,
) (types.DateTime, types.DateTime, error) {
	startedAt := defaultStartedAt
	finishedAt := defaultFinishedAt

	if startedAtRaw != nil {
		parsed, err := parseRoundDateTime(*startedAtRaw)
		if err != nil {
			return types.DateTime{}, types.DateTime{}, apierr.Validation("Invalid startedAt")
		}
		startedAt = parsed
	}
	if finishedAtRaw != nil {
		parsed, err := parseRoundDateTime(*finishedAtRaw)
		if err != nil {
			return types.DateTime{}, types.DateTime{}, apierr.Validation("Invalid finishedAt")
		}
		finishedAt = parsed
	}
	if finishedAt.Before(startedAt) {
		return types.DateTime{}, types.DateTime{}, apierr.Validation("Finished time must be on or after the start time")
	}

	return startedAt, finishedAt, nil
}

func parseRoundDateTime(value string) (types.DateTime, error) {
	value = strings.TrimSpace(value)
	for _, layout := range roundDateTimeLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return types.ParseDateTime(parsed.UTC())
		}
	}

	return types.DateTime{}, fmt.Errorf("parse round datetime %q", value)
}
