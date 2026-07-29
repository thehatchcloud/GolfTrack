// Package version resolves the running build's version — a calendar date
// plus the short git commit SHA it was built from, e.g. "2026.07.29+a3f9c2e"
// — so the PocketBase app reports its build the same way the Django app's
// core/version.py does.
package version

import (
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// GitSHA and BuildDate are set via `-ldflags -X` at Docker build time (see
// ../../Dockerfile). Both are empty under `go run` / `go test` (e.g. `make
// pb-dev`), where Get falls back to a live git lookup.
var (
	GitSHA    string
	BuildDate string
)

var (
	once  sync.Once
	value string
)

// Get returns the build's version string. A commit-less checkout (a source
// archive with no .git directory) falls back to "<today>+dev".
func Get() string {
	once.Do(func() { value = compute() })
	return value
}

func compute() string {
	sha := GitSHA
	if sha == "" {
		sha = gitShortSHA()
	}
	if sha == "" {
		sha = "dev"
	}

	date := BuildDate
	if date == "" {
		date = time.Now().UTC().Format("2006.01.02")
	}

	return date + "+" + sha
}

func gitShortSHA() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Register mounts GET /api/version.
//
// This is a sibling of PocketBase's own /api/health rather than a field
// folded into it: that endpoint's exact envelope is asserted in
// parity_test.go (TestHealthEndpointShape) against the Django health-probe
// contract, so extending it here would break that contract for no reason.
func Register(app core.App) {
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		e.Router.GET("/api/version", func(re *core.RequestEvent) error {
			return re.JSON(http.StatusOK, map[string]string{"version": Get()})
		})
		return e.Next()
	})
}
