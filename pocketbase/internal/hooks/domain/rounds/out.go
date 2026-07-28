package rounds

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/courses"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/roundholes"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/records"
)

// Out is Django's RoundOut: a round with its course nested inline (not an id
// plus `expand`) and `null` — not `0` or `""` — for the totals and
// `finishedAt` a round in progress has not earned yet.
//
// This is Phase 7's (#128) custom read route counterpart to the generated
// `/api/collections/rounds/records` endpoint. It closes API.md's gap 1
// (camelCase), gap 2 (null vs. zero) and gap 4 (relations nested inline) on the
// read path the frontend actually uses — the three gaps Phase 5 (#126) carried
// here because none of them close on generated CRUD.
type Out struct {
	ID            string       `json:"id"`
	Status        string       `json:"status"`
	PlayMode      string       `json:"playMode"`
	StartedAt     string       `json:"startedAt"`
	FinishedAt    interface{}  `json:"finishedAt"`
	Note          string       `json:"note"`
	CurrentHole   int          `json:"currentHole"`
	TotalStrokes  interface{}  `json:"totalStrokes"`
	TotalPar      interface{}  `json:"totalPar"`
	RelativeToPar interface{}  `json:"relativeToPar"`
	Course        *courses.Out `json:"course"`
}

// DetailOut is Django's RoundDetailOut: Out plus the round's own holes (each
// with its shots), the way `GET /api/rounds/{id}` and `GET
// /api/rounds/in-progress` return it.
type DetailOut struct {
	Out
	Holes []roundholes.Out `json:"holes"`
}

// NewOut builds the list-shaped response for a round: its own fields plus the
// course it was played on, without the per-hole detail only a single round's
// page needs.
func NewOut(app core.App, round *core.Record) (*Out, error) {
	course, err := app.FindRecordById(collections.NameCourses, round.GetString(collections.FieldCourse))
	if err != nil {
		return nil, err
	}
	courseOut, err := courses.NewOut(app, course)
	if err != nil {
		return nil, err
	}

	return newOut(round, courseOut), nil
}

// NewOutList is NewOut for a set of rounds, loading the courses they were played
// on — and those courses' holes — in two queries rather than two per round
// (Phase 8, #129).
//
// A player's rounds cluster onto a handful of courses, so the batch is usually
// far smaller than the list: twelve rounds at one club are one course lookup.
func NewOutList(app core.App, roundRecords []*core.Record) ([]*Out, error) {
	courseIDs := make([]string, 0, len(roundRecords))
	for _, round := range roundRecords {
		courseIDs = append(courseIDs, round.GetString(collections.FieldCourse))
	}

	courseRecords, err := records.CoursesByID(app, courseIDs)
	if err != nil {
		return nil, err
	}

	distinct := make([]*core.Record, 0, len(courseRecords))
	for _, course := range courseRecords {
		distinct = append(distinct, course)
	}

	courseOuts, err := courses.NewOutList(app, distinct)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]*courses.Out, len(courseOuts))
	for _, courseOut := range courseOuts {
		byID[courseOut.ID] = courseOut
	}

	out := make([]*Out, 0, len(roundRecords))
	for _, round := range roundRecords {
		courseOut, ok := byID[round.GetString(collections.FieldCourse)]
		if !ok {
			// rounds.course is a required relation with cascadeDelete off, so
			// the database refuses to delete a course a round still points at.
			// A round without one is corruption, not an empty list.
			return nil, fmt.Errorf("round %q references missing course %q",
				round.Id, round.GetString(collections.FieldCourse))
		}
		out = append(out, newOut(round, courseOut))
	}

	return out, nil
}

// newOut assembles the payload from a round and its already-built course.
func newOut(round *core.Record, courseOut *courses.Out) *Out {
	finished := round.GetDateTime(collections.FieldFinishedAt)

	out := &Out{
		ID:          round.Id,
		Status:      round.GetString(collections.FieldStatus),
		PlayMode:    round.GetString(collections.FieldPlayMode),
		StartedAt:   round.GetDateTime(collections.FieldStartedAt).String(),
		Note:        round.GetString(collections.FieldNote),
		CurrentHole: round.GetInt(collections.FieldCurrentHole),
		Course:      courseOut,
	}

	// A round only has finishedAt/the totals once it is completed — Django
	// types all four `| None` and PocketBase has no null for a number or an
	// unset date, so an in-progress round would otherwise read back as
	// finishedAt: "" and every total as 0 (API.md gap 2). Reading the
	// nullability off finished_at mirrors the model: the four fields are set
	// together, in Complete, and nowhere else.
	if !finished.IsZero() {
		out.FinishedAt = finished.String()
		out.TotalStrokes = round.GetInt(collections.FieldTotalStrokes)
		out.TotalPar = round.GetInt(collections.FieldTotalPar)
		out.RelativeToPar = round.GetInt(collections.FieldRelativeToPar)
	}

	return out
}

// NewDetailOut builds the detail-shaped response: NewOut plus the round's
// holes, each with its shots, in hole-number order.
//
// Four statements regardless of how long the round is — the course, its holes,
// the round's holes, and every shot on them — which is what makes the play and
// review pages cost the same on the eighteenth hole as on the first.
func NewDetailOut(app core.App, round *core.Record) (*DetailOut, error) {
	out, err := NewOut(app, round)
	if err != nil {
		return nil, err
	}

	holeRecords, err := records.RoundHoles(app, round.Id)
	if err != nil {
		return nil, err
	}

	holes, err := roundholes.NewOutList(app, holeRecords)
	if err != nil {
		return nil, err
	}

	return &DetailOut{Out: *out, Holes: holes}, nil
}
