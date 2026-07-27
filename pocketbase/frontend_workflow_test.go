package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Phase 7 (#128) end-to-end check.
//
// There is no reference frontend checked into the repository yet, so this
// drives the same request sequence a browser client would make for every
// workflow the issue lists — start a round, play it, complete it, review it,
// and separately cancel one — over one real in-process HTTP server, using
// only the camelCase custom routes a frontend is expected to call. It is the
// automated stand-in for "test all frontend workflows end-to-end" until an
// actual client exists: a regression here is a regression a browser would hit
// too.

// workflowServer builds the same mux benchServer does — the real router, with
// OnServe triggered so the custom routes are live — against a fresh play
// fixture.
func workflowServer(t *testing.T) (http.Handler, *playFixture) {
	t.Helper()

	f := newPlayFixture(t)

	router, err := apis.NewRouter(f.app)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	se := &core.ServeEvent{App: f.app, Router: router}
	if err := f.app.OnServe().Trigger(se, func(*core.ServeEvent) error { return nil }); err != nil {
		t.Fatalf("trigger OnServe: %v", err)
	}

	mux, err := se.Router.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}

	return mux, f
}

// workflowRequest sends one request and returns the decoded body alongside the
// status, failing the test immediately rather than letting a bad response
// cascade into a confusing later assertion. Object and array bodies both
// decode into the same map/slice-friendly type via a pointer to `any`.
func workflowRequest(t *testing.T, mux http.Handler, headers map[string]string, method, url, payload string) (int, any) {
	t.Helper()

	req := httptest.NewRequest(method, url, strings.NewReader(payload))
	req.Header.Set("content-type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var body any
	if rec.Body.Len() > 0 && rec.Body.String() != "null" {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s %s: decode body %q: %v", method, url, rec.Body.String(), err)
		}
	}

	return rec.Code, body
}

// TestFrontendWorkflowStartPlayCompleteReview walks: home (no in-progress
// round) -> courses list -> start round -> play (add, undo, re-add, move
// hole) -> complete -> home again (in-progress is gone) -> review (round
// detail, and the round appears in the completed list).
func TestFrontendWorkflowStartPlayCompleteReview(t *testing.T) {
	mux, f := workflowServer(t)
	headers := authHeader(t, f.other) // f.other owns nothing yet; see newPlayFixture

	// Home page, before starting anything: no in-progress round.
	if code, body := workflowRequest(t, mux, headers, http.MethodGet, "/api/rounds/in-progress", ""); code != http.StatusOK || body != nil {
		t.Fatalf("in-progress before start = %d %v, want 200 null", code, body)
	}

	// Courses list, to pick one from. GET /api/courses answers a bare array.
	code, rawBody := workflowRequest(t, mux, headers, http.MethodGet, "/api/courses", "")
	items, ok := rawBody.([]any)
	if code != http.StatusOK || !ok || len(items) == 0 {
		t.Fatalf("courses list = %d %v, want a non-empty array", code, rawBody)
	}

	// Start round: pick the course, front9 mode.
	code, rawBody = workflowRequest(t, mux, headers, http.MethodPost, "/api/rounds/",
		`{"courseId":"`+idPlayCourse9+`","playMode":"full"}`)
	body, _ := rawBody.(map[string]any)
	if code != http.StatusCreated {
		t.Fatalf("start round = %d %v", code, rawBody)
	}
	roundID, _ := body["id"].(string)
	if roundID == "" {
		t.Fatalf("start round did not return an id: %v", body)
	}

	// Play: add a shot, undo it, add it back, then move to the next hole.
	if code, body := workflowRequest(t, mux, headers, http.MethodPost, "/api/rounds/"+roundID+"/holes/1/shots", `{"club":"Driver"}`); code != http.StatusOK {
		t.Fatalf("add shot = %d %v", code, body)
	}
	if code, body := workflowRequest(t, mux, headers, http.MethodPost, "/api/rounds/"+roundID+"/holes/1/undo", ""); code != http.StatusOK {
		t.Fatalf("undo shot = %d %v", code, body)
	}
	if code, body := workflowRequest(t, mux, headers, http.MethodPost, "/api/rounds/"+roundID+"/holes/1/shots", `{"club":"Driver","note":"down the middle"}`); code != http.StatusOK {
		t.Fatalf("re-add shot = %d %v", code, body)
	}
	if code, body := workflowRequest(t, mux, headers, http.MethodPatch, "/api/rounds/"+roundID+"/current-hole", `{"currentHole":2}`); code != http.StatusOK {
		t.Fatalf("set current hole = %d %v", code, body)
	}

	// The round mid-play is what the play screen re-reads on refresh: nested
	// course, null totals, camelCase throughout.
	code, rawBody = workflowRequest(t, mux, headers, http.MethodGet, "/api/rounds/"+roundID, "")
	body, _ = rawBody.(map[string]any)
	if code != http.StatusOK {
		t.Fatalf("round detail mid-play = %d %v", code, rawBody)
	}
	if _, ok := body["course"].(map[string]any); !ok {
		t.Fatalf("round detail course is not a nested object: %v", body["course"])
	}
	if body["totalStrokes"] != nil {
		t.Fatalf("round detail totalStrokes before completion = %v, want null", body["totalStrokes"])
	}
	if body["currentHole"] != float64(2) {
		t.Fatalf("round detail currentHole = %v, want 2", body["currentHole"])
	}

	// Complete: the response is just an id (Django's `IdOut`); the totals show
	// up on a follow-up GET, which is what the review screen re-reads.
	code, rawBody = workflowRequest(t, mux, headers, http.MethodPost, "/api/rounds/"+roundID+"/complete", `{}`)
	if code != http.StatusOK {
		t.Fatalf("complete round = %d %v", code, rawBody)
	}
	code, rawBody = workflowRequest(t, mux, headers, http.MethodGet, "/api/rounds/"+roundID, "")
	body, _ = rawBody.(map[string]any)
	if code != http.StatusOK {
		t.Fatalf("round detail after completion = %d %v", code, rawBody)
	}
	if body["totalStrokes"] == nil {
		t.Fatalf("completed round has no totalStrokes: %v", body)
	}

	// Home again: no in-progress round left for this player.
	if code, body := workflowRequest(t, mux, headers, http.MethodGet, "/api/rounds/in-progress", ""); code != http.StatusOK || body != nil {
		t.Fatalf("in-progress after completion = %d %v, want 200 null", code, body)
	}

	// Review: the completed round shows up in the list, and its detail still
	// resolves through the same route.
	code, rawBody = workflowRequest(t, mux, headers, http.MethodGet, "/api/rounds/", "")
	rounds, ok := rawBody.([]any)
	if code != http.StatusOK || !ok {
		t.Fatalf("rounds list = %d %v, want an array", code, rawBody)
	}
	found := false
	for _, item := range rounds {
		if r, ok := item.(map[string]any); ok && r["id"] == roundID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("completed round %q missing from rounds list: %v", roundID, rounds)
	}
}

// TestFrontendWorkflowCancel walks: start a round, then cancel it instead of
// finishing — the review list must never see it.
func TestFrontendWorkflowCancel(t *testing.T) {
	mux, f := workflowServer(t)
	headers := authHeader(t, f.other)

	_, rawBody := workflowRequest(t, mux, headers, http.MethodPost, "/api/rounds/",
		`{"courseId":"`+idPlayCourse9+`","playMode":"full"}`)
	body, _ := rawBody.(map[string]any)
	roundID, _ := body["id"].(string)
	if roundID == "" {
		t.Fatalf("start round did not return an id: %v", body)
	}

	if code, body := workflowRequest(t, mux, headers, http.MethodPost, "/api/rounds/"+roundID+"/cancel", ""); code != http.StatusOK {
		t.Fatalf("cancel round = %d %v", code, body)
	}

	if code, body := workflowRequest(t, mux, headers, http.MethodGet, "/api/rounds/"+roundID, ""); code != http.StatusNotFound {
		t.Fatalf("cancelled round detail = %d %v, want 404", code, body)
	}

	if code, body := workflowRequest(t, mux, headers, http.MethodGet, "/api/rounds/in-progress", ""); code != http.StatusOK || body != nil {
		t.Fatalf("in-progress after cancel = %d %v, want 200 null", code, body)
	}
}
