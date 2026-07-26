package main

import (
	_ "embed"
	"os"

	"github.com/pocketbase/pocketbase/core"
)

// pb_schema.json is the checked-in source of truth for the six GolfTrack
// collections — fields, indexes and, from Phase 2 (#123), the API rules.
// Embedding it makes the binary self-initializing: no separate schema-apply
// step is needed on a fresh database.
//
//go:embed pb_schema.json
var schemaJSON []byte

// schemaSyncEnv disables the startup sync when set to "0", e.g. while
// experimenting with collections in the Admin UI.
const schemaSyncEnv = "GOLFTRACK_SCHEMA_SYNC"

// syncSchema reconciles the database to the embedded schema. The import is an
// upsert by collection id (deleteMissing=false), so it is idempotent and also
// repairs a drifted dev database. Consequence: schema edits made in the Admin
// UI must be exported back into pb_schema.json (scripts/export_schema.py)
// before a restart, or the restart reverts them.
//
// It is also the seam the Phase 2 tests use: they build a bare PocketBase test
// app and call this, so the rules under test are the committed ones rather
// than a fixture that can drift from them.
func syncSchema(app core.App) error {
	return app.ImportCollectionsByMarshaledJSON(schemaJSON, false)
}

// syncSchemaOnServe binds syncSchema to the serve lifecycle, honouring
// GOLFTRACK_SCHEMA_SYNC.
func syncSchemaOnServe(app core.App) {
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		if os.Getenv(schemaSyncEnv) == "0" {
			e.App.Logger().Warn("GolfTrack schema sync skipped (" + schemaSyncEnv + "=0)")
			return e.Next()
		}
		if err := syncSchema(e.App); err != nil {
			return err
		}
		e.App.Logger().Info("GolfTrack schema synced from embedded pb_schema.json")
		return e.Next()
	})
}
