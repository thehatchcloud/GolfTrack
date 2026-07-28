package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/web"
)

// Phase 5 (#126) performance baseline.
//
// These measure the endpoints the play screen hits on every tap, in-process
// against the same seeded fixture the parity suite uses. The numbers are a
// floor, not a forecast: there is no network, no TLS and no Litestream in the
// loop, and the database is a fresh SQLite file with a few dozen rows.
//
// What they are for is comparison — against the Django equivalents (see
// API.md § "Performance baseline" for how to take those) and against a later
// PocketBase change that might quietly add a query per request. Run:
//
//	cd pocketbase && go test -run '^$' -bench . -benchmem
//
// They are excluded from `make pb-test`, which runs tests only.

// benchServer builds the mux a request would land on and the header that
// authenticates the fixture's owner against it.
//
// The frontend is registered too, so the page benchmarks have something to hit.
// It costs the API benchmarks nothing: the pages are separate routes, and the
// template set is parsed once at registration rather than per request.
func benchServer(b *testing.B) (http.Handler, map[string]string) {
	b.Helper()

	f := newPlayFixture(b)
	web.Register(f.app)

	// Both credentials: the API routes read the Authorization header, the pages
	// read the pb_auth cookie. Without the cookie the page benchmarks would
	// render the signed-out home screen and measure nothing.
	headers := authHeader(b, f.owner)
	for k, v := range authCookie(b, f.owner) {
		headers[k] = v
	}

	return newMux(b, f.app), headers
}

// benchRequest replays one request b.N times, failing on the first non-2xx so a
// benchmark cannot quietly measure an error path.
//
// reset, if given, is a request replayed untimed after each iteration to undo
// the one being measured. Without it an accumulating write would be timing a
// response body that grows with b.N rather than the operation.
func benchRequest(b *testing.B, method, url, payload string, reset ...string) {
	b.Helper()

	mux, headers := benchServer(b)

	send := func(method, url, payload string) {
		req := httptest.NewRequest(method, url, strings.NewReader(payload))
		req.Header.Set("content-type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code < 200 || rec.Code > 299 {
			b.Fatalf("%s %s = %d: %s", method, url, rec.Code, rec.Body.String())
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		send(method, url, payload)

		if len(reset) > 0 {
			b.StopTimer()
			send(reset[0], reset[1], "")
			b.StartTimer()
		}
	}
}

// BenchmarkAddShot is the hot path: one tap on the club pad. It writes a shot,
// refreshes the stroke cache and returns the hole with its shots, all in a
// transaction — so it is also the most expensive thing the play screen does.
//
// Each iteration is undone by an untimed undo, so what is measured is the first
// shot on an empty hole every time.
func BenchmarkAddShot(b *testing.B) {
	benchRequest(b, http.MethodPost, "/api/rounds/"+idPlayRound+"/holes/1/shots", `{"club":"Driver"}`,
		http.MethodPost, "/api/rounds/"+idPlayRound+"/holes/1/undo")
}

// BenchmarkSetCurrentHole is the other write the play screen makes, and the
// cheapest custom route: one record read, one field, one save.
func BenchmarkSetCurrentHole(b *testing.B) {
	benchRequest(b, http.MethodPatch, "/api/rounds/"+idPlayRound+"/current-hole", `{"currentHole":5}`)
}

// BenchmarkRoundDetail is the read the review screen makes, in the shape
// TestRelationsAreIdsPlusExpand established is possible: the whole
// round → holes → shots chain in one request.
func BenchmarkRoundDetail(b *testing.B) {
	benchRequest(b, http.MethodGet,
		recordURL(collections.NameRounds, idPlayRound)+"?expand=round_holes_via_round.shots_via_round_hole", "")
}

// BenchmarkListRounds is the home screen's list, rule-filtered to the caller.
func BenchmarkListRounds(b *testing.B) {
	benchRequest(b, http.MethodGet,
		recordsURL(collections.NameRounds)+"?filter=(status='completed')&expand=course", "")
}

// BenchmarkHealth is the floor: routing and serialisation with no database work
// behind it, so the other numbers can be read net of the framework.
func BenchmarkHealth(b *testing.B) {
	benchRequest(b, http.MethodGet, "/api/health", "")
}

// The custom read routes (Phase 8, #129).
//
// The four benchmarks above were written in Phase 5, before those routes
// existed, so three of them measure the *generated* endpoints — which is the
// right baseline for "is PocketBase itself fast" and the wrong one for "is the
// app fast", because the frontend does not use them. These measure what it does
// use, and they are where the per-record reads this phase removed were.
//
// The fixture is an 18-hole round, so a per-hole read costs eighteen of
// everything. That is the point: the numbers only mean something at a size the
// N+1 could show up in.

// BenchmarkRoundDetailRoute is the play and review screens' read: a round with
// its course, its eighteen holes and every shot on them, as one response.
func BenchmarkRoundDetailRoute(b *testing.B) {
	benchRequest(b, http.MethodGet, "/api/rounds/"+idPlayRound, "")
}

// BenchmarkInProgressRoute is the read the home screen and the play screen both
// open with — the same assembly as the detail route, reached without an id.
func BenchmarkInProgressRoute(b *testing.B) {
	benchRequest(b, http.MethodGet, "/api/rounds/in-progress", "")
}

// BenchmarkListRoundsRoute is the history list in the shape the frontend renders
// it: each round with the course it was played on nested inline.
func BenchmarkListRoundsRoute(b *testing.B) {
	benchRequest(b, http.MethodGet, "/api/rounds", "")
}

// BenchmarkListCoursesRoute is the course picker: every course with its whole
// hole set and a derived total par.
func BenchmarkListCoursesRoute(b *testing.B) {
	benchRequest(b, http.MethodGet, "/api/courses", "")
}

// BenchmarkHomePage is the same work again through the server-rendered path —
// template execution on top of the two round reads — so a regression in
// rendering is separable from one in the query layer.
func BenchmarkHomePage(b *testing.B) {
	benchRequest(b, http.MethodGet, "/", "")
}
