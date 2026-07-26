package hooks

import (
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/authenv"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
)

// registerAuthConfig reconciles the users collection's authentication options
// with the environment at every startup (Phase 4, #125).
//
// Why this is a hook and not part of pb_schema.json: OAuth client secrets are
// secrets, and the client ids and the password-login switch differ per
// environment, so none of them can be committed. pb_schema.json therefore holds
// the *closed* baseline — no providers, OAuth2 disabled, password auth disabled
// — and this opens exactly what the environment configures.
//
// Ordering matters and is not accidental. The schema sync (schema.go) is bound
// to OnServe first, from main.go, and it imports the collection wholesale — so
// it resets these options on every boot. This handler is bound second, from
// hooks.Register, and therefore runs after and has the last word. Reversing the
// two would leave every restart with OAuth disabled.
func registerAuthConfig(app core.App) {
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		if err := ApplyAuthConfig(e.App); err != nil {
			return err
		}
		return e.Next()
	})
}

// ApplyAuthConfig writes the environment's authentication configuration onto
// the users collection: the OAuth2 providers, whether OAuth2 is enabled at all,
// and whether email+password authentication is available.
//
// It is exported so the tests can apply it directly rather than by booting a
// server, and it is a no-op save when nothing changed — a restart that changes
// no variable leaves the collection's `updated` timestamp alone.
func ApplyAuthConfig(app core.App) error {
	collection, err := app.FindCollectionByNameOrId(collections.NameUsers)
	if err != nil {
		return fmt.Errorf("load the %s collection: %w", collections.NameUsers, err)
	}

	providers, incomplete := authenv.Providers()
	for _, name := range incomplete {
		app.Logger().Warn(
			"GolfTrack OAuth2 provider half-configured and therefore skipped",
			"provider", name,
		)
	}

	configs := make([]core.OAuth2ProviderConfig, 0, len(providers))
	names := make([]string, 0, len(providers))
	for _, provider := range providers {
		configs = append(configs, core.OAuth2ProviderConfig{
			Name:         provider.Name,
			ClientId:     provider.ClientID,
			ClientSecret: provider.ClientSecret,
		})
		names = append(names, provider.Name)
	}

	passwordLogin := authenv.PasswordLoginEnabled()

	if !changed(collection, configs, passwordLogin) {
		return nil
	}

	collection.OAuth2.Providers = configs
	// Enabling OAuth2 with no provider configured would only add a login
	// endpoint that cannot succeed, so the switch follows the providers.
	collection.OAuth2.Enabled = len(configs) > 0
	collection.PasswordAuth.Enabled = passwordLogin

	if err := app.Save(collection); err != nil {
		return fmt.Errorf("apply the %s auth configuration: %w", collections.NameUsers, err)
	}

	if !collection.OAuth2.Enabled && !collection.PasswordAuth.Enabled {
		// Not fatal: a fresh instance is expected to boot this way, and the
		// Admin UI (a separate _superusers collection) still lets an operator
		// in to look around. Nobody can sign in to the app itself, though,
		// which is worth saying out loud.
		app.Logger().Warn(
			"GolfTrack has no way to sign in: no OAuth2 provider configured and " +
				authenv.EnvAllowPasswordLogin + " is off",
		)
	}

	app.Logger().Info(
		"GolfTrack auth configuration applied",
		"oauth2Providers", strings.Join(names, ","),
		"passwordLogin", passwordLogin,
	)

	return nil
}

// changed reports whether the collection already carries exactly this
// configuration.
func changed(collection *core.Collection, configs []core.OAuth2ProviderConfig, passwordLogin bool) bool {
	if collection.PasswordAuth.Enabled != passwordLogin {
		return true
	}
	if collection.OAuth2.Enabled != (len(configs) > 0) {
		return true
	}
	if len(collection.OAuth2.Providers) != len(configs) {
		return true
	}
	for i, want := range configs {
		got := collection.OAuth2.Providers[i]
		if got.Name != want.Name || got.ClientId != want.ClientId || got.ClientSecret != want.ClientSecret {
			return true
		}
	}
	return false
}
