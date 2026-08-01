package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
)

// Phase 7 (#128) end-to-end check.
//
// There is no reference frontend checked into the repository yet, so this
// drives the same request sequence a browser client would make for every
// workflow the issue lists — start a round, play it, complete it, review it,
// separately cancel one, and administer courses — over one real in-process HTTP
// server, using only the camelCase custom routes a frontend is expected to
// call. The courses leg arrived with the Phase 7B write routes; until those
// existed, create/edit had no route to walk. It is the
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

	// Complete: the response is just an id (Django's `IdOut`); the review step
	// can override the round's start/end times immediately before submission.
	code, rawBody = workflowRequest(t, mux, headers, http.MethodPost, "/api/rounds/"+roundID+"/complete",
		`{"startedAt":"2026-05-04T06:07","finishedAt":"2026-05-04T10:15"}`)
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
	if body["startedAt"] != "2026-05-04 06:07:00.000Z" || body["finishedAt"] != "2026-05-04 10:15:00.000Z" {
		t.Fatalf("completed round times = %v / %v, want edited values", body["startedAt"], body["finishedAt"])
	}

	// After submission the player can still adjust the recorded times.
	code, rawBody = workflowRequest(t, mux, headers, http.MethodPatch, "/api/rounds/"+roundID,
		`{"startedAt":"2026-05-04T07:00","finishedAt":"2026-05-04T11:30"}`)
	body, _ = rawBody.(map[string]any)
	if code != http.StatusOK {
		t.Fatalf("update round times = %d %v", code, rawBody)
	}
	if body["startedAt"] != "2026-05-04 07:00:00.000Z" || body["finishedAt"] != "2026-05-04 11:30:00.000Z" {
		t.Fatalf("updated round times = %v", body)
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

// TestFrontendWorkflowCourseAdmin walks the courses workflow, which is the one
// item on Phase 7A's list that had no route to walk until the write routes
// existed: an admin adds a course, sees it in the list, edits it, and archives
// it out of the list again. Archiving is how the app removes a course —
// courses/api.py has no delete — and it stays on the generated PATCH, so this
// is also the one leg that crosses from the custom routes into PocketBase's own
// endpoint, in the shape a frontend would have to handle.
func TestFrontendWorkflowCourseAdmin(t *testing.T) {
	mux, f := workflowServer(t)
	admin := authHeader(t, f.admin)

	// Create: eighteen holes and the course in one request.
	code, rawBody := workflowRequest(t, mux, admin, http.MethodPost, "/api/courses/",
		coursePayload("Workflow Downs", 18, 4))
	created, _ := rawBody.(map[string]any)
	if code != http.StatusCreated {
		t.Fatalf("create course = %d %v", code, rawBody)
	}
	courseID, _ := created["id"].(string)
	if courseID == "" {
		t.Fatalf("create course did not return an id: %v", created)
	}

	// The courses list is what the picker renders, so the new course has to be
	// in it, complete, without a second request per hole.
	code, rawBody = workflowRequest(t, mux, admin, http.MethodGet, "/api/courses", "")
	items, ok := rawBody.([]any)
	if code != http.StatusOK || !ok {
		t.Fatalf("courses list = %d %v, want an array", code, rawBody)
	}
	found := false
	for _, item := range items {
		course, ok := item.(map[string]any)
		if !ok || course["id"] != courseID {
			continue
		}
		found = true
		if course["totalPar"] != float64(72) {
			t.Errorf("new course totalPar = %v, want 72", course["totalPar"])
		}
		if holes, ok := course["holes"].([]any); !ok || len(holes) != 18 {
			t.Errorf("new course has %v holes, want 18", course["holes"])
		}
	}
	if !found {
		t.Fatalf("course %q missing from the list: %v", courseID, items)
	}

	// Edit: the same form, re-submitted with a new name and new pars.
	if code, body := workflowRequest(t, mux, admin, http.MethodPut, "/api/courses/"+courseID,
		coursePayload("Workflow Downs West", 18, 5)); code != http.StatusOK {
		t.Fatalf("edit course = %d %v", code, body)
	}

	code, rawBody = workflowRequest(t, mux, admin, http.MethodGet, "/api/courses/"+courseID, "")
	detail, _ := rawBody.(map[string]any)
	if code != http.StatusOK {
		t.Fatalf("course detail after edit = %d %v", code, rawBody)
	}
	if detail["name"] != "Workflow Downs West" {
		t.Errorf("course name after edit = %v, want %q", detail["name"], "Workflow Downs West")
	}
	if detail["totalPar"] != float64(90) {
		t.Errorf("course totalPar after edit = %v, want 90", detail["totalPar"])
	}

	// A player cannot do either of those — the nav hides the links, and the
	// API is what actually enforces it.
	player := authHeader(t, f.other)
	if code, _ := workflowRequest(t, mux, player, http.MethodPost, "/api/courses/",
		coursePayload("Not Mine", 9, 4)); code != http.StatusForbidden {
		t.Errorf("a player creating a course = %d, want 403", code)
	}
	if code, _ := workflowRequest(t, mux, player, http.MethodPut, "/api/courses/"+courseID,
		coursePayload("Not Mine", 18, 4)); code != http.StatusForbidden {
		t.Errorf("a player editing a course = %d, want 403", code)
	}

	// Archive: the generated PATCH, and the course drops out of the list.
	if code, body := workflowRequest(t, mux, admin, http.MethodPatch,
		"/api/collections/"+collections.NameCourses+"/records/"+courseID,
		`{"is_archived":true}`); code != http.StatusOK {
		t.Fatalf("archive course = %d %v", code, body)
	}

	code, rawBody = workflowRequest(t, mux, admin, http.MethodGet, "/api/courses", "")
	items, ok = rawBody.([]any)
	if code != http.StatusOK || !ok {
		t.Fatalf("courses list after archive = %d %v", code, rawBody)
	}
	for _, item := range items {
		if course, ok := item.(map[string]any); ok && course["id"] == courseID {
			t.Fatalf("archived course %q is still listed", courseID)
		}
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
