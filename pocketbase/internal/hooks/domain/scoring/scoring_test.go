package scoring

import "testing"

// Ported from tests/test_services.py — the scoring helpers are pure arithmetic
// in both implementations, so these are the Django cases verbatim.

func TestCalculate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		holes []Hole
		want  Totals
	}{
		{
			name:  "level par",
			holes: []Hole{{Par: 4, Strokes: 4}, {Par: 3, Strokes: 3}, {Par: 5, Strokes: 5}},
			want:  Totals{TotalPar: 12, TotalStrokes: 12, RelativeToPar: 0},
		},
		{
			// +1 on the par-4, +2 on the par-5, -1 on the par-3 -> net +2.
			name:  "over par",
			holes: []Hole{{Par: 4, Strokes: 5}, {Par: 5, Strokes: 7}, {Par: 3, Strokes: 2}},
			want:  Totals{TotalPar: 12, TotalStrokes: 14, RelativeToPar: 2},
		},
		{
			// -1 on the par-4, -2 on the par-5 -> net -3.
			name:  "under par",
			holes: []Hole{{Par: 4, Strokes: 3}, {Par: 5, Strokes: 3}},
			want:  Totals{TotalPar: 9, TotalStrokes: 6, RelativeToPar: -3},
		},
		{
			// An in-progress round with nothing played yet still totals its par.
			name:  "unplayed holes",
			holes: []Hole{{Par: 4, Strokes: 0}, {Par: 4, Strokes: 0}},
			want:  Totals{TotalPar: 8, TotalStrokes: 0, RelativeToPar: -8},
		},
		{
			name:  "no holes",
			holes: nil,
			want:  Totals{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Calculate(tc.holes); got != tc.want {
				t.Errorf("Calculate() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestFormatRelativeToPar(t *testing.T) {
	for _, tc := range []struct {
		value int
		want  string
	}{
		{0, "E"},
		{3, "+3"},
		{-2, "-2"},
	} {
		if got := FormatRelativeToPar(tc.value); got != tc.want {
			t.Errorf("FormatRelativeToPar(%d) = %q, want %q", tc.value, got, tc.want)
		}
	}
}
