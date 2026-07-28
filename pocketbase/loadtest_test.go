package main

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/tests"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/web"
)

// Phase 8 (#129) load test: the app under 10, 50 and 100 concurrent players.
//
// This is the gate item benchmarks cannot answer. A benchmark runs one request
// at a time and tells you the cost of the work; what a concurrency level tells
// you is the cost of the *queue*. GolfTrack is SQLite, so writes serialise on a
// single writer no matter how many players tap at once — the interesting number
// is not whether a shot insert is fast (it is) but what the hundredth
// simultaneous one waits behind.
//
// Every player gets their own round. Sharing one would measure the unique index
// on (round_hole, shot_number) rejecting concurrent writers, which is a real
// property but is concurrency_test.go's, not this file's: two people do not
// normally play the same ball.
//
// # Running it
//
//	cd pocketbase && GOLFTRACK_LOADTEST=1 go test -run TestLoad -v -timeout 20m
//	make pb-loadtest        # the same, from the repository root
//
// It is gated on the environment variable rather than left to run with the
// suite because seeding a hundred players with a round each takes longer than
// every other test in this directory put together, and because a latency
// assertion on a shared CI runner measures the runner's neighbours as much as
// the code. `make pb-test` therefore does not run it; the numbers it produced on
// a known machine are in performance_report.md.

// loadLevels are the concurrency levels #129 asks for.
var loadLevels = []int{10, 50, 100}

// pace describes how a level is driven.
//
// Zero think time is the stress rig: every player always has a request
// outstanding, which finds the ceiling. A non-zero think time — with the jitter,
// which matters more than it looks — models arrivals instead. Without jitter a
// hundred players started together stay in lockstep and arrive as a herd once
// per think time, so the measurement is of a burst rather than of a rate.
type pace struct {
	duration  time.Duration
	thinkTime time.Duration
}

// saturating is the sweep's driver: as much load as the players can generate.
// Three seconds is long enough that a level's slowest requests are not simply
// its first (the connection pool and SQLite's page cache both warm up) and short
// enough that the whole sweep is a minute.
var saturating = pace{duration: 3 * time.Second}

// strolling is a load a golf app can actually receive — see
// TestLoadAtRealisticPace.
var strolling = pace{duration: 12 * time.Second, thinkTime: 3 * time.Second}

// p95Budget is the Phase 8 gate: "response times acceptable (<200ms p95 for most
// endpoints)". It is asserted per endpoint against the in-process handler — so
// it is a budget for the application's own work, with the network still to be
// paid on top by a real client.
const p95Budget = 200 * time.Millisecond

// writeFloor is the write throughput the app must sustain, in requests per
// second, at every concurrency level.
//
// Writes are asserted on throughput where reads are asserted on latency, and the
// reason is the measurement rather than a lowered standard. PocketBase gives
// writes a one-connection pool over a single SQLite writer, so write throughput
// saturates — and it is already saturated at ten zero-think-time players, which
// is why the sweep's write numbers are flat across all three levels. Once
// throughput is flat, latency under a closed-loop driver is just Little's law on
// the number of goroutines the test happens to start: 100 players ÷ 288 req/s is
// the 347 ms it measures, and would be 3.5 s at a thousand. Asserting that would
// pin the driver's shape, not the app's.
//
// So: throughput here, and latency in TestLoadAtRealisticPace, which offers a
// load a golf app can actually receive.
const writeFloor = 150.0

// -----------------------------------------------------------------------------
// the fixture: one course, many players, a round each
// -----------------------------------------------------------------------------

// loadPlayer is one virtual user: their round, and the header that authenticates
// them against it.
type loadPlayer struct {
	roundID string
	headers map[string]string
}

// loadFixture seeds `players` players, each with an in-progress 18-hole round on
// a shared course — the shape of a busy Saturday morning at one club, which is
// also the worst case for contention: every read hits the same course rows.
type loadFixture struct {
	app     *tests.TestApp
	mux     http.Handler
	players []loadPlayer
}

func newLoadFixture(tb testing.TB, players int) *loadFixture {
	tb.Helper()

	app := newTestApp(tb)
	web.Register(app)

	course := seedCourse(tb, app, "", "Load Links", 18, 4)

	f := &loadFixture{app: app, mux: newMux(tb, app), players: make([]loadPlayer, 0, players)}

	for n := range players {
		player := createUser(tb, app, "", fmt.Sprintf("player%03d@golftrack.test", n), collections.UserRoleUser)

		// A completed round each, so the list endpoint has something to return
		// and the history join is exercised rather than short-circuited.
		done := saveAs(tb, app, newRecord(tb, app, collections.NameRounds, "", map[string]any{
			"user":            player.Id,
			"course":          course.Id,
			"status":          collections.RoundStatusCompleted,
			"play_mode":       collections.PlayModeFull,
			"started_at":      time.Now().UTC().Add(-4 * time.Hour),
			"finished_at":     time.Now().UTC().Add(-time.Hour),
			"current_hole":    18,
			"total_strokes":   76,
			"total_par":       72,
			"relative_to_par": 4,
		}))
		createRoundHole(tb, app, "", done, 1, 4)

		round := createRound(tb, app, "", player, course, collections.RoundStatusInProgress)
		for number := 1; number <= 18; number++ {
			createRoundHole(tb, app, "", round, number, 4)
		}

		f.players = append(f.players, loadPlayer{roundID: round.Id, headers: authHeader(tb, player)})
	}

	return f
}

// -----------------------------------------------------------------------------
// the driver
// -----------------------------------------------------------------------------

// step is one request in a virtual user's loop, built per player because every
// URL in this app is scoped to the caller's own round.
type step struct {
	name    string
	method  string
	url     func(loadPlayer) string
	payload func(int) string // nil for a read; the int is the iteration
}

// result is one endpoint's latency distribution at one concurrency level.
type result struct {
	name     string
	latency  []time.Duration
	failures int
	over     time.Duration // the wall time the samples were taken over
}

func (r *result) percentile(p float64) time.Duration {
	if len(r.latency) == 0 {
		return 0
	}
	// Nearest-rank, on an already-sorted slice.
	rank := int(p / 100 * float64(len(r.latency)))
	return r.latency[min(rank, len(r.latency)-1)]
}

func (r *result) mean() time.Duration {
	if len(r.latency) == 0 {
		return 0
	}
	var total time.Duration
	for _, d := range r.latency {
		total += d
	}
	return total / time.Duration(len(r.latency))
}

func (r *result) throughput() float64 {
	return float64(len(r.latency)) / r.over.Seconds()
}

// runLoad drives every player through the workload concurrently for the pace's
// duration and returns one result per step.
func runLoad(tb testing.TB, f *loadFixture, workload []step, p pace) map[string]*result {
	tb.Helper()

	var (
		mu      sync.Mutex
		results = make(map[string]*result, len(workload))
	)
	for _, s := range workload {
		results[s.name] = &result{name: s.name, over: p.duration}
	}

	deadline := time.Now().Add(p.duration)

	// Deterministic, so a run is reproducible, but uncorrelated across players
	// — which is the whole point of jittering.
	random := rand.New(rand.NewPCG(1, 2))
	var randomMu sync.Mutex
	jitter := func() time.Duration {
		if p.thinkTime == 0 {
			return 0
		}
		randomMu.Lock()
		defer randomMu.Unlock()
		// Uniform over [0.5, 1.5) of the think time.
		return p.thinkTime/2 + time.Duration(random.Int64N(int64(p.thinkTime)))
	}

	var wg sync.WaitGroup
	for _, player := range f.players {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Spread the starts over one think time so the players do not
			// arrive as a herd, and stay spread because each pause is jittered.
			if p.thinkTime > 0 {
				time.Sleep(jitter())
			}

			// Latencies are accumulated per goroutine and merged once, so the
			// shared mutex is not itself part of what is being measured.
			local := make(map[string][]time.Duration, len(workload))
			failed := make(map[string]int, len(workload))

			for iteration := 0; time.Now().Before(deadline); iteration++ {
				for _, s := range workload {
					var body *strings.Reader
					if s.payload != nil {
						body = strings.NewReader(s.payload(iteration))
					} else {
						body = strings.NewReader("")
					}

					req := httptest.NewRequest(s.method, s.url(player), body)
					req.Header.Set("content-type", "application/json")
					for k, v := range player.headers {
						req.Header.Set(k, v)
					}

					rec := httptest.NewRecorder()
					start := time.Now()
					f.mux.ServeHTTP(rec, req)
					elapsed := time.Since(start)

					local[s.name] = append(local[s.name], elapsed)
					if rec.Code < 200 || rec.Code > 299 {
						failed[s.name]++
					}
				}

				if p.thinkTime > 0 {
					time.Sleep(jitter())
				}
			}

			mu.Lock()
			defer mu.Unlock()
			for name, samples := range local {
				results[name].latency = append(results[name].latency, samples...)
				results[name].failures += failed[name]
			}
		}()
	}
	wg.Wait()

	for _, r := range results {
		sort.Slice(r.latency, func(i, j int) bool { return r.latency[i] < r.latency[j] })
	}

	return results
}

// report prints one level's table and returns nothing — the assertions are the
// caller's, because a read and a write do not deserve the same verdict.
func report(tb testing.TB, level int, p pace, results map[string]*result, workload []step) {
	tb.Helper()

	var b strings.Builder
	fmt.Fprintf(&b, "\n%d concurrent players, %s, think time %s\n", level, p.duration, p.thinkTime)
	fmt.Fprintf(&b, "%-28s %8s %8s %9s %9s %9s %9s %7s\n",
		"endpoint", "reqs", "rps", "mean", "p50", "p95", "p99", "errors")

	for _, s := range workload {
		r := results[s.name]
		fmt.Fprintf(&b, "%-28s %8d %8.0f %9s %9s %9s %9s %7d\n",
			s.name,
			len(r.latency),
			r.throughput(),
			round3(r.mean()),
			round3(r.percentile(50)),
			round3(r.percentile(95)),
			round3(r.percentile(99)),
			r.failures,
		)
	}

	tb.Log(b.String())
}

// round3 trims a duration to three significant figures so a table of them lines
// up and reads as measurements rather than as nanosecond noise.
func round3(d time.Duration) time.Duration {
	switch {
	case d > time.Second:
		return d.Round(time.Millisecond)
	case d > time.Millisecond:
		return d.Round(10 * time.Microsecond)
	default:
		return d.Round(time.Microsecond)
	}
}

// -----------------------------------------------------------------------------
// the workloads
// -----------------------------------------------------------------------------

// readWorkload is what a player's device asks for while they walk to the ball:
// the home page, the round they are playing, and their history. All of it goes
// through the concurrent connection pool, so it should scale with cores rather
// than serialise.
func readWorkload() []step {
	return []step{
		{"GET /api/rounds/in-progress", http.MethodGet,
			func(loadPlayer) string { return "/api/rounds/in-progress" }, nil},
		{"GET /api/rounds/{id}", http.MethodGet,
			func(p loadPlayer) string { return "/api/rounds/" + p.roundID }, nil},
		{"GET /api/rounds", http.MethodGet,
			func(loadPlayer) string { return "/api/rounds" }, nil},
		{"GET /api/courses", http.MethodGet,
			func(loadPlayer) string { return "/api/courses" }, nil},
		{"GET / (page)", http.MethodGet,
			func(loadPlayer) string { return "/" }, nil},
	}
}

// writeWorkload is the play screen: a shot, then the hole navigation. Both are
// transactions on the single writer, which is what makes this the level that
// decides whether SQLite is enough.
//
// The shot is undone on the next iteration rather than accumulating, so a hole
// does not grow to thousands of shots and start timing a response body instead
// of a write.
func writeWorkload() []step {
	return []step{
		{"POST …/holes/1/shots", http.MethodPost,
			func(p loadPlayer) string { return "/api/rounds/" + p.roundID + "/holes/1/shots" },
			func(int) string { return `{"club":"Driver"}` }},
		{"POST …/holes/1/undo", http.MethodPost,
			func(p loadPlayer) string { return "/api/rounds/" + p.roundID + "/holes/1/undo" },
			func(int) string { return "" }},
		{"PATCH …/current-hole", http.MethodPatch,
			func(p loadPlayer) string { return "/api/rounds/" + p.roundID + "/current-hole" },
			func(i int) string { return fmt.Sprintf(`{"currentHole":%d}`, i%18+1) }},
	}
}

// -----------------------------------------------------------------------------
// the tests
// -----------------------------------------------------------------------------

func requireLoadTest(t *testing.T) {
	t.Helper()

	if os.Getenv("GOLFTRACK_LOADTEST") == "" {
		t.Skip("set GOLFTRACK_LOADTEST=1 to run the load sweep (see the file header)")
	}
}

// noFailures is the assertion every level owes regardless of what else it
// promises: SQLite under contention fails by *rejecting* writes (SQLITE_BUSY, a
// unique-index collision), so a clean run is the evidence that the queueing
// below is queueing and not loss.
func noFailures(t *testing.T, results map[string]*result, workload []step) {
	t.Helper()

	for _, s := range workload {
		if r := results[s.name]; r.failures > 0 {
			t.Errorf("%s: %d of %d requests failed", s.name, r.failures, len(r.latency))
		}
	}
}

// TestLoadReads sweeps the read mix across the three concurrency levels and
// holds every endpoint to the p95 budget at each one.
//
// Reads get the latency assertion because they do not queue: they run on a
// 120-connection pool against WAL, so concurrency buys parallelism rather than a
// waiting line, and p95 measures the app.
func TestLoadReads(t *testing.T) {
	requireLoadTest(t)

	workload := readWorkload()

	for _, level := range loadLevels {
		t.Run(fmt.Sprintf("%d_players", level), func(t *testing.T) {
			f := newLoadFixture(t, level)
			results := runLoad(t, f, workload, saturating)
			report(t, level, saturating, results, workload)
			noFailures(t, results, workload)

			for _, s := range workload {
				if p95 := results[s.name].percentile(95); p95 > p95Budget {
					t.Errorf("%s: p95 %s over the %s budget", s.name, round3(p95), p95Budget)
				}
			}
		})
	}
}

// TestLoadWrites drives the writes to saturation and asserts the ceiling it
// finds, not the latency at it — see writeFloor for why that is the honest way
// round.
//
// What this catches is the regression that matters on a serialised writer:
// anything added inside a write transaction comes straight off this number,
// because every statement in it is time the next player is queued behind.
//
// The Phase 8 work moved it by about a tenth — dropping a redundant round
// lookup and replacing two full-shot-list reads with a MAX() and a COUNT() —
// which is worth having and is not the headline. The write path's remaining
// cost is the record hooks that re-read the hole and its round to enforce
// "a completed round cannot be scored" on *every* write path rather than only
// on this one, and that is a correctness property, not fat. See
// performance_report.md § "What the write path still spends".
func TestLoadWrites(t *testing.T) {
	requireLoadTest(t)

	workload := writeWorkload()

	for _, level := range loadLevels {
		t.Run(fmt.Sprintf("%d_players", level), func(t *testing.T) {
			f := newLoadFixture(t, level)
			results := runLoad(t, f, workload, saturating)
			report(t, level, saturating, results, workload)
			noFailures(t, results, workload)

			for _, s := range workload {
				if rps := results[s.name].throughput(); rps < writeFloor {
					t.Errorf("%s: %.0f req/s, under the %.0f req/s floor — something was added to a write transaction",
						s.name, rps, writeFloor)
				}
			}
		})
	}
}

// TestLoadMixed runs reads and writes together, which neither of the other two
// does: some players are scoring while others are looking at yesterday's card.
// It exists because the two halves interact — WAL means a reader never waits on
// a writer for the *database*, but both compete for the same cores, and the
// question is whether saturating the writer degrades reads.
//
// It answers that in throughput, not latency, and for both halves. At write
// saturation the machine is busy end to end, so a read's p95 here measures how
// hard the driver is pushing rather than what a read costs — the same objection
// writeFloor makes, arriving at the reads by a different route. What the read
// throughput floor does catch is the failure worth catching: reads collapsing
// because writes are hogging the box. TestLoadReads holds read latency where it
// is meaningful, and TestLoadAtRealisticPace holds both.
func TestLoadMixed(t *testing.T) {
	requireLoadTest(t)

	reads, writes := readWorkload(), writeWorkload()
	workload := append(append([]step{}, reads...), writes...)

	for _, level := range loadLevels {
		t.Run(fmt.Sprintf("%d_players", level), func(t *testing.T) {
			f := newLoadFixture(t, level)
			results := runLoad(t, f, workload, saturating)
			report(t, level, saturating, results, workload)
			noFailures(t, results, workload)

			for _, s := range workload {
				if rps := results[s.name].throughput(); rps < writeFloor {
					t.Errorf("%s: %.0f req/s, under the %.0f req/s floor", s.name, rps, writeFloor)
				}
			}
		})
	}
}

// TestLoadAtRealisticPace is the one that answers the gate question for writes:
// at an offered load this app can actually receive, is p95 under 200 ms?
//
// The other three tests drive closed-loop with no think time, which is a stress
// rig — it deliberately keeps every player's request outstanding in order to
// find the ceiling. Nobody plays golf that way. A player records a shot every
// minute or so, so a hundred of them offer under two writes a second against a
// ceiling near three hundred.
//
// This test does not go anywhere near that far in the app's favour: a hundred
// players on a jittered three-second cycle offer ~100 writes/s, a third of the
// ceiling and some sixty times a real Saturday morning. Under that, every
// endpoint — writes included — is held to the same budget the reads carry.
//
// The jitter is load-bearing. An unjittered version of this test failed at 200
// ms not because the app was slow but because a hundred players started together
// stay in lockstep, and what it measured was a once-a-second thundering herd.
func TestLoadAtRealisticPace(t *testing.T) {
	requireLoadTest(t)

	workload := append(append([]step{}, readWorkload()...), writeWorkload()...)

	const level = 100
	f := newLoadFixture(t, level)
	results := runLoad(t, f, workload, strolling)
	report(t, level, strolling, results, workload)
	noFailures(t, results, workload)

	for _, s := range workload {
		if p95 := results[s.name].percentile(95); p95 > p95Budget {
			t.Errorf("%s: p95 %s over the %s budget at a realistic pace", s.name, round3(p95), p95Budget)
		}
	}
}

// request drives one workload step once, failing on a non-2xx. It is how the
// resource test in perf_test.go reuses this file's fixture and workloads without
// the sweep's timing machinery.
func (f *loadFixture) request(tb testing.TB, s step, player loadPlayer, iteration int) {
	tb.Helper()

	var body *strings.Reader
	if s.payload != nil {
		body = strings.NewReader(s.payload(iteration))
	} else {
		body = strings.NewReader("")
	}

	req := httptest.NewRequest(s.method, s.url(player), body)
	req.Header.Set("content-type", "application/json")
	for k, v := range player.headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)

	if rec.Code < 200 || rec.Code > 299 {
		tb.Fatalf("%s %s = %d: %s", s.method, s.url(player), rec.Code, rec.Body.String())
	}
}
