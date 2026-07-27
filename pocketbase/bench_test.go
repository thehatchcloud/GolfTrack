package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
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

// benchServer builds the mux a request would land on, the way
// tests.ApiScenario does: a router with the default app routes, then OnServe
// triggered so the custom routes and hooks register.
func benchServer(b *testing.B) (http.Handler, map[string]string) {
	b.Helper()

	f := newPlayFixture(b)

	router, err := apis.NewRouter(f.app)
	if err != nil {
		b.Fatalf("new router: %v", err)
	}

	se := &core.ServeEvent{App: f.app, Router: router}
	if err := f.app.OnServe().Trigger(se, func(*core.ServeEvent) error { return nil }); err != nil {
		b.Fatalf("trigger OnServe: %v", err)
	}

	mux, err := se.Router.BuildMux()
	if err != nil {
		b.Fatalf("build mux: %v", err)
	}

	return mux, authHeader(b, f.owner)
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
