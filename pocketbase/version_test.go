package main

import (
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/tests"
)

// TestVersionRoute exercises GET /api/version (internal/version). PocketBase's
// own /api/health keeps its native envelope untouched — see
// TestHealthEndpointShape in parity_test.go — so the version is exposed
// through this sibling route instead, mirroring the Django app's
// /api/health {"version": ...} field.
func TestVersionRoute(t *testing.T) {
	t.Parallel()
	runPlay(t, nil, tests.ApiScenario{
		Name:            "version is unauthenticated and reports a build version",
		Method:          http.MethodGet,
		URL:             "/api/version",
		ExpectedStatus:  200,
		ExpectedContent: []string{`"version":"`},
	})
}
