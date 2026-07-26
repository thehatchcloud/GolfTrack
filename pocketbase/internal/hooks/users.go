package hooks

import (
	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
)

// registerUserDefaults fills in `users.role` on create.
//
// This is the field default Django expresses declaratively
// (`role = CharField(..., default=Role.USER)`) and PocketBase has no
// equivalent for: a `select` field carries no default value, and `role` is
// required, so a create that omits it fails validation with
// "role: cannot be blank".
//
// That matters because the only create path the `users` create rule allows is
// OAuth2 sign-up, and PocketBase builds that record from the provider's profile
// — email, name, avatar — never `role`. Without this hook every first-time
// sign-in would 400.
//
// Scope: this sets the *default* only. Promoting a user to ADMIN from
// ADMIN_EMAILS is adminrole.go's OnRecordAuthWithOAuth2Request hook (Phase 4,
// #125), and it deliberately does not live here. The two meet on a first
// sign-in: with ADMIN_EMAILS set, that hook puts the decided role in the
// sign-up payload and this one finds the field already filled; with it unset,
// this one supplies USER.
func registerUserDefaults(app core.App) {
	app.OnRecordCreate(collections.NameUsers).BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetString(collections.FieldRole) == "" {
			e.Record.Set(collections.FieldRole, collections.UserRoleUser)
		}
		return e.Next()
	})
}
