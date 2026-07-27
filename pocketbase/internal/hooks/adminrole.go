package hooks

import (
	"maps"
	"slices"

	"github.com/pocketbase/pocketbase/core"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/authenv"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
)

// registerOAuth2SignIn owns what happens on an OAuth2 sign-in beyond what
// PocketBase does for itself (Phase 4, #125): the sign-up payload is discarded,
// and users.role is synced from ADMIN_EMAILS.
//
// Django does the second half in a `user_logged_in` receiver
// (accounts/signals.py) and this is a direct port of it, including the two
// behaviours that are easy to get wrong:
//
//   - it *revokes* as well as grants, so removing an address from the list
//     demotes that user on their next sign-in rather than leaving a stale
//     administrator behind;
//   - an empty ADMIN_EMAILS is a no-op, not "demote everyone" — the safety
//     valve for an environment that forgets the variable.
//
// The work happens before e.Next() so that the role is already correct in the
// auth response the caller receives, which is what makes the frontend's
// `record.role` usable straight out of the login call.
func registerOAuth2SignIn(app core.App) {
	app.OnRecordAuthWithOAuth2Request(collections.NameUsers).BindFunc(func(e *core.RecordAuthWithOAuth2RequestEvent) error {
		clearCreateData(e)
		if err := syncAdminRole(e); err != nil {
			return err
		}
		return e.Next()
	})
}

// clearCreateData drops the caller-supplied createData from an OAuth2 sign-in.
//
// PocketBase merges that map into the payload it creates the new user record
// from, and the users create rule (@request.context = "oauth2") does not
// restrict its contents, so anything in it lands on the record. That is a
// privilege-escalation and account-takeover surface rather than a feature:
// `{"role": "ADMIN"}` would mint an administrator, and `{"password": …}` would
// replace the random password PocketBase generates for OAuth2 sign-ups with one
// the caller knows — usable the moment an environment turns
// GOLFTRACK_ALLOW_PASSWORD_LOGIN on.
//
// GolfTrack has no use for the field either way: a new account is exactly the
// provider's profile (email, name, avatar) plus the role decided below.
func clearCreateData(e *core.RecordAuthWithOAuth2RequestEvent) {
	if len(e.CreateData) == 0 {
		return
	}

	e.App.Logger().Warn(
		"GolfTrack ignored the createData of an OAuth2 sign-in",
		"provider", e.ProviderName,
		"fields", mapKeys(e.CreateData),
	)
	e.CreateData = nil
}

// syncAdminRole grants or revokes users.role = ADMIN according to
// ADMIN_EMAILS.
//
// The decision is made from e.OAuth2User.Email — the address the provider just
// verified — and never from the record's own email or from anything the caller
// submitted. Those can diverge (PocketBase matches an existing account by
// email, and a record can be created with one address and later sign in with
// another), and only the provider-verified address is evidence of who is
// knocking.
func syncAdminRole(e *core.RecordAuthWithOAuth2RequestEvent) error {
	admins := authenv.LoadAdmins()
	if !admins.Enforced() {
		return nil
	}

	role := collections.UserRoleUser
	if admins.Contains(e.OAuth2User.Email) {
		role = collections.UserRoleAdmin
	}

	if e.Record == nil {
		// First sign-in: there is no record yet, so the role rides along in the
		// payload PocketBase is about to create it from. Going through
		// CreateData rather than saving afterwards keeps the whole sign-up in
		// PocketBase's transaction.
		e.CreateData = map[string]any{collections.FieldRole: role}
		return nil
	}

	if e.Record.GetString(collections.FieldRole) == role {
		return nil
	}

	e.Record.Set(collections.FieldRole, role)
	if err := e.App.Save(e.Record); err != nil {
		return err
	}

	e.App.Logger().Info(
		"GolfTrack synced a user's role from "+authenv.EnvAdminEmails,
		"user", e.Record.Id,
		"role", role,
	)
	return nil
}

// mapKeys returns the keys in a stable order so the warning above reads the
// same on every occurrence.
func mapKeys(m map[string]any) []string {
	return slices.Sorted(maps.Keys(m))
}
