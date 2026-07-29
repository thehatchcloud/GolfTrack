// Package hooks is the single registration point for GolfTrack's PocketBase
// hooks.
//
// Domain logic lives in subpackages of internal/hooks/domain, one per
// aggregate; each exposes a Register function and never binds hooks as an
// import side effect. Only Register below — called once from main.go — wires
// them up, so registration order is an explicit, reviewable list rather than a
// consequence of import order.
//
// Because this package imports the domain packages, the vocabulary and error
// helpers they all share cannot live here: they are in internal/collections and
// internal/apierr, which are leaves.
package hooks

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/courses"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/roundholes"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/rounds"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/shots"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/version"
)

// domainModules lists the registered domain modules in registration order.
//
// scoring is deliberately absent: it is pure arithmetic with no hooks to bind,
// and is called by rounds.
var domainModules = []struct {
	name     string
	register func(core.App)
}{
	{collections.NameCourses, courses.Register},
	{collections.NameRounds, rounds.Register},
	{collections.NameRoundHoles, roundholes.Register},
	{collections.NameShots, shots.Register},
}

// Register binds every GolfTrack hook onto the app: the users.role field
// default (users.go), the Phase 4 authentication hooks (authconfig.go,
// adminrole.go), the GET /api/version route (internal/version), and the
// Phase 3 domain modules — record hooks and the custom routes each one owns.
// See pocketbase/ARCHITECTURE.md for which rule lives where.
//
// registerAuthConfig binds to OnServe and must stay downstream of the schema
// sync main.go binds first — see its comment.
func Register(app core.App) {
	registerUserDefaults(app)
	registerAuthConfig(app)
	registerOAuth2SignIn(app)
	version.Register(app)

	names := make([]string, 0, len(domainModules))
	for _, module := range domainModules {
		module.register(app)
		names = append(names, module.name)
	}

	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		// Let PocketBase finish booting before touching the app.
		if err := e.Next(); err != nil {
			return err
		}

		e.App.Logger().Info(
			"GolfTrack hooks loaded",
			"collections", strings.Join(collections.Names, ","),
			"domainModules", strings.Join(names, ","),
		)
		return nil
	})
}
