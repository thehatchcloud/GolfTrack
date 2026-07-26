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
	_ "embed"
	"log"
	"os"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks"
)

// pb_schema.json is the checked-in source of truth for the six GolfTrack
// collections. Embedding it makes the binary self-initializing: no separate
// schema-apply step is needed on a fresh database.
//
//go:embed pb_schema.json
var schemaJSON []byte

func main() {
	app := pocketbase.New()

	// Reconcile the database to the embedded schema on every startup. The
	// import is an upsert by collection id (deleteMissing=false), so it is
	// idempotent and also repairs a drifted dev database. Consequence: schema
	// edits made in the Admin UI must be exported back into pb_schema.json
	// (scripts/export_schema.py) before a restart, or the restart reverts
	// them. Set GOLFTRACK_SCHEMA_SYNC=0 to skip, e.g. while experimenting in
	// the Admin UI.
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		if os.Getenv("GOLFTRACK_SCHEMA_SYNC") == "0" {
			e.App.Logger().Warn("GolfTrack schema sync skipped (GOLFTRACK_SCHEMA_SYNC=0)")
			return e.Next()
		}
		if err := e.App.ImportCollectionsByMarshaledJSON(schemaJSON, false); err != nil {
			return err
		}
		e.App.Logger().Info("GolfTrack schema synced from embedded pb_schema.json")
		return e.Next()
	})

	// The single registration point for all GolfTrack hooks; see
	// internal/hooks/hooks.go.
	hooks.Register(app)

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
