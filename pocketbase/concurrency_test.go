package main

import (
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/apierr"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/rounds"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/shots"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/records"
)

// Phase 3 (#124) concurrency suite, mirroring tests/test_concurrency.py.
//
// Django takes a row lock (select_for_update) on the RoundHole before computing
// the next shot_number. PocketBase has no equivalent, and does not need one: its
// write connection is serialised, so a read and a write inside one transaction
// cannot be split by another writer. The unique (round_hole, shot_number) index
// is the backstop that turns a lost race into a *rejected* write.
//
// What these assert, therefore, is not that both callers win but that the
// failure mode is a rejection: never two shots sharing a number, never a stroke
// cache out of step with the shots it counts.

// runConcurrently runs the callables in parallel and returns their errors in
// order. Nothing here touches *testing.T from a goroutine.
func runConcurrently(fns ...func() error) []error {
	errs := make([]error, len(fns))

	var wg sync.WaitGroup
	start := make(chan struct{})

	for i, fn := range fns {
		wg.Add(1)
		go func(i int, fn func() error) {
			defer wg.Done()
			<-start
			errs[i] = fn()
		}(i, fn)
	}

	close(start)
	wg.Wait()

	return errs
}

func TestConcurrentAddShotNeverDuplicatesAShotNumber(t *testing.T) {
	g := newGame(t)
	round := g.start(t)
	hole := g.hole(t, round, 1)

	errs := runConcurrently(
		func() error {
			_, err := shots.Add(g.app, g.user.Id, round.Id, 1, "Driver")
			return err
		},
		func() error {
			_, err := shots.Add(g.app, g.user.Id, round.Id, 1, "7i")
			return err
		},
	)

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		}
	}
	if succeeded == 0 {
		t.Fatalf("both concurrent add_shot calls failed: %v", errs)
	}

	strokes, numbers, _ := holeState(t, g.app, hole.Id)
	if len(numbers) != succeeded {
		t.Errorf("%d shot(s) stored after %d successful add(s): %v", len(numbers), succeeded, numbers)
	}
	if strokes != len(numbers) {
		t.Errorf("strokes = %d but the hole has %d shot(s) — the cache is out of step", strokes, len(numbers))
	}

	seen := map[int]bool{}
	for _, number := range numbers {
		if seen[number] {
			t.Fatalf("duplicate shot_number %d: %v", number, numbers)
		}
		seen[number] = true
	}
}

func TestConcurrentCreateRoundAllowsOnlyOne(t *testing.T) {
	g := newGame(t)

	errs := runConcurrently(
		func() error {
			_, err := rounds.Create(g.app, g.user.Id, g.course.Id, collections.PlayModeFull)
			return err
		},
		func() error {
			_, err := rounds.Create(g.app, g.user.Id, g.course.Id, collections.PlayModeFull)
			return err
		},
	)

	var succeeded int
	var failure error
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else {
			failure = err
		}
	}

	if succeeded != 1 {
		t.Fatalf("%d of 2 concurrent creates succeeded, want exactly 1: %v", succeeded, errs)
	}
	// The loser has to be Django's 409, not the 400 a raw index violation
	// would produce.
	wantAPIError(t, failure, http.StatusConflict, "A round is already in progress")

	all, err := g.app.FindAllRecords(collections.NameRounds)
	if err != nil {
		t.Fatalf("list rounds: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("%d rounds exist, want 1", len(all))
	}
}

func TestConcurrentDeleteOfTheSameShotAllowsOnlyOne(t *testing.T) {
	g := newGame(t)
	round := g.start(t)
	hole := g.hole(t, round, 1)
	mustAddShots(t, g, round, 1, "Driver", "7i")

	shotList, err := records.HoleShots(g.app, hole.Id)
	if err != nil {
		t.Fatalf("list shots: %v", err)
	}
	target := shotList[0].Id

	errs := runConcurrently(
		func() error {
			_, err := shots.Delete(g.app, g.user.Id, round.Id, 1, target)
			return err
		},
		func() error {
			_, err := shots.Delete(g.app, g.user.Id, round.Id, 1, target)
			return err
		},
	)

	var succeeded int
	var failure error
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else {
			failure = err
		}
	}

	if succeeded != 1 {
		t.Fatalf("%d of 2 concurrent deletes succeeded, want exactly 1: %v", succeeded, errs)
	}
	wantAPIError(t, failure, http.StatusNotFound, "Shot not found")

	strokes, numbers, clubs := holeState(t, g.app, hole.Id)
	if strokes != 1 || !equalInts(numbers, []int{1}) || clubs[0] != "7i" {
		t.Errorf("after the race: strokes=%d numbers=%v clubs=%v, want 1 [1] [7i]", strokes, numbers, clubs)
	}
}

// TestConcurrentCompleteAndAddShot mirrors the Django case: completing an
// in-progress round has no precondition a concurrent add can invalidate, so it
// always succeeds. The add either lands first or is refused as a conflict.
func TestConcurrentCompleteAndAddShot(t *testing.T) {
	g := newGame(t)
	round := g.start(t)
	hole := g.hole(t, round, 1)

	errs := runConcurrently(
		func() error {
			_, err := rounds.Complete(g.app, g.user.Id, round.Id, "Done", nil, nil)
			return err
		},
		func() error {
			_, err := shots.Add(g.app, g.user.Id, round.Id, 1, "Driver")
			return err
		},
	)

	if errs[0] != nil {
		t.Fatalf("complete should always succeed: %v", errs[0])
	}
	if errs[1] != nil {
		var apiErr *apierr.Error
		if !errors.As(errs[1], &apiErr) || apiErr.Status != http.StatusConflict {
			t.Errorf("the losing add_shot = %v, want a 409 conflict", errs[1])
		}
	}

	final, err := g.app.FindRecordById(collections.NameRounds, round.Id)
	if err != nil {
		t.Fatalf("reload round: %v", err)
	}
	if got := final.GetString(collections.FieldStatus); got != collections.RoundStatusCompleted {
		t.Errorf("status = %q, want %q", got, collections.RoundStatusCompleted)
	}

	// Whichever way the race went, the totals stored on the round have to match
	// the shots that are actually on it.
	strokes, numbers, _ := holeState(t, g.app, hole.Id)
	if strokes != len(numbers) {
		t.Errorf("strokes = %d but the hole has %d shot(s)", strokes, len(numbers))
	}
	if errs[1] == nil && final.GetInt(collections.FieldTotalStrokes) != strokes {
		t.Errorf("total_strokes = %d but hole 1 holds %d stroke(s) — the add landed after the totals were summed",
			final.GetInt(collections.FieldTotalStrokes), strokes)
	}
}
