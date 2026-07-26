// Package hooks is the single registration point for GolfTrack's PocketBase
// hooks.
//
// Domain logic lives in subpackages of internal/hooks (one per aggregate,
// under domain/ from Phase 3, #124); each exposes a Register function and
// never binds hooks as an import side effect. Only Register below — called
// once from main.go — wires them up, so registration order is an explicit,
// reviewable list rather than a consequence of import order.
package hooks

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// domainModules lists the registered domain modules in registration order.
// Phase 3 appends here as each module lands; empty means schema-only
// behaviour.
var domainModules = []string{}

// Register binds every GolfTrack hook onto the app. Phase 1 registers no
// domain behaviour — see pocketbase/ARCHITECTURE.md for the modules Phase 3
// adds and the hooks each one binds.
func Register(app core.App) {
	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		// Let PocketBase finish booting before touching the app.
		if err := e.Next(); err != nil {
			return err
		}

		modules := "(none — Phase 1)"
		if len(domainModules) > 0 {
			modules = strings.Join(domainModules, ",")
		}
		e.App.Logger().Info(
			"GolfTrack hooks loaded",
			"collections", strings.Join(CollectionNames, ","),
			"domainModules", modules,
		)
		return nil
	})
}
