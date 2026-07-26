package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/rounds"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/shots"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/records"
)

// Phase 3 (#124) domain suite: the business rules, against a real database.
//
// These are the cases in tests/test_services.py, which is the specification the
// migration has to preserve — the Django service layer is itself a port of the
// Next.js lib/, so the behaviour asserted here has survived two rewrites.
// Endpoint-level behaviour is in routes_test.go; races are in concurrency_test.go.

// game is the seeded world: one player, another player to prove ownership
// scoping, a full 18-hole course (par 4 throughout, so total par is 72) and a
// full 9-hole course.
type game struct {
	app    *tests.TestApp
	user   *core.Record
	other  *core.Record
	course *core.Record
	nine   *core.Record
}

func newGame(t *testing.T) *game {
	t.Helper()

	app := newTestApp(t)
	g := &game{app: app}
	g.user = createUser(t, app, "", "alice@golftrack.test", collections.UserRoleUser)
	g.other = createUser(t, app, "", "bob@golftrack.test", collections.UserRoleUser)
	g.course = seedCourse(t, app, "", "Test Course", 18, 4)
	g.nine = seedCourse(t, app, "", "Nine Hole", 9, 3)

	return g
}

// start opens the round the shot tests play on.
func (g *game) start(t *testing.T) *core.Record {
	t.Helper()

	round, err := rounds.Create(g.app, g.user.Id, g.course.Id, collections.PlayModeFull)
	if err != nil {
		t.Fatalf("create round: %v", err)
	}

	return round
}

func (g *game) hole(t *testing.T, round *core.Record, number int) *core.Record {
	t.Helper()

	hole, err := records.FindRoundHole(g.app, round.Id, number, g.user.Id)
	if err != nil {
		t.Fatalf("find hole %d of round %q: %v", number, round.Id, err)
	}

	return hole
}

func (g *game) holeNumbers(t *testing.T, round *core.Record) []int {
	t.Helper()

	holes, err := records.RoundHoles(g.app, round.Id)
	if err != nil {
		t.Fatalf("list holes of round %q: %v", round.Id, err)
	}

	numbers := make([]int, 0, len(holes))
	for _, hole := range holes {
		numbers = append(numbers, hole.GetInt(collections.FieldHoleNumber))
	}

	return numbers
}

func rangeInts(from, to int) []int {
	numbers := make([]int, 0, to-from+1)
	for n := from; n <= to; n++ {
		numbers = append(numbers, n)
	}
	return numbers
}

// -----------------------------------------------------------------------------
// course rules
// -----------------------------------------------------------------------------

// TestHoleCountMustBeNineOrEighteen covers the rule the schema cannot express:
// pb_schema.json bounds hole_count to 9–18 because PocketBase number fields have
// no value set, so everything in between is a hook's job.
func TestHoleCountMustBeNineOrEighteen(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		app := newTestApp(t)
		twelve := newRecord(t, app, collections.NameCourses, "", map[string]any{
			"name": "Twelve Holes", "hole_count": 12,
		})
		saveShouldFail(t, app, twelve, collections.FieldHoleCount)
	})

	t.Run("update", func(t *testing.T) {
		app := newTestApp(t)
		course := seedCourse(t, app, "", "Eighteen", 18, 4)

		course.Set(collections.FieldHoleCount, 12)
		saveShouldFail(t, app, course, collections.FieldHoleCount)
	})

	t.Run("both valid sizes are accepted", func(t *testing.T) {
		app := newTestApp(t)
		seedCourse(t, app, "", "Nine", 9, 3)
		seedCourse(t, app, "", "Eighteen", 18, 4)
	})
}

// TestCourseHoleMustFitItsCourse is the per-record half of Django's
// validate_holes: with the unique (course, hole_number) index, bounding every
// hole to 1..hole_count makes a full set exactly {1..hole_count}.
func TestCourseHoleMustFitItsCourse(t *testing.T) {
	app := newTestApp(t)
	nine := seedCourse(t, app, "", "Nine", 9, 3)

	tenth := newRecord(t, app, collections.NameCourseHoles, "", map[string]any{
		"course": nine.Id, "hole_number": 10, "par": 4,
	})
	saveShouldFail(t, app, tenth, collections.FieldHoleNumber)
}

// -----------------------------------------------------------------------------
// create_round
// -----------------------------------------------------------------------------

func TestCreateRoundSnapshotsTheCourse(t *testing.T) {
	g := newGame(t)
	round := g.start(t)

	if got := len(g.holeNumbers(t, round)); got != 18 {
		t.Fatalf("round has %d holes, want 18", got)
	}
	if got := g.hole(t, round, 1).GetInt(collections.FieldPar); got != 4 {
		t.Errorf("hole 1 par = %d, want 4 (snapshotted from the course)", got)
	}
	if got := round.GetInt(collections.FieldCurrentHole); got != 1 {
		t.Errorf("current_hole = %d, want 1", got)
	}
}

// TestCreateRoundSnapshotIsIndependentOfTheCourse is the rule the whole snapshot
// exists for: editing a course must not rescore a round already played on it.
func TestCreateRoundSnapshotIsIndependentOfTheCourse(t *testing.T) {
	g := newGame(t)
	round := g.start(t)

	courseHole, err := g.app.FindFirstRecordByFilter(
		collections.NameCourseHoles, "course = {:course} && hole_number = 1",
		map[string]any{"course": g.course.Id},
	)
	if err != nil {
		t.Fatalf("find course hole 1: %v", err)
	}
	courseHole.Set(collections.FieldPar, 5)
	if err := g.app.Save(courseHole); err != nil {
		t.Fatalf("re-par course hole 1: %v", err)
	}

	if got := g.hole(t, round, 1).GetInt(collections.FieldPar); got != 4 {
		t.Errorf("round hole 1 par = %d after the course changed, want the snapshotted 4", got)
	}
}

func TestCreateRoundPlayModes(t *testing.T) {
	for _, tc := range []struct {
		playMode string
		want     []int
	}{
		{collections.PlayModeFull, rangeInts(1, 18)},
		{collections.PlayModeFront9, rangeInts(1, 9)},
		{collections.PlayModeBack9, rangeInts(10, 18)},
	} {
		t.Run(tc.playMode, func(t *testing.T) {
			g := newGame(t)

			round, err := rounds.Create(g.app, g.user.Id, g.course.Id, tc.playMode)
			if err != nil {
				t.Fatalf("create %s round: %v", tc.playMode, err)
			}

			if got := g.holeNumbers(t, round); !equalInts(got, tc.want) {
				t.Errorf("holes = %v, want %v", got, tc.want)
			}
			if got, want := round.GetInt(collections.FieldCurrentHole), tc.want[0]; got != want {
				t.Errorf("current_hole = %d, want %d", got, want)
			}
		})
	}
}

func TestCreateRoundRejections(t *testing.T) {
	t.Run("nine holes cannot be played as front9", func(t *testing.T) {
		g := newGame(t)
		_, err := rounds.Create(g.app, g.user.Id, g.nine.Id, collections.PlayModeFront9)
		wantAPIError(t, err, http.StatusBadRequest, "Front 9 or back 9 is only available on 18-hole courses")
	})

	t.Run("unknown play mode", func(t *testing.T) {
		g := newGame(t)
		_, err := rounds.Create(g.app, g.user.Id, g.course.Id, "middle9")
		wantAPIError(t, err, http.StatusBadRequest, "Invalid play mode: middle9")
	})

	t.Run("missing course", func(t *testing.T) {
		g := newGame(t)
		_, err := rounds.Create(g.app, g.user.Id, fixedID("nosuchcourse"), collections.PlayModeFull)
		wantAPIError(t, err, http.StatusNotFound, "Course not found")
	})

	t.Run("second in-progress round", func(t *testing.T) {
		g := newGame(t)
		g.start(t)

		_, err := rounds.Create(g.app, g.user.Id, g.course.Id, collections.PlayModeFull)
		wantAPIError(t, err, http.StatusConflict, "A round is already in progress")
	})

	// Django validates a course and its holes as one payload, so an incomplete
	// course cannot exist there. Here the holes are separate records, and round
	// creation is the first place the set is read as a whole.
	t.Run("course missing holes", func(t *testing.T) {
		g := newGame(t)

		stub := createCourse(t, g.app, "", "Half Built", 18)
		createCourseHole(t, g.app, "", stub, 1, 4)

		_, err := rounds.Create(g.app, g.user.Id, stub.Id, collections.PlayModeFull)
		wantAPIError(t, err, http.StatusBadRequest, "Expected 18 holes")
	})
}

// TestCreateRoundIsAllOrNothing pins the transaction: a create that fails must
// not leave a round behind without its holes.
func TestCreateRoundIsAllOrNothing(t *testing.T) {
	g := newGame(t)
	g.start(t)

	if _, err := rounds.Create(g.app, g.user.Id, g.nine.Id, collections.PlayModeFull); err == nil {
		t.Fatal("second create succeeded, expected a conflict")
	}

	rounds, err := g.app.FindAllRecords(collections.NameRounds)
	if err != nil {
		t.Fatalf("list rounds: %v", err)
	}
	if len(rounds) != 1 {
		t.Errorf("%d rounds exist after a rejected create, want 1", len(rounds))
	}
}

// -----------------------------------------------------------------------------
// add / undo / update / delete shot
// -----------------------------------------------------------------------------

func TestAddShotMaintainsTheStrokeCache(t *testing.T) {
	g := newGame(t)
	round := g.start(t)

	hole, err := shots.Add(g.app, g.user.Id, round.Id, 1, "Driver")
	if err != nil {
		t.Fatalf("add first shot: %v", err)
	}
	if _, err := shots.Add(g.app, g.user.Id, round.Id, 1, "7i"); err != nil {
		t.Fatalf("add second shot: %v", err)
	}

	strokes, numbers, clubs := holeState(t, g.app, hole.Id)
	if strokes != 2 {
		t.Errorf("strokes = %d, want 2", strokes)
	}
	if !equalInts(numbers, []int{1, 2}) {
		t.Errorf("shot numbers = %v, want [1 2]", numbers)
	}
	if clubs[0] != "Driver" || clubs[1] != "7i" {
		t.Errorf("clubs = %v, want [Driver 7i]", clubs)
	}
}

func TestAddShotTrimsAndValidatesTheClub(t *testing.T) {
	g := newGame(t)
	round := g.start(t)

	hole, err := shots.Add(g.app, g.user.Id, round.Id, 1, "  Driver  ")
	if err != nil {
		t.Fatalf("add shot: %v", err)
	}
	if _, _, clubs := holeState(t, g.app, hole.Id); clubs[0] != "Driver" {
		t.Errorf("club = %q, want it trimmed to %q", clubs[0], "Driver")
	}

	_, err = shots.Add(g.app, g.user.Id, round.Id, 1, "   ")
	wantAPIError(t, err, http.StatusBadRequest, "Club is required")

	_, err = shots.Add(g.app, g.user.Id, round.Id, 1, strings.Repeat("x", 33))
	wantAPIError(t, err, http.StatusBadRequest, "Club name is too long")
}

func TestShotOperationsAreScopedToTheRoundOwner(t *testing.T) {
	g := newGame(t)
	round := g.start(t)

	_, err := shots.Add(g.app, g.other.Id, round.Id, 1, "Driver")
	wantAPIError(t, err, http.StatusNotFound, "Round hole not found")

	_, err = shots.Add(g.app, g.user.Id, round.Id, 99, "Driver")
	wantAPIError(t, err, http.StatusNotFound, "Round hole not found")
}

func TestUndoRemovesOnlyTheLastShot(t *testing.T) {
	g := newGame(t)
	round := g.start(t)

	mustAddShots(t, g, round, 1, "Driver", "7i")

	hole, err := shots.Undo(g.app, g.user.Id, round.Id, 1)
	if err != nil {
		t.Fatalf("undo: %v", err)
	}

	strokes, numbers, clubs := holeState(t, g.app, hole.Id)
	if strokes != 1 || !equalInts(numbers, []int{1}) || clubs[0] != "Driver" {
		t.Errorf("after undo: strokes=%d numbers=%v clubs=%v, want 1 [1] [Driver]", strokes, numbers, clubs)
	}
}

func TestUndoOnAnEmptyHoleIsANoop(t *testing.T) {
	g := newGame(t)
	round := g.start(t)

	hole, err := shots.Undo(g.app, g.user.Id, round.Id, 1)
	if err != nil {
		t.Fatalf("undo on an empty hole: %v", err)
	}
	if strokes, numbers, _ := holeState(t, g.app, hole.Id); strokes != 0 || len(numbers) != 0 {
		t.Errorf("strokes=%d numbers=%v, want 0 and no shots", strokes, numbers)
	}
}

// TestDeleteShotRenumbersSubsequentShots is the ordering case the single
// UPDATE exists for: the surviving shots have to end up 1,2 in their original
// order, not merely two rows with some pair of numbers.
func TestDeleteShotRenumbersSubsequentShots(t *testing.T) {
	g := newGame(t)
	round := g.start(t)

	mustAddShots(t, g, round, 1, "Driver", "7i", "PW")
	hole := g.hole(t, round, 1)

	_, numbers, _ := holeState(t, g.app, hole.Id)
	if !equalInts(numbers, []int{1, 2, 3}) {
		t.Fatalf("shot numbers before the delete = %v, want [1 2 3]", numbers)
	}

	shotList, err := records.HoleShots(g.app, hole.Id)
	if err != nil {
		t.Fatalf("list shots: %v", err)
	}
	middle := shotList[1]

	if _, err := shots.Delete(g.app, g.user.Id, round.Id, 1, middle.Id); err != nil {
		t.Fatalf("delete the middle shot: %v", err)
	}

	strokes, numbers, clubs := holeState(t, g.app, hole.Id)
	if strokes != 2 {
		t.Errorf("strokes = %d, want 2", strokes)
	}
	if !equalInts(numbers, []int{1, 2}) {
		t.Errorf("shot numbers = %v, want [1 2] — the gap must close", numbers)
	}
	if clubs[0] != "Driver" || clubs[1] != "PW" {
		t.Errorf("clubs = %v, want [Driver PW] — renumbering must not reorder the shots", clubs)
	}
}

func TestDeleteMissingShot(t *testing.T) {
	g := newGame(t)
	round := g.start(t)

	_, err := shots.Delete(g.app, g.user.Id, round.Id, 1, fixedID("nosuchshot"))
	wantAPIError(t, err, http.StatusNotFound, "Shot not found")
}

func TestUpdateShotChangesTheClubOnly(t *testing.T) {
	g := newGame(t)
	round := g.start(t)

	mustAddShots(t, g, round, 1, "Driver")
	hole := g.hole(t, round, 1)
	shotList, err := records.HoleShots(g.app, hole.Id)
	if err != nil {
		t.Fatalf("list shots: %v", err)
	}

	updated, err := shots.UpdateClub(g.app, g.user.Id, round.Id, 1, shotList[0].Id, "3W")
	if err != nil {
		t.Fatalf("update shot: %v", err)
	}
	if got := updated.GetString(collections.FieldClub); got != "3W" {
		t.Errorf("club = %q, want %q", got, "3W")
	}

	strokes, numbers, _ := holeState(t, g.app, hole.Id)
	if strokes != 1 || !equalInts(numbers, []int{1}) {
		t.Errorf("strokes=%d numbers=%v, want 1 [1] — an update must not touch either", strokes, numbers)
	}
}

// TestTheStrokeCacheSurvivesTheGeneratedEndpoints proves the invariants live in
// record hooks rather than only in the service functions: a shot written or
// removed straight through the record API — which the round's owner is allowed
// to do — still leaves the cache and the numbering correct.
func TestTheStrokeCacheSurvivesTheGeneratedEndpoints(t *testing.T) {
	g := newGame(t)
	round := g.start(t)
	hole := g.hole(t, round, 1)

	for number, club := range map[int]string{1: "Driver", 2: "7i", 3: "PW"} {
		saveAs(t, g.app, newRecord(t, g.app, collections.NameShots, "", map[string]any{
			"round_hole": hole.Id, "shot_number": number, "club": club,
		}))
	}

	if strokes, numbers, _ := holeState(t, g.app, hole.Id); strokes != 3 || !equalInts(numbers, []int{1, 2, 3}) {
		t.Fatalf("after three direct saves: strokes=%d numbers=%v, want 3 [1 2 3]", strokes, numbers)
	}

	shotList, err := records.HoleShots(g.app, hole.Id)
	if err != nil {
		t.Fatalf("list shots: %v", err)
	}
	if err := g.app.Delete(shotList[0]); err != nil {
		t.Fatalf("delete the first shot directly: %v", err)
	}

	strokes, numbers, clubs := holeState(t, g.app, hole.Id)
	if strokes != 2 || !equalInts(numbers, []int{1, 2}) {
		t.Errorf("after a direct delete: strokes=%d numbers=%v, want 2 [1 2]", strokes, numbers)
	}
	if clubs[0] != "7i" || clubs[1] != "PW" {
		t.Errorf("clubs = %v, want [7i PW]", clubs)
	}
}

// TestRenumberingIsIndependentOfInsertionOrder pins the fix for a bug the test
// above could only find by luck.
//
// Renumbering is a single `SET shot_number = shot_number - 1` sweep, and SQLite
// checks the unique (round_hole, shot_number) index per row, in the order its
// scan visits them — insertion order, not shot_number order. So `3 -> 2` ahead
// of `2 -> 1` failed with a UNIQUE constraint violation, reachable whenever a
// shot was written out of order straight through the generated record endpoint,
// which the round's owner is allowed to do.
//
// TestTheStrokeCacheSurvivesTheGeneratedEndpoints inserts its three shots by
// ranging over a map, so Go's randomised iteration order hit the bad case
// roughly a quarter of the time and the suite failed intermittently. This
// spells the orders out instead.
func TestRenumberingIsIndependentOfInsertionOrder(t *testing.T) {
	for name, order := range map[string][]int{
		"ascending":                     {1, 2, 3},
		"descending":                    {3, 2, 1},
		"highest first, then ascending": {3, 1, 2},
		"lowest last":                   {2, 3, 1},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGame(t)
			round := g.start(t)
			hole := g.hole(t, round, 1)

			clubs := map[int]string{1: "Driver", 2: "7i", 3: "PW"}
			for _, number := range order {
				saveAs(t, g.app, newRecord(t, g.app, collections.NameShots, "", map[string]any{
					"round_hole": hole.Id, "shot_number": number, "club": clubs[number],
				}))
			}

			shotList, err := records.HoleShots(g.app, hole.Id)
			if err != nil {
				t.Fatalf("list shots: %v", err)
			}
			if err := g.app.Delete(shotList[0]); err != nil {
				t.Fatalf("delete the first shot: %v", err)
			}

			strokes, numbers, gotClubs := holeState(t, g.app, hole.Id)
			if strokes != 2 || !equalInts(numbers, []int{1, 2}) {
				t.Errorf("strokes=%d numbers=%v, want 2 [1 2]", strokes, numbers)
			}
			if len(gotClubs) != 2 || gotClubs[0] != "7i" || gotClubs[1] != "PW" {
				t.Errorf("clubs = %v, want [7i PW] — the survivors must keep their order", gotClubs)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// set_current_hole
// -----------------------------------------------------------------------------

func TestSetCurrentHole(t *testing.T) {
	g := newGame(t)
	round := g.start(t)

	updated, err := rounds.SetCurrentHole(g.app, g.user.Id, round.Id, 5)
	if err != nil {
		t.Fatalf("set current hole: %v", err)
	}
	if got := updated.GetInt(collections.FieldCurrentHole); got != 5 {
		t.Errorf("current_hole = %d, want 5", got)
	}

	_, err = rounds.SetCurrentHole(g.app, g.user.Id, round.Id, 99)
	wantAPIError(t, err, http.StatusBadRequest, "Invalid hole number")
}

// TestSetCurrentHoleIsScopedToTheRoundsOwnHoles is what makes the check more
// than a range check: on a back9 round, hole 3 exists on the course but is not
// being played.
func TestSetCurrentHoleIsScopedToTheRoundsOwnHoles(t *testing.T) {
	g := newGame(t)

	round, err := rounds.Create(g.app, g.user.Id, g.course.Id, collections.PlayModeBack9)
	if err != nil {
		t.Fatalf("create back9 round: %v", err)
	}

	_, err = rounds.SetCurrentHole(g.app, g.user.Id, round.Id, 3)
	wantAPIError(t, err, http.StatusBadRequest, "Invalid hole number")

	if _, err := rounds.SetCurrentHole(g.app, g.user.Id, round.Id, 12); err != nil {
		t.Errorf("hole 12 should be playable on a back9 round: %v", err)
	}
}

// -----------------------------------------------------------------------------
// complete_round
// -----------------------------------------------------------------------------

func TestCompleteRoundSetsTotals(t *testing.T) {
	g := newGame(t)
	round := g.start(t)

	mustAddShots(t, g, round, 1, "Driver")

	completed, err := rounds.Complete(g.app, g.user.Id, round.Id, "  Great round  ")
	if err != nil {
		t.Fatalf("complete round: %v", err)
	}

	if got := completed.GetString(collections.FieldStatus); got != collections.RoundStatusCompleted {
		t.Errorf("status = %q, want %q", got, collections.RoundStatusCompleted)
	}
	if got := completed.GetInt(collections.FieldTotalStrokes); got != 1 {
		t.Errorf("total_strokes = %d, want 1", got)
	}
	if got := completed.GetInt(collections.FieldTotalPar); got != 72 {
		t.Errorf("total_par = %d, want 72", got)
	}
	if got := completed.GetInt(collections.FieldRelativeToPar); got != 1-72 {
		t.Errorf("relative_to_par = %d, want %d", got, 1-72)
	}
	if completed.GetDateTime(collections.FieldFinishedAt).IsZero() {
		t.Error("finished_at is unset")
	}
	if got := completed.GetString(collections.FieldNote); got != "Great round" {
		t.Errorf("note = %q, want it trimmed to %q", got, "Great round")
	}
}

func TestCompleteRoundRejections(t *testing.T) {
	t.Run("twice", func(t *testing.T) {
		g := newGame(t)
		round := g.start(t)

		if _, err := rounds.Complete(g.app, g.user.Id, round.Id, ""); err != nil {
			t.Fatalf("complete round: %v", err)
		}

		_, err := rounds.Complete(g.app, g.user.Id, round.Id, "")
		wantAPIError(t, err, http.StatusConflict, "Round is already completed")
	})

	t.Run("another user's round", func(t *testing.T) {
		g := newGame(t)
		round := g.start(t)

		_, err := rounds.Complete(g.app, g.other.Id, round.Id, "")
		wantAPIError(t, err, http.StatusNotFound, "Round not found")
	})

	t.Run("note too long", func(t *testing.T) {
		g := newGame(t)
		round := g.start(t)

		_, err := rounds.Complete(g.app, g.user.Id, round.Id, strings.Repeat("x", 1001))
		wantAPIError(t, err, http.StatusBadRequest, "Note is too long")
	})
}

// TestCompletedRoundIsImmutable covers the rule at the level the custom routes
// cannot reach: a direct record save, which the round's owner is allowed to make
// through the generated PATCH endpoint.
func TestCompletedRoundIsImmutable(t *testing.T) {
	g := newGame(t)
	round := g.start(t)
	hole := g.hole(t, round, 1)

	if _, err := rounds.Complete(g.app, g.user.Id, round.Id, ""); err != nil {
		t.Fatalf("complete round: %v", err)
	}

	reloaded, err := g.app.FindRecordById(collections.NameRounds, round.Id)
	if err != nil {
		t.Fatalf("reload round: %v", err)
	}
	reloaded.Set(collections.FieldCurrentHole, 7)
	saveShouldFail(t, g.app, reloaded, collections.FieldStatus)

	// …and the shot operations report it as the conflict Django reports.
	_, err = shots.Add(g.app, g.user.Id, round.Id, 1, "Driver")
	wantAPIError(t, err, http.StatusConflict, "Round is already completed")

	_, err = shots.Undo(g.app, g.user.Id, round.Id, 1)
	wantAPIError(t, err, http.StatusConflict, "Round is already completed")

	_, err = rounds.SetCurrentHole(g.app, g.user.Id, round.Id, 2)
	wantAPIError(t, err, http.StatusConflict, "Round is already completed")

	// A shot written straight through the record API is refused too, so the
	// stored totals cannot drift from the shots behind them.
	stray := newRecord(t, g.app, collections.NameShots, "", map[string]any{
		"round_hole": hole.Id, "shot_number": 1, "club": "Driver",
	})
	saveShouldFail(t, g.app, stray, collections.FieldRoundHole)
}

// -----------------------------------------------------------------------------
// cancel_round
// -----------------------------------------------------------------------------

func TestCancelRoundRemovesEverything(t *testing.T) {
	g := newGame(t)
	round := g.start(t)
	mustAddShots(t, g, round, 1, "Driver")

	if err := rounds.Cancel(g.app, g.user.Id, round.Id); err != nil {
		t.Fatalf("cancel round: %v", err)
	}

	if _, err := g.app.FindRecordById(collections.NameRounds, round.Id); err == nil {
		t.Error("the round still exists after being cancelled")
	}
	for _, collection := range []string{collections.NameRoundHoles, collections.NameShots} {
		remaining, err := g.app.FindAllRecords(collection)
		if err != nil {
			t.Fatalf("list %s: %v", collection, err)
		}
		if len(remaining) != 0 {
			t.Errorf("%s still has %d record(s) after the round was cancelled", collection, len(remaining))
		}
	}
}

func TestCancelRoundRejections(t *testing.T) {
	g := newGame(t)
	round := g.start(t)

	wantAPIError(t, rounds.Cancel(g.app, g.other.Id, round.Id), http.StatusNotFound, "Round not found")
	wantAPIError(t, rounds.Cancel(g.app, g.user.Id, fixedID("nosuchround")), http.StatusNotFound, "Round not found")

	if _, err := rounds.Complete(g.app, g.user.Id, round.Id, ""); err != nil {
		t.Fatalf("complete round: %v", err)
	}
	wantAPIError(t, rounds.Cancel(g.app, g.user.Id, round.Id),
		http.StatusConflict, "Only in-progress rounds can be cancelled")
}

// mustAddShots plays a hole, in order.
func mustAddShots(t *testing.T, g *game, round *core.Record, holeNumber int, clubs ...string) {
	t.Helper()

	for _, club := range clubs {
		if _, err := shots.Add(g.app, g.user.Id, round.Id, holeNumber, club); err != nil {
			t.Fatalf("add %q to hole %d: %v", club, holeNumber, err)
		}
	}
}
