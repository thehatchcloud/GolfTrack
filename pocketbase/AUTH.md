# Authentication

How a person signs in to GolfTrack on PocketBase, how they become an
administrator, and what a deployment has to supply for either to work. Written
in Phase 4 (#125).

The reference implementation is the Django app: `config/settings.py`
(`SOCIALACCOUNT_PROVIDERS`, `ADMIN_EMAILS`, `ALLOW_PASSWORD_LOGIN`) and
`accounts/signals.py` (`sync_admin_role`). Behaviour is not supposed to change
in this migration, and where it does, the difference is called out below.

## The shape of it

| Concern | Django | PocketBase |
|---|---|---|
| Identity provider | django-allauth, Google + Microsoft | PocketBase's own OAuth2, same two providers |
| Session | Django session cookie | a JWT the client stores and sends as `Authorization` |
| Sign-up | `SOCIALACCOUNT_AUTO_SIGNUP`, self-service registration disabled | the `users` create rule, `@request.context = "oauth2"` |
| `role` default | `default=Role.USER` on the model | `internal/hooks/users.go` |
| `role` from `ADMIN_EMAILS` | `user_logged_in` receiver | `internal/hooks/adminrole.go`, on `OnRecordAuthWithOAuth2Request` |
| Password login | `DJANGO_ALLOW_PASSWORD_LOGIN`, default off | `GOLFTRACK_ALLOW_PASSWORD_LOGIN`, default off |

## Environment variables

Nothing here is committed: client secrets are secrets, client ids and the
admin list differ per environment, and a schema file that carried them would
have to be edited per deployment. `pb_schema.json` therefore holds the closed
baseline — no providers, OAuth2 disabled, password authentication disabled —
and `internal/hooks/authconfig.go` opens exactly what the environment
configures, at every startup. Same shape as the schema sync: one authoritative
source, applied deterministically on boot.

| Variable | Default | Effect |
|---|---|---|
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | unset | Registers the Google provider |
| `MICROSOFT_CLIENT_ID` / `MICROSOFT_CLIENT_SECRET` | unset | Registers the Microsoft Entra ID provider |
| `ADMIN_EMAILS` | unset | Comma-separated addresses granted `role = ADMIN` on sign-in |
| `GOLFTRACK_ALLOW_PASSWORD_LOGIN` | `false` | Allows email+password authentication for accounts that already exist |
| `GOLFTRACK_SCHEMA_SYNC` | `1` | Unrelated, listed for completeness: `0` skips the startup schema sync |

The names are Django's on purpose (`GOLFTRACK_ALLOW_PASSWORD_LOGIN` being the
one rename, from `DJANGO_ALLOW_PASSWORD_LOGIN`), so a host that already sets
them for the shipped app needs no new secrets when the PocketBase container
arrives in Phase 9 (#130). `ADMIN_EMAILS` is parsed the same way too —
lower-cased, trimmed, blanks dropped — so one list means the same thing to both
applications during a dual-stack rollout, and
`GOLFTRACK_ALLOW_PASSWORD_LOGIN` accepts the same truthy values Django's
`_bool_env` does (`1`, `true`, `yes`, `on`, case-insensitively).

Three behaviours worth knowing before debugging a deployment:

- **A provider with only one of its two variables set is skipped**, with a
  warning naming it. PocketBase rejects a provider config with a blank client
  id or secret, and rejecting it takes the whole collection save down — so a
  typo in one secret would otherwise disable the provider that *is* configured.
- **With no provider configured, OAuth2 is left disabled**, and
  `/api/collections/users/auth-with-oauth2` answers `403`. Enabling it with an
  empty provider list would only advertise a login that cannot succeed.
- **With no provider and no password login, nobody can sign in to the app**,
  which is what a fresh instance looks like. There is a startup warning saying
  so. The Admin UI is unaffected — superusers are a separate collection.

Changing any of these takes effect on the next start, except `ADMIN_EMAILS`,
which is read per sign-in.

## Registering the OAuth apps

PocketBase's redirect URI is its own, and differs from Django's, so the
existing OAuth client apps can be reused by *adding* a redirect URI rather than
registering new apps. Whether to reuse or register fresh is a deployment call;
the URI to add is the same either way.

| Provider | Console | Redirect URI to add |
|---|---|---|
| Google | console.cloud.google.com → APIs & Services → Credentials | `{origin}/api/oauth2-redirect` |
| Microsoft Entra ID | entra.microsoft.com → App registrations | `{origin}/api/oauth2-redirect` |

`{origin}` is the public origin of the PocketBase instance
(`http://127.0.0.1:8090` locally). `/api/oauth2-redirect` is PocketBase's own
handler, which is what the JS SDK's `authWithOAuth2` popup flow uses; a
frontend that drives the redirect itself instead registers its own callback URL
and passes it as `redirectURL`.

For Microsoft, register the app with the "any organizational directory and
personal Microsoft accounts" account type — PocketBase's provider uses the
`common` tenant endpoint, matching Django's `"TENANT": "common"`. It reads the
signed-in user's address from the Graph API's `mail` field, which is also what
allauth does, so the same accounts resolve to the same addresses across the
two stacks.

## Sign-in, step by step

1. The client asks `GET /api/collections/users/auth-methods` for the enabled
   providers and their authorization URLs.
2. The person authorizes at the provider and comes back with a code.
3. The client posts it to `POST /api/collections/users/auth-with-oauth2`.
4. PocketBase exchanges the code, fetches the profile, and looks for an
   existing account — first by the stored provider link, then by email.
5. `OnRecordAuthWithOAuth2Request` runs (`internal/hooks/adminrole.go`): the
   caller's `createData` is discarded and `role` is decided from
   `ADMIN_EMAILS`.
6. PocketBase creates the account if it is new — `role` already in the payload,
   or filled in as `USER` by `internal/hooks/users.go` — links the provider,
   and answers with `{token, record, meta}`.

Steps 5 and 6 are in that order deliberately: the role is settled *before* the
response is built, so the `record.role` a client reads straight out of the
login call is already correct. Django's `user_logged_in` receiver has the same
property.

### `role` and `ADMIN_EMAILS`

The rule is Django's, ported line for line:

- an address on the list gets `ADMIN`, on their first sign-in as much as their
  hundredth;
- an address *not* on the list gets `USER`, so removing someone from the list
  demotes them on their next sign-in rather than leaving a stale administrator
  behind;
- **an empty or unset `ADMIN_EMAILS` does nothing at all** — it is not "demote
  everyone". That safety valve is what stops an environment that forgets the
  variable from stripping every administrator, and it is documented for the
  shipped app in `DEPLOYMENT.md`.

The decision is made from the address the *provider* just verified, never from
the record's own email and never from anything the caller submitted. Those can
diverge, and only the provider-verified address is evidence of who is knocking.

Promotion still only happens on an OAuth sign-in. An account that has never
signed in since being listed keeps its old role, and the Admin UI remains the
manual path — the `users` update rule blocks self-assignment of `role`
(Phase 2, #123), so a superuser is the only other way to set it.

### What a caller may not smuggle in

`POST /auth-with-oauth2` takes an optional `createData` map, which PocketBase
merges into the record it creates for a first-time sign-in. The `users` create
rule (`@request.context = "oauth2"`) does not restrict its *contents*, so
without a guard it is a way to write arbitrary fields onto a brand-new account:

- `{"role": "ADMIN"}` would mint an administrator, and with it read access to
  every round in the database;
- `{"password": …}` would replace the random password PocketBase generates for
  OAuth2 sign-ups with one the caller chose — dormant until an environment
  turns `GOLFTRACK_ALLOW_PASSWORD_LOGIN` on, and an account takeover from that
  moment;
- `{"email": …}` would provision the account under an address the provider
  never verified.

GolfTrack discards `createData` entirely instead of filtering it: a new account
is exactly the provider's profile (email, name, avatar) plus the role decided
above, and the application has no field a client needs to seed. A sign-in that
carries one is logged and ignored, not rejected. `auth_test.go` covers all
three payloads.

## The password-login decision

**Recorded decision: self-service registration stays closed everywhere;
password *authentication* is available for already-provisioned accounts, behind
`GOLFTRACK_ALLOW_PASSWORD_LOGIN`, off by default.**

#125 framed this as a decision rather than a toggle because the two halves are
not the same switch:

- **Registration** is the `users` create rule. Relaxing it to admit password
  sign-up would reopen the account-minting half of a privilege-escalation path
  that Phase 2 closed deliberately, and any replacement rule would have to keep
  `role` unsettable by the client anyway. Nothing in GolfTrack needs walk-in
  accounts — the shipped app disables self-service signup in every environment,
  including the dev server — so the rule is unchanged:
  `@request.context = "oauth2"`. `acl_test.go`'s `TestSignupIsOAuth2Only` pins
  it, including with password login enabled.
- **Authentication** is `passwordAuth.enabled` on the collection. Django gates
  the equivalent behind `DJANGO_ALLOW_PASSWORD_LOGIN` so that an environment
  with no OAuth apps registered — the dev server, per `deploy-dev.yml` — can
  still be signed in to with accounts an operator created by hand. PocketBase
  gets the same switch, defaulting the same way, applied from the environment
  alongside the providers.

So a deployment can be OAuth-only (the default and what production runs),
OAuth + password, or password-only for an environment with no OAuth apps. It
can never be signed up to.

An operator creates an account for the password-only case from the Admin UI, or
with the binary's CLI. Nothing else changes: the account is an ordinary `users`
record, and its `role` is whatever the Admin UI sets, since `ADMIN_EMAILS` is
only consulted on an OAuth sign-in.

## For the frontend (Phase 7, #128)

Phase 7 owns the frontend adaptation; this is the auth slice of it, and the
contract it can build against.

**The session is a token, not a cookie.** `auth-with-oauth2` and
`auth-with-password` both answer `{"token": …, "record": {…}, "meta": {…}}`.
The client stores the token and sends it as `Authorization: <token>` on every
request; PocketBase's JS SDK does this out of the box, and its `authStore` can
be persisted to a cookie (`pb.authStore.exportToCookie()`) for a server-rendered
page to read. That is a difference from the Django app, where the session lives
in an `HttpOnly` cookie the JavaScript never sees, and it is the one auth
decision Phase 7 has to make explicitly:

- a **token in `localStorage`** is the SDK's default and the simplest, but is
  readable by any script on the origin;
- a **token in a cookie** the server also reads keeps server-side rendering
  possible and can be `Secure`, but not `HttpOnly` if the client has to send it
  too.

Either way the token is a JWT signed by the instance, valid for 5 days
(`authToken.duration` in `pb_schema.json`), refreshable via
`POST /api/collections/users/auth-refresh`.

What the frontend needs from this phase:

| Need | Where it comes from |
|---|---|
| Which providers to render sign-in buttons for | `GET /api/collections/users/auth-methods` — enabled providers, display names, logos, authorization URLs. Client secrets are never in it. |
| Whether to render a password form | the same response's `password.enabled` |
| The signed-in user | the `record` in the auth response, and `GET /api/collections/users/records/{id}` afterwards |
| Whether to show admin-only UI | `record.role == "ADMIN"` — already correct in the login response |
| Sign-out | discard the token; there is no server-side session to end |

`role` is advisory in the UI only. Every admin-gated read and write is enforced
by the access rules on the server (`@request.auth.role = "ADMIN"`), so a client
that lies to itself about the field gains nothing.

## Tests

`auth_test.go` drives the real endpoint — `POST
/api/collections/users/auth-with-oauth2` — with a fake provider swapped into
PocketBase's provider registry under Google's name. Everything downstream of the
token exchange is the production path: the same form, the same record lookup,
the same create rule, the same hooks. Using a real provider's name rather than
inventing a test one keeps the collection configured exactly as a deployment
with `GOOGLE_CLIENT_ID` set would have it.

| Test | Covers |
|---|---|
| `TestOAuth2ProvidersComeFromTheEnvironment` | both providers configured from their variables, secrets included |
| `TestApplyAuthConfigIsIdempotent` | a restart that changes nothing does not rewrite the collection |
| `TestHalfConfiguredProviderIsSkipped` | one missing secret does not take the other provider down |
| `TestAuthMethodsAdvertiseTheProvidersWithoutSecrets` | what the frontend reads, and what it must never contain |
| `TestOAuth2IsRefusedWhenNoProviderIsConfigured` | the closed baseline |
| `TestPasswordLoginIsOffByDefault` / `…CanBeEnabledForProvisionedAccounts` | both halves of the recorded decision |
| `TestFirstOAuth2SignInCreatesTheUserWithRoleUser` | the gate item: `role = USER` through the real OAuth path |
| `TestSecondOAuth2SignInUpdatesTheExistingUser` | a repeat sign-in links, and does not duplicate |
| `TestAdminEmailsGrantsAdminOnFirstSignIn` | promotion of a brand-new account |
| `TestAdminEmailsPromotesAnExistingUser` | the gate item: a `USER` whose address was added later |
| `TestAdminEmailsRevokesAdminWhenTheAddressIsRemoved` | the revocation half Django also has |
| `TestEmptyAdminEmailsLeavesRolesAlone` | the safety valve |
| `TestAdminEmailsIgnoresCaseAndSurroundingSpace` | parsing parity with Django |
| `TestOAuth2CreateDataCannotMintAnAdmin` / `…SetAKnownPassword` / `…ChooseAnotherAddress` | the three `createData` payloads |
| `internal/authenv/authenv_test.go` | the parsing itself, as plain unit tests |
| `acl_test.go` — `TestRoleIsNotSelfAssignable`, `TestSignupIsOAuth2Only` | unchanged in substance: a non-admin still cannot self-assign `ADMIN`, and sign-up is still OAuth2-only |

What the automated tests deliberately do **not** cover is the token exchange
itself — a real round trip to Google and to Microsoft. That is a live-credential
check, listed in `pocketbase/README.md` among the things the owner verifies.
