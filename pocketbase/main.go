// GolfTrack's PocketBase application.
//
// PocketBase is used as a Go framework rather than a downloaded binary: this
// package builds to a single portable executable that embeds the PocketBase
// server, the GolfTrack collection schema (pb_schema.json, via go:embed), and
// — from Phase 3 (#124) on — the domain logic as Go hooks. The standard
// PocketBase CLI (serve, superuser, migrate, …) is retained, so the binary is
// operated exactly like stock PocketBase.
package main

import (
	"log"

	"github.com/pocketbase/pocketbase"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/web"
)

func main() {
	app := pocketbase.New()

	// Reconcile the database to the embedded schema on every startup, and
	// with it the API rules — see schema.go.
	syncSchemaOnServe(app)

	// The single registration point for all GolfTrack hooks; see
	// internal/hooks/hooks.go.
	hooks.Register(app)

	// The frontend: page routes, templates and static assets, all embedded in
	// this binary — see internal/web. Registered separately from the hooks
	// because it is the browser's half of the app rather than a domain rule.
	web.Register(app)

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
