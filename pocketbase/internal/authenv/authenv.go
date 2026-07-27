// Package authenv reads GolfTrack's authentication configuration from the
// process environment.
//
// Everything here is deployment configuration rather than schema: OAuth client
// credentials are secrets and cannot live in the committed pb_schema.json, and
// the admin list and the password-login switch differ per environment. The
// environment is therefore the source of truth for them, and
// internal/hooks/authconfig.go reconciles the users collection to it at every
// startup — the same shape as the schema sync, one authoritative source applied
// deterministically on boot.
//
// The variable names are Django's (config/settings.py), so a deployment that
// already sets them for the shipped app needs no new secrets when the
// PocketBase container arrives in Phase 9 (#130).
//
// It is a leaf package, like internal/collections and internal/apierr: the hook
// packages import it, it imports nothing of ours.
package authenv

import (
	"os"
	"strings"
)

// Environment variables read by this package.
const (
	// EnvAdminEmails is a comma-separated list of addresses granted
	// users.role = ADMIN on OAuth sign-in. Django: ADMIN_EMAILS.
	EnvAdminEmails = "ADMIN_EMAILS"

	// EnvGoogleClientID / EnvGoogleClientSecret configure the Google
	// provider. Django: the same two names.
	EnvGoogleClientID     = "GOOGLE_CLIENT_ID"
	EnvGoogleClientSecret = "GOOGLE_CLIENT_SECRET"

	// EnvMicrosoftClientID / EnvMicrosoftClientSecret configure the Microsoft
	// Entra ID provider. Django: the same two names.
	EnvMicrosoftClientID     = "MICROSOFT_CLIENT_ID"
	EnvMicrosoftClientSecret = "MICROSOFT_CLIENT_SECRET"

	// EnvAllowPasswordLogin opts an environment into email+password
	// authentication for accounts that already exist. Django:
	// DJANGO_ALLOW_PASSWORD_LOGIN. The GOLFTRACK_ prefix matches
	// GOLFTRACK_SCHEMA_SYNC, the other switch this application owns.
	EnvAllowPasswordLogin = "GOLFTRACK_ALLOW_PASSWORD_LOGIN"
)

// Provider is one OAuth2 provider's credentials, named as PocketBase's provider
// registry names it (tools/auth: "google", "microsoft").
type Provider struct {
	Name         string
	ClientID     string
	ClientSecret string
}

// providerEnv maps a PocketBase provider name onto the pair of variables that
// configure it. Order is the order providers are offered in.
var providerEnv = []struct {
	name      string
	idEnv     string
	secretEnv string
}{
	{"google", EnvGoogleClientID, EnvGoogleClientSecret},
	{"microsoft", EnvMicrosoftClientID, EnvMicrosoftClientSecret},
}

// Providers returns the fully configured OAuth2 providers, plus the names of
// any provider that has exactly one of its two variables set.
//
// A half-configured provider is reported rather than registered: PocketBase
// rejects a provider config with a blank client id or secret, so registering
// one would fail the whole collection save and take the configured providers
// down with it. The caller logs the name so a typo'd secret surfaces as a
// warning at startup instead of as a login that 500s later.
func Providers() (configured []Provider, incomplete []string) {
	for _, p := range providerEnv {
		id := strings.TrimSpace(os.Getenv(p.idEnv))
		secret := strings.TrimSpace(os.Getenv(p.secretEnv))

		switch {
		case id != "" && secret != "":
			configured = append(configured, Provider{
				Name:         p.name,
				ClientID:     id,
				ClientSecret: secret,
			})
		case id != "" || secret != "":
			incomplete = append(incomplete, p.name)
		}
	}

	return configured, incomplete
}

// PasswordLoginEnabled reports whether email+password authentication should be
// available on the users collection. Default false: OAuth is the way in, which
// is what the shipped app does (SOCIALACCOUNT_ONLY = not
// ALLOW_PASSWORD_LOGIN).
//
// This governs authentication only. Self-service *registration* is closed by
// the users create rule (@request.context = "oauth2") in every environment —
// see pocketbase/AUTH.md.
func PasswordLoginEnabled() bool {
	return boolEnv(EnvAllowPasswordLogin, false)
}

// Admins is the parsed ADMIN_EMAILS set: lower-cased addresses, blanks
// dropped.
type Admins map[string]struct{}

// LoadAdmins parses ADMIN_EMAILS. It is read per sign-in rather than cached so
// that changing the variable takes effect on the next login, the same as
// Django's settings-backed set does on a restart.
func LoadAdmins() Admins {
	admins := Admins{}
	for _, raw := range strings.Split(os.Getenv(EnvAdminEmails), ",") {
		email := normalizeEmail(raw)
		if email != "" {
			admins[email] = struct{}{}
		}
	}
	return admins
}

// Enforced reports whether ADMIN_EMAILS names anyone at all.
//
// An empty list is a deliberate no-op rather than "demote everyone": the same
// safety valve Django's sync_admin_role has, so an environment that forgets the
// variable does not silently strip every administrator on their next sign-in.
// It is documented in DEPLOYMENT.md for the shipped app.
func (a Admins) Enforced() bool { return len(a) > 0 }

// Contains reports whether email is in the admin list, comparing the way
// Django does — case-insensitively, on the trimmed address.
func (a Admins) Contains(email string) bool {
	normalized := normalizeEmail(email)
	if normalized == "" {
		return false
	}
	_, ok := a[normalized]
	return ok
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// truthy is the set of values Django's _bool_env accepts, so the same
// deployment variable means the same thing to both applications during the
// dual-stack rollout.
var truthy = map[string]struct{}{
	"1": {}, "true": {}, "yes": {}, "on": {},
}

// boolEnv parses a boolean variable, treating unset and empty as the fallback
// and anything outside the truthy set as false.
func boolEnv(name string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if value == "" {
		return fallback
	}
	_, ok := truthy[value]
	return ok
}
