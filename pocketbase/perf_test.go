package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/web"
)

// Phase 8 (#129) performance suite.
//
// The question this file answers is not "how fast is it" — bench_test.go and
// loadtest_test.go measure that, and both numbers move with the machine. It is
// the one that does not move: **how many queries does a request issue, and does
// that number grow with the data**. An N+1 is invisible in a benchmark taken
// against a fixture with one course in it and fatal against a season of rounds,
// so the assertions here are counts, taken at two data sizes.
//
// A response built one record at a time is the natural shape for the read paths
// in internal/hooks/domain — `for _, round := range rounds { NewOut(round) }`
// reads exactly like the Django it was ported from, where the ORM's
// select_related hid the cost. It does not hide here, which is why these bounds
// are asserted rather than assumed.

// -----------------------------------------------------------------------------
// counting queries
// -----------------------------------------------------------------------------

// dbCounts is what one request cost the database: statements, split by kind
// because a read path issuing a write is itself worth noticing.
type dbCounts struct {
	queries int
	execs   int
}

func (c dbCounts) total() int { return c.queries + c.execs }

func (c dbCounts) String() string {
	return fmt.Sprintf("%d statements (%d queries, %d execs)", c.total(), c.queries, c.execs)
}

// countStatements runs fn with the app's statement loggers replaced by counters
// and returns what fn cost.
//
// PocketBase installs its own loggers on both pools when logging is enabled, so
// they are saved and restored rather than simply set — a test that swallowed
// them would silently change the behaviour of every later test on the same app.
func countStatements(tb testing.TB, app core.App, fn func()) dbCounts {
	tb.Helper()

	var (
		mu     sync.Mutex
		counts dbCounts
	)

	query := func(context.Context, time.Duration, string, *sql.Rows, error) {
		mu.Lock()
		defer mu.Unlock()
		counts.queries++
	}
	exec := func(context.Context, time.Duration, string, sql.Result, error) {
		mu.Lock()
		defer mu.Unlock()
		counts.execs++
	}

	// Both pools: reads go to the concurrent one, writes and everything inside
	// a transaction to the nonconcurrent one.
	for _, db := range []dbx.Builder{app.ConcurrentDB(), app.NonconcurrentDB()} {
		pool, ok := db.(*dbx.DB)
		if !ok {
			tb.Fatalf("db pool is %T, want *dbx.DB", db)
		}

		priorQuery, priorExec := pool.QueryLogFunc, pool.ExecLogFunc
		tb.Cleanup(func() { pool.QueryLogFunc, pool.ExecLogFunc = priorQuery, priorExec })

		pool.QueryLogFunc = query
		pool.ExecLogFunc = exec
	}

	fn()

	mu.Lock()
	defer mu.Unlock()
	return counts
}

// countRequest is countStatements around a single request, failing on a non-2xx
// so a count can never be the cheap cost of an error path.
func countRequest(tb testing.TB, app core.App, mux http.Handler, method, url string, headers map[string]string) dbCounts {
	tb.Helper()

	return countStatements(tb, app, func() {
		req := httptest.NewRequest(method, url, nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code < 200 || rec.Code > 299 {
			tb.Fatalf("%s %s = %d: %s", method, url, rec.Code, rec.Body.String())
		}
	})
}

// -----------------------------------------------------------------------------
// a fixture that scales
// -----------------------------------------------------------------------------

// perfFixture is a player with a history: `rounds` completed rounds on 18-hole
// courses, plus one in-progress round with every hole snapshotted and `shots`
// shots on each of them.
//
// Everything is parameterised because the assertions are comparative — the same
// request is counted against a small fixture and a large one, and an N+1 is the
// difference between the two.
type perfFixture struct {
	app    *tests.TestApp
	player *core.Record
	round  *core.Record // the in-progress one
	course *core.Record
	mux    http.Handler
}

type perfScale struct {
	courses   int // courses in the list
	completed int // completed rounds in the player's history
	holes     int // holes per round
	shots     int // shots on each hole of the in-progress round
}

// smallScale and largeScale differ in every dimension a per-record read would
// multiply by. If a request's statement count is the same at both, nothing in it
// loops over the database.
var (
	smallScale = perfScale{courses: 1, completed: 1, holes: 9, shots: 1}
	largeScale = perfScale{courses: 6, completed: 12, holes: 18, shots: 5}
)

func newPerfFixture(tb testing.TB, scale perfScale) *perfFixture {
	tb.Helper()

	app := newTestApp(tb)
	web.Register(app)

	f := &perfFixture{app: app}
	f.player = createUser(tb, app, idOwner, "owner@golftrack.test", collections.UserRoleUser)

	for n := range scale.courses {
		course := seedCourse(tb, app, "", fmt.Sprintf("Course %02d", n), scale.holes, 4)
		if f.course == nil {
			f.course = course
		}
	}

	// The history. Totals are written at creation the way rounds.Complete
	// leaves them — a completed round is immutable, so they cannot be filled in
	// afterwards.
	for n := range scale.completed {
		finished := time.Now().UTC().Add(-time.Duration(n) * time.Hour)
		done := saveAs(tb, app, newRecord(tb, app, collections.NameRounds, "", map[string]any{
			"user":            f.player.Id,
			"course":          f.course.Id,
			"status":          collections.RoundStatusCompleted,
			"play_mode":       collections.PlayModeFull,
			"started_at":      finished.Add(-3 * time.Hour),
			"finished_at":     finished,
			"current_hole":    scale.holes,
			"total_strokes":   4 * scale.holes,
			"total_par":       4 * scale.holes,
			"relative_to_par": 0,
		}))
		for number := 1; number <= scale.holes; number++ {
			createRoundHole(tb, app, "", done, number, 4)
		}
	}

	// The round being played.
	f.round = createRound(tb, app, "", f.player, f.course, collections.RoundStatusInProgress)
	for number := 1; number <= scale.holes; number++ {
		hole := createRoundHole(tb, app, "", f.round, number, 4)
		for shot := 1; shot <= scale.shots; shot++ {
			createShot(tb, app, "", hole, shot, "7 Iron")
		}
	}

	f.mux = newMux(tb, app)

	return f
}

func (f *perfFixture) auth(tb testing.TB) map[string]string { return authHeader(tb, f.player) }

func (f *perfFixture) cookie(tb testing.TB) map[string]string { return authCookie(tb, f.player) }

// -----------------------------------------------------------------------------
// the gate: a request's cost does not grow with the data
// -----------------------------------------------------------------------------

// TestReadPathsDoNotQueryPerRecord is the "queries efficient" half of the Phase 8
// gate, and the regression test for what this phase fixed.
//
// Each route is counted twice: once against a 9-hole round with one shot per
// hole and a single completed round, once against an 18-hole round with five
// shots per hole and twelve completed rounds. The counts must be *identical* —
// not merely close — because every read path now issues a fixed set of queries
// and expands the rows in Go.
//
// Before this phase the round-detail route was 2 statements per hole plus 2 per
// round; the large fixture cost 40 statements to the small one's 22.
func TestReadPathsDoNotQueryPerRecord(t *testing.T) {
	t.Parallel()
	small := newPerfFixture(t, smallScale)
	large := newPerfFixture(t, largeScale)

	for _, tc := range []struct {
		name  string
		url   func(*perfFixture) string
		limit int // the fixed statement count, asserted as an upper bound too
	}{
		{"round detail", func(f *perfFixture) string { return "/api/rounds/" + f.round.Id }, 6},
		{"in-progress round", func(*perfFixture) string { return "/api/rounds/in-progress" }, 6},
		{"completed rounds", func(*perfFixture) string { return "/api/rounds" }, 4},
		{"course list", func(*perfFixture) string { return "/api/courses" }, 3},
		{"course detail", func(f *perfFixture) string { return "/api/courses/" + f.course.Id }, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			atSmall := countRequest(t, small.app, small.mux, http.MethodGet, tc.url(small), small.auth(t))
			atLarge := countRequest(t, large.app, large.mux, http.MethodGet, tc.url(large), large.auth(t))
			t.Logf("%v", atLarge)

			if atSmall.total() != atLarge.total() {
				t.Errorf("statements grow with the data: %v at %+v, %v at %+v",
					atSmall, smallScale, atLarge, largeScale)
			}
			if atLarge.total() > tc.limit {
				t.Errorf("%v, want at most %d — a query was added to this path", atLarge, tc.limit)
			}
		})
	}
}

// TestPagesDoNotQueryPerRecord is the same assertion for the server-rendered
// pages, which read through the same domain functions but are reached with a
// cookie rather than a bearer token. They are counted separately because a page
// renders more than one query's worth of data — the play page needs the round,
// its holes and the club list in one response — so a regression could land on
// the page path alone.
func TestPagesDoNotQueryPerRecord(t *testing.T) {
	t.Parallel()
	small := newPerfFixture(t, smallScale)
	large := newPerfFixture(t, largeScale)

	for _, tc := range []struct {
		name  string
		url   func(*perfFixture) string
		limit int
	}{
		// Home and the round list render two answers — the round in progress
		// and the completed history — so they cost the sum of the two API
		// routes behind them, less the auth lookup they share.
		{"home", func(*perfFixture) string { return "/" }, 9},
		{"course list", func(*perfFixture) string { return "/courses/" }, 3},
		{"course detail", func(f *perfFixture) string { return "/courses/" + f.course.Id + "/" }, 3},
		{"round list", func(*perfFixture) string { return "/rounds/" }, 9},
		{"round play", func(f *perfFixture) string { return "/rounds/" + f.round.Id + "/play/" }, 6},
		{"round review", func(f *perfFixture) string { return "/rounds/" + f.round.Id + "/review/" }, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			atSmall := countRequest(t, small.app, small.mux, http.MethodGet, tc.url(small), small.cookie(t))
			atLarge := countRequest(t, large.app, large.mux, http.MethodGet, tc.url(large), large.cookie(t))
			t.Logf("%v", atLarge)

			if atSmall.total() != atLarge.total() {
				t.Errorf("statements grow with the data: %v at %+v, %v at %+v",
					atSmall, smallScale, atLarge, largeScale)
			}
			if atLarge.total() > tc.limit {
				t.Errorf("%v, want at most %d — a query was added to this path", atLarge, tc.limit)
			}
		})
	}
}

// TestAddShotCostIsFlat covers the write the play screen makes on every tap. It
// is the one hot path that has to be a transaction, so its statement count
// carries BEGIN/COMMIT and the stroke-cache refresh; what it must not carry is
// anything proportional to the shots already on the hole.
//
// Counted as the fifth and the twenty-fifth shot on the same hole, because the
// route returns the hole with all of its shots — a per-shot lookup in that
// response would only show up once the hole is deep.
func TestAddShotCostIsFlat(t *testing.T) {
	t.Parallel()
	f := newPerfFixture(t, largeScale)
	headers := f.auth(t)

	url := "/api/rounds/" + f.round.Id + "/holes/1/shots"

	addShot := func() dbCounts {
		return countStatements(t, f.app, func() {
			req := httptest.NewRequest(http.MethodPost, url, strings.NewReader(`{"club":"Driver"}`))
			req.Header.Set("content-type", "application/json")
			for k, v := range headers {
				req.Header.Set(k, v)
			}

			rec := httptest.NewRecorder()
			f.mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
				t.Fatalf("POST %s = %d: %s", url, rec.Code, rec.Body.String())
			}
		})
	}

	early := addShot()
	for range 19 {
		addShot()
	}
	late := addShot()
	t.Logf("%v", late)

	if early.total() != late.total() {
		t.Errorf("add shot costs %v on a shallow hole and %v on a deep one", early, late)
	}
}

// -----------------------------------------------------------------------------
// indexing
// -----------------------------------------------------------------------------

// TestHotQueriesUseAnIndex is the "review database indexing" task, expressed as
// an assertion rather than a note: SQLite is asked how it would run each query
// the read paths issue, and a plan that scans a table fails.
//
// `SCAN` in a query plan means every row is read. It is acceptable on a table
// that is small by construction and never grows — none of these are — so any
// occurrence here is a missing index.
func TestHotQueriesUseAnIndex(t *testing.T) {
	t.Parallel()
	f := newPerfFixture(t, largeScale)

	for _, tc := range []struct {
		name  string
		query string
		args  dbx.Params
	}{
		{
			"the player's completed rounds, newest first",
			`SELECT * FROM {{` + collections.NameRounds + `}}
			 WHERE [[user]] = {:user} AND [[status]] = {:status}
			 ORDER BY [[finished_at]] DESC, [[started_at]] DESC LIMIT 20`,
			dbx.Params{"user": f.player.Id, "status": collections.RoundStatusCompleted},
		},
		{
			"the player's in-progress round",
			`SELECT * FROM {{` + collections.NameRounds + `}}
			 WHERE [[user]] = {:user} AND [[status]] = {:status}`,
			dbx.Params{"user": f.player.Id, "status": collections.RoundStatusInProgress},
		},
		{
			"a round's holes",
			`SELECT * FROM {{` + collections.NameRoundHoles + `}}
			 WHERE [[round]] = {:round} ORDER BY [[hole_number]] ASC`,
			dbx.Params{"round": f.round.Id},
		},
		{
			"a course's holes",
			`SELECT * FROM {{` + collections.NameCourseHoles + `}}
			 WHERE [[course]] = {:course} ORDER BY [[hole_number]] ASC`,
			dbx.Params{"course": f.course.Id},
		},
		{
			"a hole's shots",
			`SELECT * FROM {{` + collections.NameShots + `}}
			 WHERE [[round_hole]] = {:hole} ORDER BY [[shot_number]] ASC`,
			dbx.Params{"hole": "anything"},
		},
		{
			"the courses list, by name",
			`SELECT * FROM {{` + collections.NameCourses + `}}
			 WHERE [[is_archived]] = false ORDER BY [[name]] ASC`,
			nil,
		},
		// The batched forms the read paths actually issue (Phase 8). An IN over
		// a set has to reach the same index the single-id form does, or the
		// batching would have traded N cheap queries for one expensive scan.
		{
			"several courses by id",
			`SELECT * FROM {{` + collections.NameCourses + `}} WHERE [[id]] IN ({:a}, {:b})`,
			dbx.Params{"a": "a", "b": "b"},
		},
		{
			"the holes of several courses",
			`SELECT * FROM {{` + collections.NameCourseHoles + `}}
			 WHERE [[course]] IN ({:a}, {:b}) ORDER BY [[course]] ASC, [[hole_number]] ASC`,
			dbx.Params{"a": "a", "b": "b"},
		},
		{
			"the shots of several holes",
			`SELECT * FROM {{` + collections.NameShots + `}}
			 WHERE [[round_hole]] IN ({:a}, {:b}) ORDER BY [[round_hole]] ASC, [[shot_number]] ASC`,
			dbx.Params{"a": "a", "b": "b"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := explain(t, f.app, tc.query, tc.args)

			if strings.Contains(plan, "SCAN") {
				t.Errorf("query plan scans a table:\n%s", plan)
			}
			if plan == "" {
				t.Fatal("empty query plan")
			}
			t.Logf("plan:\n%s", plan)
		})
	}
}

// -----------------------------------------------------------------------------
// memory and goroutines
// -----------------------------------------------------------------------------

// TestSustainedTrafficDoesNotLeak is the "no memory leaks" gate item.
//
// A leak in a request handler is a per-request cost that is never given back:
// the heap after ten thousand requests is larger than the heap after one
// thousand, by a margin that keeps growing. So the shape of the test is two
// measurements of the same workload rather than one — a first batch to let
// everything that is allocated *once* be allocated (the connection pool, the
// collection cache, the template set, SQLite's page cache), then a second,
// larger batch measured against the settled heap.
//
// The threshold is deliberately generous. Go's heap is not a straight line even
// under a perfectly clean workload — the GC's target grows with live data and
// the allocator holds spans back — so a strict bound here would be a flaky test
// that taught the reader nothing. What it catches is the shape a real leak has:
// retained bytes proportional to requests served.
func TestSustainedTrafficDoesNotLeak(t *testing.T) {
	// Not t.Parallel(): runtime.NumGoroutine() and the heap stats below are
	// process-wide, so a sibling test allocating or spawning goroutines during
	// this one's measurement window would produce a false failure.
	f := newLoadFixture(t, 4)
	workload := append(readWorkload(), writeWorkload()...)

	drive := func(rounds int) {
		for i := range rounds {
			for _, player := range f.players {
				for _, s := range workload {
					f.request(t, s, player, i)
				}
			}
		}
	}

	// Warm-up, then the settled baseline.
	drive(20)
	settled := heapInUse()
	goroutinesBefore := runtime.NumGoroutine()

	// Five times the warm-up. A per-request leak would show up five times over.
	drive(100)
	after := heapInUse()
	goroutinesAfter := runtime.NumGoroutine()

	requests := 100 * len(f.players) * len(workload)
	growth := int64(after) - int64(settled)

	t.Logf("heap in use: %.1f MiB settled, %.1f MiB after %d further requests (%+.1f MiB, %+d bytes/request)",
		mib(settled), mib(after), requests, float64(growth)/(1<<20), growth/int64(requests))

	// 8 MiB across ~10k requests is under a kilobyte each — far below anything
	// that accumulates, and far above ordinary allocator drift.
	const budget = 8 << 20
	if growth > budget {
		t.Errorf("heap grew %.1f MiB over %d requests, want under %d MiB — that is retention per request, not drift",
			float64(growth)/(1<<20), requests, budget>>20)
	}

	// Goroutines are the other thing a handler can leak, and they are exact:
	// nothing in a request should outlive it. A couple of slots of tolerance
	// covers PocketBase's own background workers starting lazily.
	if goroutinesAfter > goroutinesBefore+4 {
		t.Errorf("goroutines went from %d to %d over %d requests — a handler is leaving one behind",
			goroutinesBefore, goroutinesAfter, requests)
	}
}

// heapInUse returns the live heap after a settling GC. Two collections rather
// than one: the first runs finalizers, the second reclaims what they released.
func heapInUse() uint64 {
	runtime.GC()
	runtime.GC()

	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapInuse
}

func mib(bytes uint64) float64 { return float64(bytes) / (1 << 20) }

// explain returns SQLite's query plan for a statement, one step per line.
func explain(tb testing.TB, app core.App, query string, args dbx.Params) string {
	tb.Helper()

	var rows []struct {
		Detail string `db:"detail"`
	}

	// The plan is asked of the statement as PocketBase would issue it, so the
	// {{table}} / [[column]] quoting is expanded by dbx first.
	q := app.ConcurrentDB().NewQuery("EXPLAIN QUERY PLAN " + query)
	if args != nil {
		q.Bind(args)
	}
	if err := q.All(&rows); err != nil {
		tb.Fatalf("explain query plan: %v", err)
	}

	steps := make([]string, 0, len(rows))
	for _, row := range rows {
		steps = append(steps, "  "+row.Detail)
	}
	return strings.Join(steps, "\n")
}
