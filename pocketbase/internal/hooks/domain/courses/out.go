package courses

import (
	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/records"
)

// HoleOut is Django's CourseHoleOut, in the camelCase the current API returns.
type HoleOut struct {
	ID         string `json:"id"`
	HoleNumber int    `json:"holeNumber"`
	Par        int    `json:"par"`
}

// Out is Django's CourseOut: a course with its holes nested inline and
// `totalPar` derived rather than stored, the same way registerDerivedTotalPar
// computes it for the generated endpoints.
//
// This is Phase 7's (#128) custom read route counterpart to the generated
// `/api/collections/courses/records` endpoint — it closes API.md's gap 1
// (camelCase) and gap 4 (relations nested inline, not id + expand) on the read
// path the frontend actually uses, the way roundholes.Out already does for a
// round hole.
type Out struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	HoleCount  int       `json:"holeCount"`
	TotalPar   int       `json:"totalPar"`
	IsArchived bool      `json:"isArchived"`
	Holes      []HoleOut `json:"holes"`
	CreatedAt  string    `json:"createdAt"`
	UpdatedAt  string    `json:"updatedAt"`
}

// NewOut builds the response payload for a course, in hole-number order.
func NewOut(app core.App, course *core.Record) (*Out, error) {
	holes, err := records.CourseHoles(app, course.Id)
	if err != nil {
		return nil, err
	}

	out := &Out{
		ID:         course.Id,
		Name:       course.GetString(collections.FieldName),
		HoleCount:  course.GetInt(collections.FieldHoleCount),
		IsArchived: course.GetBool(collections.FieldIsArchived),
		Holes:      make([]HoleOut, 0, len(holes)),
		CreatedAt:  course.GetDateTime(collections.FieldCreatedAt).String(),
		UpdatedAt:  course.GetDateTime(collections.FieldUpdatedAt).String(),
	}

	for _, hole := range holes {
		par := hole.GetInt(collections.FieldPar)
		out.TotalPar += par
		out.Holes = append(out.Holes, HoleOut{
			ID:         hole.Id,
			HoleNumber: hole.GetInt(collections.FieldHoleNumber),
			Par:        par,
		})
	}

	return out, nil
}
