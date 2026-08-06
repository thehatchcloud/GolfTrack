// The import half of round export/import (GOL-1): an ExportFile parsed by
// ParseImport, rebuilt as the importing player's completed rounds.
//
// Courses are matched by name and hole count. A round whose course does not
// exist in this instance is the error the issue calls for — the file cannot
// create courses, because a course here is an admin-curated record with its
// own hole set, not something a player import should conjure.
//
// Each round is rebuilt through the same lifecycle a played round takes:
// created in progress, its shots inserted one by one (so the shot hooks keep
// the stroke cache and the numbering invariants), then completed with totals
// summed by the scoring package. Writing the records "pre-completed" instead
// would bypass the very hooks that make the invariants hold on every other
// write path.
package rounds

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/apierr"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/scoring"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/records"
)

// ImportResult is the import route's response: how many rounds were created
// and how many were skipped as already present.
type ImportResult struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
}

// Import validates the file, then rebuilds its rounds for userID inside one
// transaction — a file with any invalid round imports nothing, so a failed
// import never leaves half a history behind.
//
// A round that already exists (same course and start time, completed) is
// skipped rather than duplicated, so importing the same file twice is safe.
func Import(app core.App, userID string, file *ExportFile) (*ImportResult, error) {
	if len(file.Rounds) == 0 {
		return nil, apierr.Validation("Import file contains no rounds")
	}

	for i := range file.Rounds {
		if err := validateExportRound(i, &file.Rounds[i]); err != nil {
			return nil, err
		}
	}

	result := &ImportResult{}

	txErr := app.RunInTransaction(func(txApp core.App) error {
		courses, err := resolveImportCourses(txApp, file.Rounds)
		if err != nil {
			return err
		}

		// Each import passes through an in-progress round, and the partial
		// unique index allows only one per user — a round the player is
		// actually out playing must not collide with it.
		existing, err := records.InProgressRound(txApp, userID)
		if err != nil {
			return err
		}
		if existing != nil {
			return apierr.Conflict("Finish or cancel your in-progress round before importing")
		}

		for i := range file.Rounds {
			round := &file.Rounds[i]
			imported, err := importRound(txApp, userID, courses[courseKey(round.Course)], round)
			if err != nil {
				return fmt.Errorf("import round %d: %w", i+1, err)
			}
			if imported {
				result.Imported++
			} else {
				result.Skipped++
			}
		}

		return nil
	})
	if txErr != nil {
		return nil, txErr
	}

	return result, nil
}

// validateExportRound checks one round against the schema's own bounds and the
// domain's hole-set rules, so every failure surfaces as a 400 naming the round
// — not as a save error deep inside the transaction.
func validateExportRound(index int, round *ExportRound) error {
	label := fmt.Sprintf("Round %d", index+1)

	round.Course.Name = strings.TrimSpace(round.Course.Name)
	if round.Course.Name == "" {
		return apierr.Validation(label + " is missing its course name")
	}
	if round.Course.HoleCount != 9 && round.Course.HoleCount != 18 {
		return apierr.Validation(fmt.Sprintf("%s has an invalid course hole count: %d", label, round.Course.HoleCount))
	}

	if !isPlayMode(round.PlayMode) {
		return apierr.Validation(fmt.Sprintf("%s has an invalid play mode: %q", label, round.PlayMode))
	}
	if round.Course.HoleCount != 18 && round.PlayMode != collections.PlayModeFull {
		return apierr.Validation(label + " plays front 9 or back 9 on a 9-hole course")
	}

	startedAt, err := parseRoundDateTime(round.StartedAt, time.UTC)
	if err != nil {
		return apierr.Validation(label + " has an invalid startedAt")
	}
	finishedAt, err := parseRoundDateTime(round.FinishedAt, time.UTC)
	if err != nil {
		return apierr.Validation(label + " has an invalid finishedAt")
	}
	if finishedAt.Before(startedAt) {
		return apierr.Validation(label + " finishes before it starts")
	}

	round.Note = strings.TrimSpace(round.Note)
	if len(round.Note) > noteMaxLength {
		return apierr.Validation(label + "'s note is too long")
	}

	expected := expectedHoleNumbers(round.Course.HoleCount, round.PlayMode)
	seen := map[int]bool{}
	for _, hole := range round.Holes {
		if !expected[hole.HoleNumber] {
			return apierr.Validation(fmt.Sprintf("%s has an unexpected hole number: %d", label, hole.HoleNumber))
		}
		if seen[hole.HoleNumber] {
			return apierr.Validation(fmt.Sprintf("%s lists hole %d twice", label, hole.HoleNumber))
		}
		seen[hole.HoleNumber] = true

		if hole.Par < 3 || hole.Par > 6 {
			return apierr.Validation(fmt.Sprintf("%s hole %d has an invalid par: %d", label, hole.HoleNumber, hole.Par))
		}

		for i, shot := range hole.Shots {
			if shot.ShotNumber != i+1 {
				return apierr.Validation(fmt.Sprintf("%s hole %d's shots are not numbered 1..%d in order", label, hole.HoleNumber, len(hole.Shots)))
			}
			club := strings.TrimSpace(shot.Club)
			if club == "" || len(club) > 32 {
				return apierr.Validation(fmt.Sprintf("%s hole %d shot %d has an invalid club", label, hole.HoleNumber, shot.ShotNumber))
			}
		}
	}
	if len(seen) != len(expected) {
		return apierr.Validation(fmt.Sprintf("%s does not cover every hole its play mode requires", label))
	}

	// The snapshot order rounds are created with, whatever order the file
	// carried the holes in.
	sort.Slice(round.Holes, func(i, j int) bool {
		return round.Holes[i].HoleNumber < round.Holes[j].HoleNumber
	})

	return nil
}

// expectedHoleNumbers is the exact hole set a play mode snapshots — the same
// selection selectHoles makes at round creation.
func expectedHoleNumbers(holeCount int, playMode string) map[int]bool {
	low, high := 1, holeCount
	switch playMode {
	case collections.PlayModeFront9:
		low, high = 1, 9
	case collections.PlayModeBack9:
		low, high = 10, 18
	}

	expected := make(map[int]bool, high-low+1)
	for number := low; number <= high; number++ {
		expected[number] = true
	}
	return expected
}

func courseKey(course ExportCourse) string {
	return fmt.Sprintf("%s\x00%d", course.Name, course.HoleCount)
}

// resolveImportCourses maps every distinct course the file references to a
// course record, matched by exact name and hole count. Every unmatchable
// course is reported in one error, so a player fixes the whole list in one
// pass rather than one upload per missing course.
func resolveImportCourses(app core.App, importRounds []ExportRound) (map[string]*core.Record, error) {
	resolved := map[string]*core.Record{}
	var problems []string

	for _, round := range importRounds {
		key := courseKey(round.Course)
		if _, done := resolved[key]; done {
			continue
		}

		candidates, err := app.FindAllRecords(collections.NameCourses,
			dbx.HashExp{collections.FieldName: round.Course.Name})
		if err != nil {
			return nil, fmt.Errorf("find course %q: %w", round.Course.Name, err)
		}

		var match *core.Record
		for _, candidate := range candidates {
			if candidate.GetInt(collections.FieldHoleCount) != round.Course.HoleCount {
				continue
			}
			// Prefer an active course; an archived one still matches, since
			// history may be imported long after a course is retired.
			if match == nil || (match.GetBool(collections.FieldIsArchived) && !candidate.GetBool(collections.FieldIsArchived)) {
				match = candidate
			}
		}

		switch {
		case match != nil:
			resolved[key] = match
		case len(candidates) > 0:
			problems = append(problems, fmt.Sprintf("course %q exists but does not have %d holes",
				round.Course.Name, round.Course.HoleCount))
		default:
			problems = append(problems, fmt.Sprintf("course %q does not exist in this instance",
				round.Course.Name))
		}
	}

	if len(problems) > 0 {
		return nil, apierr.Validation("Cannot import: " + strings.Join(problems, "; "))
	}

	return resolved, nil
}

// importRound rebuilds one already-validated round, or skips it when an equal
// round is already on the player's history. Returns whether it imported.
func importRound(app core.App, userID string, course *core.Record, round *ExportRound) (bool, error) {
	startedAt, err := parseRoundDateTime(round.StartedAt, time.UTC)
	if err != nil {
		return false, fmt.Errorf("parse startedAt %q: %w", round.StartedAt, err)
	}
	finishedAt, err := parseRoundDateTime(round.FinishedAt, time.UTC)
	if err != nil {
		return false, fmt.Errorf("parse finishedAt %q: %w", round.FinishedAt, err)
	}

	duplicate, err := hasEqualRound(app, userID, course.Id, startedAt)
	if err != nil {
		return false, err
	}
	if duplicate {
		return false, nil
	}

	roundsCollection, err := app.FindCollectionByNameOrId(collections.NameRounds)
	if err != nil {
		return false, fmt.Errorf("find %s collection: %w", collections.NameRounds, err)
	}

	record := core.NewRecord(roundsCollection)
	record.Set(collections.FieldUser, userID)
	record.Set(collections.FieldCourse, course.Id)
	record.Set(collections.FieldStatus, collections.RoundStatusInProgress)
	record.Set(collections.FieldPlayMode, round.PlayMode)
	record.Set(collections.FieldStartedAt, startedAt)
	record.Set(collections.FieldCurrentHole, round.Holes[0].HoleNumber)
	if err := app.Save(record); err != nil {
		return false, fmt.Errorf("create round: %w", err)
	}

	holesCollection, err := app.FindCollectionByNameOrId(collections.NameRoundHoles)
	if err != nil {
		return false, fmt.Errorf("find %s collection: %w", collections.NameRoundHoles, err)
	}
	shotsCollection, err := app.FindCollectionByNameOrId(collections.NameShots)
	if err != nil {
		return false, fmt.Errorf("find %s collection: %w", collections.NameShots, err)
	}

	for _, hole := range round.Holes {
		holeRecord := core.NewRecord(holesCollection)
		holeRecord.Set(collections.FieldRound, record.Id)
		holeRecord.Set(collections.FieldHoleNumber, hole.HoleNumber)
		holeRecord.Set(collections.FieldPar, hole.Par)
		holeRecord.Set(collections.FieldStrokes, 0)
		if err := app.Save(holeRecord); err != nil {
			return false, fmt.Errorf("create hole %d: %w", hole.HoleNumber, err)
		}

		for _, shot := range hole.Shots {
			shotRecord := core.NewRecord(shotsCollection)
			shotRecord.Set(collections.FieldRoundHole, holeRecord.Id)
			shotRecord.Set(collections.FieldShotNumber, shot.ShotNumber)
			shotRecord.Set(collections.FieldClub, strings.TrimSpace(shot.Club))
			if err := app.Save(shotRecord); err != nil {
				return false, fmt.Errorf("create shot %d on hole %d: %w", shot.ShotNumber, hole.HoleNumber, err)
			}
		}
	}

	// Reload the holes so the totals sum the stroke cache the shot hooks just
	// maintained, exactly as Complete does for a played round.
	holes, err := records.RoundHoles(app, record.Id)
	if err != nil {
		return false, err
	}
	totals := scoring.FromRecords(holes)

	record.Set(collections.FieldStatus, collections.RoundStatusCompleted)
	record.Set(collections.FieldFinishedAt, finishedAt)
	record.Set(collections.FieldNote, round.Note)
	record.Set(collections.FieldTotalStrokes, totals.TotalStrokes)
	record.Set(collections.FieldTotalPar, totals.TotalPar)
	record.Set(collections.FieldRelativeToPar, totals.RelativeToPar)
	if err := app.Save(record); err != nil {
		return false, fmt.Errorf("complete imported round: %w", err)
	}

	return true, nil
}

// hasEqualRound reports whether the player already has a completed round on
// this course starting at the same instant — the identity that makes a
// re-import a no-op instead of a duplicate history.
func hasEqualRound(app core.App, userID, courseID string, startedAt types.DateTime) (bool, error) {
	existing, err := app.FindAllRecords(collections.NameRounds, dbx.HashExp{
		collections.FieldUser:   userID,
		collections.FieldCourse: courseID,
		collections.FieldStatus: collections.RoundStatusCompleted,
	})
	if err != nil {
		return false, fmt.Errorf("list existing rounds on course %q: %w", courseID, err)
	}

	for _, record := range existing {
		if record.GetDateTime(collections.FieldStartedAt).Time().Equal(startedAt.Time()) {
			return true, nil
		}
	}
	return false, nil
}
