// Package scoring ports rounds/scoring.py.
//
// It is the one domain package with no hooks: pure arithmetic over a round's
// holes, with no PocketBase types in its signatures, so it is unit-testable
// with plain `go test` and reusable from anywhere that has par and stroke
// counts to hand.
package scoring

import (
	"strconv"

	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
)

// Hole is one hole's contribution to a round total.
type Hole struct {
	Par     int
	Strokes int
}

// Totals is the result of calculate_round_totals.
type Totals struct {
	TotalPar      int
	TotalStrokes  int
	RelativeToPar int
}

// Calculate sums par and strokes across a round's holes.
func Calculate(holes []Hole) Totals {
	var totals Totals

	for _, hole := range holes {
		totals.TotalPar += hole.Par
		totals.TotalStrokes += hole.Strokes
	}
	totals.RelativeToPar = totals.TotalStrokes - totals.TotalPar

	return totals
}

// FromRecords adapts round_holes records to Calculate.
func FromRecords(holes []*core.Record) Totals {
	scores := make([]Hole, 0, len(holes))

	for _, hole := range holes {
		scores = append(scores, Hole{
			Par:     hole.GetInt(collections.FieldPar),
			Strokes: hole.GetInt(collections.FieldStrokes),
		})
	}

	return Calculate(scores)
}

// FormatRelativeToPar renders a score relative to par the way the UI shows it:
// "E" for level, an explicit "+" when over.
func FormatRelativeToPar(value int) string {
	switch {
	case value == 0:
		return "E"
	case value > 0:
		return "+" + strconv.Itoa(value)
	default:
		return strconv.Itoa(value)
	}
}
