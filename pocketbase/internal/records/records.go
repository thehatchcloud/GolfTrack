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
	hole, _, err := FindRoundHoleAndRound(app, roundID, holeNumber, userID)
	return hole, err
}

// FindRoundHoleAndRound is FindRoundHole returning the round it had to load
// anyway.
//
// Every caller that opens a hole for writing then checks the round's status, and
// fetching the round a second time for that check put a redundant read inside
// the write transaction — where SQLite's single writer makes every statement
// somebody else's queue (Phase 8, #129).
func FindRoundHoleAndRound(app core.App, roundID string, holeNumber int, userID string) (hole, round *core.Record, err error) {
	round, err = FindRound(app, roundID, userID)
	if err != nil {
		// A hole is only ever addressed through its round, and Django reports
		// the miss against the hole in both cases.
		return nil, nil, apierr.NotFound("Round hole not found")
	}

	holes, err := app.FindAllRecords(collections.NameRoundHoles, dbx.HashExp{
		collections.FieldRound:      round.Id,
		collections.FieldHoleNumber: holeNumber,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("find round hole %d of round %q: %w", holeNumber, roundID, err)
	}
	if len(holes) == 0 {
		return nil, nil, apierr.NotFound("Round hole not found")
	}

	return holes[0], round, nil
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

// The two aggregate forms of HoleShots (Phase 8, #129). Both are read inside a
// write transaction, where the shot list was never wanted for its own sake — one
// caller needs the next shot number and the other needs the count — and building
// a core.Record per shot to answer a one-row question lengthens the window every
// other writer is queued behind.

// LastShotNumber returns the highest shot_number on a hole, or 0 when it has no
// shots — which is also the number the next shot follows.
func LastShotNumber(app core.App, roundHoleID string) (int, error) {
	// MAX over no rows is SQL NULL, which is the empty hole — hence the
	// nullable scan target rather than a plain int.
	var highest sql.NullInt64

	err := app.ConcurrentDB().
		Select("MAX([[" + collections.FieldShotNumber + "]])").
		From(collections.NameShots).
		Where(dbx.HashExp{collections.FieldRoundHole: roundHoleID}).
		Row(&highest)
	if err != nil {
		return 0, fmt.Errorf("last shot number of round hole %q: %w", roundHoleID, err)
	}

	return int(highest.Int64), nil
}

// CountHoleShots returns how many shots are on a hole — the definition of its
// stroke cache.
func CountHoleShots(app core.App, roundHoleID string) (int, error) {
	var count int

	err := app.ConcurrentDB().
		Select("COUNT(*)").
		From(collections.NameShots).
		Where(dbx.HashExp{collections.FieldRoundHole: roundHoleID}).
		Row(&count)
	if err != nil {
		return 0, fmt.Errorf("count shots of round hole %q: %w", roundHoleID, err)
	}

	return count, nil
}

// LastShot returns the highest-numbered shot on a hole, or nil when it has none.
// Unlike LastShotNumber its caller needs the record itself, to delete it.
func LastShot(app core.App, roundHoleID string) (*core.Record, error) {
	var shots []*core.Record

	err := app.RecordQuery(collections.NameShots).
		AndWhere(dbx.HashExp{collections.FieldRoundHole: roundHoleID}).
		OrderBy(collections.FieldShotNumber + " DESC").
		Limit(1).
		All(&shots)
	if err != nil {
		return nil, fmt.Errorf("last shot of round hole %q: %w", roundHoleID, err)
	}
	if len(shots) == 0 {
		return nil, nil
	}

	return shots[0], nil
}

// The set-at-a-time counterparts of the three lookups above (Phase 8, #129).
//
// A response assembled record by record — a course per round, a shot list per
// hole — reads exactly like the Django it was ported from, where the ORM's
// select_related/prefetch_related hid the round trips. Nothing hides them here:
// the round detail response is one course, one hole set and one shot list, and
// building it a hole at a time cost two queries per hole. These return the whole
// set in one statement and let the caller do the grouping in Go, which is what
// keeps a request's cost independent of how deep a round is.
//
// Each returns a map keyed by the foreign key, and every requested id is present
// in it — an id with no children maps to an empty slice rather than to nothing,
// so a caller never has to distinguish "no rows" from "not asked for".

// CoursesByID returns the given courses keyed by id, in one query. Ids that do
// not exist are simply absent, which is the same signal FindRecordById's
// sql.ErrNoRows carries.
func CoursesByID(app core.App, courseIDs []string) (map[string]*core.Record, error) {
	byID := make(map[string]*core.Record, len(courseIDs))

	err := eachChunk(unique(courseIDs), func(chunk []string) error {
		var courses []*core.Record
		err := app.RecordQuery(collections.NameCourses).
			AndWhere(dbx.In("id", toAny(chunk)...)).
			All(&courses)
		if err != nil {
			return fmt.Errorf("load %d courses: %w", len(chunk), err)
		}

		for _, course := range courses {
			byID[course.Id] = course
		}
		return nil
	})

	return byID, err
}

// CourseHolesByCourse returns the holes of every given course, keyed by course
// id and ordered by hole number within each course.
func CourseHolesByCourse(app core.App, courseIDs []string) (map[string][]*core.Record, error) {
	return groupBy(app, collections.NameCourseHoles, collections.FieldCourse,
		collections.FieldHoleNumber, courseIDs)
}

// RoundHolesByRound returns the holes of every given round, keyed by round id
// and ordered by hole number within each round.
func RoundHolesByRound(app core.App, roundIDs []string) (map[string][]*core.Record, error) {
	return groupBy(app, collections.NameRoundHoles, collections.FieldRound,
		collections.FieldHoleNumber, roundIDs)
}

// ShotsByRoundHole returns the shots of every given hole, keyed by round hole id
// and ordered by shot number within each hole.
func ShotsByRoundHole(app core.App, roundHoleIDs []string) (map[string][]*core.Record, error) {
	return groupBy(app, collections.NameShots, collections.FieldRoundHole,
		collections.FieldShotNumber, roundHoleIDs)
}

// groupBy loads every record of a collection whose parentField is one of parents
// and returns them grouped by that field, each group ordered by orderField.
//
// The ordering is applied by the database over the whole result set rather than
// per group, which is enough: rows arrive in (parent, order) sequence only if
// the parent is sorted too, so orderField is applied second and the grouping
// preserves the relative order of each parent's rows as they arrive.
func groupBy(app core.App, collection, parentField, orderField string, parents []string) (map[string][]*core.Record, error) {
	parents = unique(parents)

	grouped := make(map[string][]*core.Record, len(parents))
	for _, parent := range parents {
		grouped[parent] = nil
	}

	err := eachChunk(parents, func(chunk []string) error {
		var found []*core.Record
		err := app.RecordQuery(collection).
			AndWhere(dbx.In(parentField, toAny(chunk)...)).
			OrderBy(parentField+" ASC", orderField+" ASC").
			All(&found)
		if err != nil {
			return fmt.Errorf("load %s of %d %s: %w", collection, len(chunk), parentField, err)
		}

		for _, record := range found {
			parent := record.GetString(parentField)
			grouped[parent] = append(grouped[parent], record)
		}
		return nil
	})

	return grouped, err
}

// idChunk bounds how many ids go into one IN clause. SQLite's host-parameter
// limit is far higher, but a request whose query grows without limit is the
// thing these functions exist to avoid, and chunking is the only way to hold
// that for a list whose length is the user's to decide (the course list).
const idChunk = 500

func eachChunk(ids []string, fn func([]string) error) error {
	for start := 0; start < len(ids); start += idChunk {
		end := min(start+idChunk, len(ids))
		if err := fn(ids[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func unique(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func toAny(ids []string) []any {
	values := make([]any, len(ids))
	for i, id := range ids {
		values[i] = id
	}
	return values
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
