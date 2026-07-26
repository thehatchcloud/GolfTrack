package hooks

import "github.com/pocketbase/pocketbase/core"

// registerUserDefaults fills in `users.role` on create.
//
// This is the field default Django expresses declaratively
// (`role = CharField(..., default=Role.USER)`) and PocketBase has no
// equivalent for: a `select` field carries no default value, and `role` is
// required, so a create that omits it fails validation with
// "role: cannot be blank".
//
// That matters because the only create path the `users` create rule allows is
// OAuth2 sign-up (Phase 4, #125), and PocketBase builds that record from the
// provider's profile — email, name, avatar — never `role`. Without this hook
// every first-time sign-in would 400.
//
// Scope: this sets the *default* only. Promoting a user to ADMIN from the
// ADMIN_EMAILS environment variable is Phase 4's `OnRecordAuthWithOAuth2Request`
// hook, and it deliberately does not live here.
func registerUserDefaults(app core.App) {
	app.OnRecordCreate(NameUsers).BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetString(FieldRole) == "" {
			e.Record.Set(FieldRole, UserRoleUser)
		}
		return e.Next()
	})
}
