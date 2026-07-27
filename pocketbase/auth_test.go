package main

import (
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/auth"
	"golang.org/x/oauth2"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/authenv"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/collections"
	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks"
)

// Phase 4 (#125) authentication suite.
//
// The sign-in tests drive the real endpoint — POST
// /api/collections/users/auth-with-oauth2 — with a fake provider swapped into
// PocketBase's provider registry under the name "google". Everything downstream
// of the token exchange is therefore the production path: the same form, the
// same record lookup, the same create rule, the same
// OnRecordAuthWithOAuth2Request hook. That is what #125's gate asks for when it
// says a first sign-in must land with role = USER "through the real OAuth path
// rather than a direct save".
//
// The provider configuration itself comes from the environment
// (internal/authenv), applied to the users collection on OnServe, so every
// scenario sets the variables it needs with t.Setenv and lets the scenario's
// server boot pick them up.

const (
	googleClientID     = "google-client-id"
	googleClientSecret = "google-client-secret"
)

// -----------------------------------------------------------------------------
// the fake provider
// -----------------------------------------------------------------------------

var _ auth.Provider = (*mockProvider)(nil)

// mockProvider answers the two calls PocketBase makes against a provider during
// a login — exchange the code, fetch the profile — without leaving the process.
type mockProvider struct {
	auth.BaseProvider

	authUser *auth.AuthUser
}

func (p *mockProvider) FetchToken(code string, opts ...oauth2.AuthCodeOption) (*oauth2.Token, error) {
	return &oauth2.Token{AccessToken: "mock-access-token"}, nil
}

func (p *mockProvider) FetchAuthUser(token *oauth2.Token) (*auth.AuthUser, error) {
	return p.authUser, nil
}

// mockGoogle registers the fake provider under Google's name for the duration
// of the test and configures the credentials that make ApplyAuthConfig enable
// it.
//
// Using a real provider's name rather than inventing a "test" one keeps the
// scenario on the configured path: the collection ends up with exactly the
// provider list a deployment with GOOGLE_CLIENT_ID set would have.
func mockGoogle(t *testing.T, user *auth.AuthUser) {
	t.Helper()

	original, existed := auth.Providers[auth.NameGoogle]
	t.Cleanup(func() {
		if existed {
			auth.Providers[auth.NameGoogle] = original
		} else {
			delete(auth.Providers, auth.NameGoogle)
		}
	})
	auth.Providers[auth.NameGoogle] = func() auth.Provider {
		return &mockProvider{authUser: user}
	}

	t.Setenv(authenv.EnvGoogleClientID, googleClientID)
	t.Setenv(authenv.EnvGoogleClientSecret, googleClientSecret)
}

// oauth2User is the profile the fake provider returns. The id is what PocketBase
// stores in the _externalAuths link.
func oauth2User(id, email, name string) *auth.AuthUser {
	return &auth.AuthUser{Id: id, Email: email, Name: name}
}

const (
	authWithOAuth2URL   = "/api/collections/users/auth-with-oauth2"
	authWithPasswordURL = "/api/collections/users/auth-with-password"
	authMethodsURL      = "/api/collections/users/auth-methods"
)

// oauth2Login is the request body a client sends after the provider redirect.
// The code is never verified — the fake provider accepts anything.
func oauth2Login(extra string) string {
	return `{"provider":"google","code":"mock-code"` + extra + `}`
}

// roleOf reads a user's stored role back, by email, so an assertion is about
// what was persisted rather than what the response happened to say.
func roleOf(t testing.TB, app core.App, email string) string {
	t.Helper()

	record, err := app.FindAuthRecordByEmail(collections.NameUsers, email)
	if err != nil {
		t.Fatalf("find user %q: %v", email, err)
	}
	return record.GetString(collections.FieldRole)
}

// -----------------------------------------------------------------------------
// provider configuration from the environment
// -----------------------------------------------------------------------------

// TestOAuth2ProvidersComeFromTheEnvironment covers the whole of task 1: a
// deployment supplies client credentials as environment variables and both
// providers show up on the collection, secrets included but never served.
func TestOAuth2ProvidersComeFromTheEnvironment(t *testing.T) {
	t.Setenv(authenv.EnvGoogleClientID, googleClientID)
	t.Setenv(authenv.EnvGoogleClientSecret, googleClientSecret)
	t.Setenv(authenv.EnvMicrosoftClientID, "microsoft-client-id")
	t.Setenv(authenv.EnvMicrosoftClientSecret, "microsoft-client-secret")

	app := newTestApp(t)
	if err := hooks.ApplyAuthConfig(app); err != nil {
		t.Fatalf("apply auth config: %v", err)
	}

	users, err := app.FindCollectionByNameOrId(collections.NameUsers)
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}

	if !users.OAuth2.Enabled {
		t.Error("OAuth2 is disabled even though both providers are configured")
	}
	if got := len(users.OAuth2.Providers); got != 2 {
		t.Fatalf("configured providers = %d, want 2", got)
	}
	for _, want := range []struct{ name, id, secret string }{
		{auth.NameGoogle, googleClientID, googleClientSecret},
		{auth.NameMicrosoft, "microsoft-client-id", "microsoft-client-secret"},
	} {
		config, ok := users.OAuth2.GetProviderConfig(want.name)
		if !ok {
			t.Errorf("provider %q is not configured", want.name)
			continue
		}
		if config.ClientId != want.id {
			t.Errorf("%s clientId = %q, want %q", want.name, config.ClientId, want.id)
		}
		if config.ClientSecret != want.secret {
			t.Errorf("%s clientSecret = %q, want %q", want.name, config.ClientSecret, want.secret)
		}
	}
}

// TestApplyAuthConfigIsIdempotent pins the "no-op save when nothing changed"
// behaviour: the config is reapplied on every startup, and a restart that
// changes no variable should not touch the collection.
func TestApplyAuthConfigIsIdempotent(t *testing.T) {
	t.Setenv(authenv.EnvGoogleClientID, googleClientID)
	t.Setenv(authenv.EnvGoogleClientSecret, googleClientSecret)

	app := newTestApp(t)
	if err := hooks.ApplyAuthConfig(app); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	before, err := app.FindCollectionByNameOrId(collections.NameUsers)
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}
	updated := before.Updated

	if err := hooks.ApplyAuthConfig(app); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	after, err := app.FindCollectionByNameOrId(collections.NameUsers)
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}
	if after.Updated != updated {
		t.Errorf("re-applying an unchanged configuration rewrote the collection (updated %v -> %v)", updated, after.Updated)
	}
}

// TestHalfConfiguredProviderIsSkipped: a client id without its secret is a
// deployment mistake. PocketBase would reject the provider config and take the
// whole collection save down with it, so the provider is dropped and the
// working ones stay up.
func TestHalfConfiguredProviderIsSkipped(t *testing.T) {
	t.Setenv(authenv.EnvGoogleClientID, googleClientID)
	t.Setenv(authenv.EnvGoogleClientSecret, googleClientSecret)
	t.Setenv(authenv.EnvMicrosoftClientID, "microsoft-client-id")
	// MICROSOFT_CLIENT_SECRET deliberately unset.

	app := newTestApp(t)
	if err := hooks.ApplyAuthConfig(app); err != nil {
		t.Fatalf("apply auth config: %v", err)
	}

	users, err := app.FindCollectionByNameOrId(collections.NameUsers)
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}
	if _, ok := users.OAuth2.GetProviderConfig(auth.NameMicrosoft); ok {
		t.Error("the half-configured microsoft provider was registered")
	}
	if _, ok := users.OAuth2.GetProviderConfig(auth.NameGoogle); !ok {
		t.Error("the fully configured google provider was dropped along with it")
	}
}

// TestAuthMethodsAdvertiseTheProvidersWithoutSecrets is the client-facing half
// of the same thing: /auth-methods is what the frontend reads to render the
// sign-in buttons (Phase 7, #128), and a client secret must never appear in it.
func TestAuthMethodsAdvertiseTheProvidersWithoutSecrets(t *testing.T) {
	// The real provider factories, not the fake one: this is about what
	// PocketBase publishes for a deployment configured the normal way, down to
	// the authorization URL it builds from the client id.
	t.Setenv(authenv.EnvGoogleClientID, googleClientID)
	t.Setenv(authenv.EnvGoogleClientSecret, googleClientSecret)
	t.Setenv(authenv.EnvMicrosoftClientID, "microsoft-client-id")
	t.Setenv(authenv.EnvMicrosoftClientSecret, "microsoft-client-secret")

	run(t, anonymous, tests.ApiScenario{
		Name:           "auth-methods lists the configured providers",
		Method:         http.MethodGet,
		URL:            authMethodsURL,
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"oauth2":{`,
			`"name":"google"`,
			`"displayName":"Google"`,
			`client_id=` + googleClientID,
			`"name":"microsoft"`,
			`"displayName":"Microsoft"`,
			// password login is off unless an environment opts in
			`"emailPassword":false`,
		},
		NotExpectedContent: []string{googleClientSecret, "microsoft-client-secret"},
	})
}

// TestOAuth2IsRefusedWhenNoProviderIsConfigured: a fresh instance with no
// credentials in its environment has OAuth2 off, and PocketBase answers 403
// rather than pretending the provider exists.
func TestOAuth2IsRefusedWhenNoProviderIsConfigured(t *testing.T) {
	run(t, anonymous, tests.ApiScenario{
		Name:            "sign-in without a configured provider",
		Method:          http.MethodPost,
		URL:             authWithOAuth2URL,
		Body:            body(oauth2Login("")),
		ExpectedStatus:  403,
		ExpectedContent: []string{`"status":403`},
	})
}

// -----------------------------------------------------------------------------
// password login — the recorded decision (task 4)
// -----------------------------------------------------------------------------

// TestPasswordLoginIsOffByDefault pins the default half of the decision:
// GolfTrack is OAuth-only unless an environment opts out, matching the shipped
// app's SOCIALACCOUNT_ONLY.
func TestPasswordLoginIsOffByDefault(t *testing.T) {
	run(t, anonymous, tests.ApiScenario{
		Name:            "password login is refused by default",
		Method:          http.MethodPost,
		URL:             authWithPasswordURL,
		Body:            body(`{"identity":"owner@golftrack.test","password":"` + testPassword + `"}`),
		ExpectedStatus:  403,
		ExpectedContent: []string{`"status":403`},
	})
}

// TestPasswordLoginCanBeEnabledForProvisionedAccounts pins the other half: an
// environment without OAuth apps registered (the dev server, per
// deploy-dev.yml) can let pre-created accounts in with a password. Sign-*up*
// stays closed either way — see TestSignupIsOAuth2Only.
func TestPasswordLoginCanBeEnabledForProvisionedAccounts(t *testing.T) {
	t.Setenv(authenv.EnvAllowPasswordLogin, "true")

	run(t, anonymous, tests.ApiScenario{
		Name:           "password login for an existing account",
		Method:         http.MethodPost,
		URL:            authWithPasswordURL,
		Body:           body(`{"identity":"owner@golftrack.test","password":"` + testPassword + `"}`),
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"token":"`,
			`"email":"owner@golftrack.test"`,
			`"role":"USER"`,
		},
	})
}

// -----------------------------------------------------------------------------
// sign-in: record creation and the role
// -----------------------------------------------------------------------------

// TestFirstOAuth2SignInCreatesTheUserWithRoleUser is the gate item "a
// first-time sign-in lands with role = USER … through the real OAuth path
// rather than a direct save": the record does not exist before the request and
// carries the users.go default after it.
func TestFirstOAuth2SignInCreatesTheUserWithRoleUser(t *testing.T) {
	const email = "newcomer@golftrack.test"
	mockGoogle(t, oauth2User("google-newcomer", email, "New Comer"))

	run(t, anonymous, tests.ApiScenario{
		Name:           "first sign-in creates the user",
		Method:         http.MethodPost,
		URL:            authWithOAuth2URL,
		Body:           body(oauth2Login("")),
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"token":"`,
			`"email":"` + email + `"`,
			`"role":"USER"`,
			`"isNew":true`,
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if got := roleOf(t, app, email); got != collections.UserRoleUser {
				t.Errorf("stored role = %q, want %q", got, collections.UserRoleUser)
			}
		},
	})
}

// TestSecondOAuth2SignInUpdatesTheExistingUser is the other half of "users
// created on first successful OAuth sign-in, and updated on subsequent ones":
// the fixture's owner already exists, so the sign-in links the provider to that
// record instead of minting a second one.
func TestSecondOAuth2SignInUpdatesTheExistingUser(t *testing.T) {
	const email = "owner@golftrack.test"
	mockGoogle(t, oauth2User("google-owner", email, "Owner"))

	run(t, anonymous, tests.ApiScenario{
		Name:           "sign-in as an existing user",
		Method:         http.MethodPost,
		URL:            authWithOAuth2URL,
		Body:           body(oauth2Login("")),
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"id":"` + idOwner + `"`,
			`"email":"` + email + `"`,
			`"isNew":false`,
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			users, err := app.FindAllRecords(collections.NameUsers)
			if err != nil {
				t.Fatalf("list users: %v", err)
			}
			if len(users) != 3 {
				t.Errorf("user count = %d, want the 3 seeded ones — the sign-in created a duplicate", len(users))
			}
		},
	})
}

// TestAdminEmailsGrantsAdminOnFirstSignIn: an address on the list is an
// administrator from its very first sign-in, without a manual promotion in the
// Admin UI.
func TestAdminEmailsGrantsAdminOnFirstSignIn(t *testing.T) {
	const email = "chief@golftrack.test"
	t.Setenv(authenv.EnvAdminEmails, "someone@golftrack.test,"+email)
	mockGoogle(t, oauth2User("google-chief", email, "Chief"))

	run(t, anonymous, tests.ApiScenario{
		Name:           "first sign-in of a listed address",
		Method:         http.MethodPost,
		URL:            authWithOAuth2URL,
		Body:           body(oauth2Login("")),
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"email":"` + email + `"`,
			`"role":"ADMIN"`,
			`"isNew":true`,
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if got := roleOf(t, app, email); got != collections.UserRoleAdmin {
				t.Errorf("stored role = %q, want %q", got, collections.UserRoleAdmin)
			}
		},
	})
}

// TestAdminEmailsPromotesAnExistingUser is the gate item spelled out in #125:
// "including a user who was already USER before their address was added". The
// promotion has to happen on sign-in, because that is the only moment the
// application sees the address again.
func TestAdminEmailsPromotesAnExistingUser(t *testing.T) {
	const email = "owner@golftrack.test"
	t.Setenv(authenv.EnvAdminEmails, email)
	mockGoogle(t, oauth2User("google-owner", email, "Owner"))

	run(t, anonymous, tests.ApiScenario{
		Name:           "an existing USER whose address was just listed",
		Method:         http.MethodPost,
		URL:            authWithOAuth2URL,
		Body:           body(oauth2Login("")),
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"id":"` + idOwner + `"`,
			`"role":"ADMIN"`,
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if got := roleOf(t, app, email); got != collections.UserRoleAdmin {
				t.Errorf("stored role = %q, want %q", got, collections.UserRoleAdmin)
			}
		},
	})
}

// TestAdminEmailsRevokesAdminWhenTheAddressIsRemoved: the sync grants *and*
// revokes, the same as Django's sync_admin_role. Without this half, removing
// someone from ADMIN_EMAILS would leave them an administrator forever.
func TestAdminEmailsRevokesAdminWhenTheAddressIsRemoved(t *testing.T) {
	const email = "admin@golftrack.test" // the fixture's ADMIN, no longer listed
	t.Setenv(authenv.EnvAdminEmails, "someone-else@golftrack.test")
	mockGoogle(t, oauth2User("google-admin", email, "Admin"))

	run(t, anonymous, tests.ApiScenario{
		Name:           "a former admin signs in",
		Method:         http.MethodPost,
		URL:            authWithOAuth2URL,
		Body:           body(oauth2Login("")),
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"id":"` + idAdmin + `"`,
			`"role":"USER"`,
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if got := roleOf(t, app, email); got != collections.UserRoleUser {
				t.Errorf("stored role = %q, want %q", got, collections.UserRoleUser)
			}
		},
	})
}

// TestEmptyAdminEmailsLeavesRolesAlone is the safety valve DEPLOYMENT.md
// documents for the shipped app: an environment that forgets the variable must
// not strip every administrator on their next sign-in.
func TestEmptyAdminEmailsLeavesRolesAlone(t *testing.T) {
	const email = "admin@golftrack.test"
	t.Setenv(authenv.EnvAdminEmails, "")
	mockGoogle(t, oauth2User("google-admin", email, "Admin"))

	run(t, anonymous, tests.ApiScenario{
		Name:           "an admin signs in with an empty ADMIN_EMAILS",
		Method:         http.MethodPost,
		URL:            authWithOAuth2URL,
		Body:           body(oauth2Login("")),
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"id":"` + idAdmin + `"`,
			`"role":"ADMIN"`,
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if got := roleOf(t, app, email); got != collections.UserRoleAdmin {
				t.Errorf("stored role = %q, want %q", got, collections.UserRoleAdmin)
			}
		},
	})
}

// TestAdminEmailsIgnoresCaseAndSurroundingSpace matches how Django parses the
// same variable, so one list behaves identically for both applications during
// the dual-stack rollout.
func TestAdminEmailsIgnoresCaseAndSurroundingSpace(t *testing.T) {
	const email = "owner@golftrack.test"
	t.Setenv(authenv.EnvAdminEmails, "  Owner@GolfTrack.TEST , ")
	mockGoogle(t, oauth2User("google-owner", email, "Owner"))

	run(t, anonymous, tests.ApiScenario{
		Name:            "a listed address written in mixed case",
		Method:          http.MethodPost,
		URL:             authWithOAuth2URL,
		Body:            body(oauth2Login("")),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"role":"ADMIN"`},
	})
}

// -----------------------------------------------------------------------------
// what a caller may not smuggle into a sign-up
// -----------------------------------------------------------------------------

// TestOAuth2CreateDataCannotMintAnAdmin closes the escalation path that opens
// the moment OAuth2 sign-up is enabled: PocketBase merges the caller's
// createData into the record it creates, and the users create rule
// (@request.context = "oauth2") does not restrict its contents.
func TestOAuth2CreateDataCannotMintAnAdmin(t *testing.T) {
	const email = "sneaky@golftrack.test"
	mockGoogle(t, oauth2User("google-sneaky", email, "Sneaky"))

	run(t, anonymous, tests.ApiScenario{
		Name:           "createData asks for ADMIN",
		Method:         http.MethodPost,
		URL:            authWithOAuth2URL,
		Body:           body(oauth2Login(`,"createData":{"role":"ADMIN"}`)),
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"email":"` + email + `"`,
			`"role":"USER"`,
		},
		NotExpectedContent: []string{`"role":"ADMIN"`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if got := roleOf(t, app, email); got != collections.UserRoleUser {
				t.Errorf("stored role = %q, want %q", got, collections.UserRoleUser)
			}
		},
	})
}

// TestOAuth2CreateDataCannotSetAKnownPassword closes the other half of the same
// hole. PocketBase gives an OAuth2 sign-up a random password precisely so
// nobody knows it; a createData password would replace it with one the caller
// chose, ready to use the moment an environment turns password login on.
func TestOAuth2CreateDataCannotSetAKnownPassword(t *testing.T) {
	const (
		email          = "sneaky@golftrack.test"
		chosenPassword = "chosen-by-the-caller"
	)
	mockGoogle(t, oauth2User("google-sneaky", email, "Sneaky"))

	run(t, anonymous, tests.ApiScenario{
		Name:            "createData carries a password",
		Method:          http.MethodPost,
		URL:             authWithOAuth2URL,
		Body:            body(oauth2Login(`,"createData":{"password":"` + chosenPassword + `","passwordConfirm":"` + chosenPassword + `"}`)),
		ExpectedStatus:  200,
		ExpectedContent: []string{`"email":"` + email + `"`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			record, err := app.FindAuthRecordByEmail(collections.NameUsers, email)
			if err != nil {
				t.Fatalf("find user %q: %v", email, err)
			}
			if record.ValidatePassword(chosenPassword) {
				t.Error("the caller's createData password was accepted onto the new account")
			}
		},
	})
}

// TestOAuth2CreateDataCannotChooseAnotherAddress: the role decision is made
// from the address the provider verified, never from the record's own email, so
// a caller cannot sign in with one address while being provisioned under a
// listed one.
func TestOAuth2CreateDataCannotChooseAnotherAddress(t *testing.T) {
	const (
		verified = "sneaky@golftrack.test"
		listed   = "chief@golftrack.test"
	)
	t.Setenv(authenv.EnvAdminEmails, listed)
	mockGoogle(t, oauth2User("google-sneaky", verified, "Sneaky"))

	run(t, anonymous, tests.ApiScenario{
		Name:           "createData claims a listed address",
		Method:         http.MethodPost,
		URL:            authWithOAuth2URL,
		Body:           body(oauth2Login(`,"createData":{"email":"` + listed + `"}`)),
		ExpectedStatus: 200,
		ExpectedContent: []string{
			`"email":"` + verified + `"`,
			`"role":"USER"`,
		},
	})
}
