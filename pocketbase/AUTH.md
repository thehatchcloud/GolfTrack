# Authentication

How a person signs in to GolfTrack on PocketBase, how they become an
administrator, and what a deployment has to supply for either to work.

## The shape of it

| Concern | How |
|---|---|
| Identity provider | PocketBase's own OAuth2, Google + Microsoft Entra ID |
| Session | a JWT the client stores and sends as `Authorization` |
| Sign-up | the `users` create rule, `@request.context = "oauth2"` |
| `role` default | `internal/hooks/users.go` |
| `role` from `ADMIN_EMAILS` | `internal/hooks/adminrole.go`, on `OnRecordAuthWithOAuth2Request` |
| Password login | `GOLFTRACK_ALLOW_PASSWORD_LOGIN`, default off |

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

`ADMIN_EMAILS` is parsed lower-cased, trimmed, with blanks dropped.
`GOLFTRACK_ALLOW_PASSWORD_LOGIN` accepts common truthy values (`1`, `true`,
`yes`, `on`, case-insensitively).

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
`common` tenant endpoint. It reads the signed-in user's address from the
Graph API's `mail` field.

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
login call is already correct.

### `role` and `ADMIN_EMAILS`

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
manual path — the `users` update rule blocks self-assignment of `role`, so a
superuser is the only other way to set it.

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

This is a decision rather than a toggle because the two halves are not the
same switch:

- **Registration** is the `users` create rule. Relaxing it to admit password
  sign-up would reopen a privilege-escalation path, and any replacement rule
  would have to keep `role` unsettable by the client anyway. Nothing in
  GolfTrack needs walk-in accounts — the shipped app disables self-service
  signup in every environment, including the dev server — so the rule is
  unchanged: `@request.context = "oauth2"`. `acl_test.go`'s
  `TestSignupIsOAuth2Only` pins it, including with password login enabled.
- **Authentication** is `passwordAuth.enabled` on the collection, gated behind
  `GOLFTRACK_ALLOW_PASSWORD_LOGIN` so that an environment with no OAuth apps
  registered — the dev server, per `deploy-dev.yml` — can still be signed in
  to with accounts an operator created by hand.

So a deployment can be OAuth-only (the default and what production runs),
OAuth + password, or password-only for an environment with no OAuth apps. It
can never be signed up to.

An operator creates an account for the password-only case from the Admin UI, or
with the binary's CLI. Nothing else changes: the account is an ordinary `users`
record, and its `role` is whatever the Admin UI sets, since `ADMIN_EMAILS` is
only consulted on an OAuth sign-in.

## For the frontend

This is the auth slice the frontend builds against.

**The session is a token, not a cookie.** `auth-with-oauth2` and
`auth-with-password` both answer `{"token": …, "record": {…}, "meta": {…}}`.
The client stores the token and sends it as `Authorization: <token>` on every
request; PocketBase's JS SDK does this out of the box, and its `authStore` can
be persisted to a cookie (`pb.authStore.exportToCookie()`) for a server-rendered
page to read. Two options were on the table:

- a **token in `localStorage`** is the SDK's default and the simplest, but is
  readable by any script on the origin;
- a **token in a cookie** the server also reads keeps server-side rendering
  possible and can be `Secure`, but not `HttpOnly` if the client has to send it
  too.

Either way the token is a JWT signed by the instance, valid for 5 days
(`authToken.duration` in `pb_schema.json`), refreshable via
`POST /api/collections/users/auth-refresh`.

**Decision: a cookie, not `localStorage`.** The frontend is server-rendered
with Alpine.js islands rather than a client-side SPA, and needs the token on
the *server* request too, not just the browser's `fetch` calls —
`localStorage` cannot do that without an extra round trip through JavaScript
on every page load. `pb.authStore.exportToCookie()` writes the same JWT into
a cookie the server can read directly, so a page render and an API call
authenticate the same way. The cookie is `Secure` and `SameSite=Lax`, but
cannot be `HttpOnly`: the client-side SDK has to read it back out to set the
`Authorization` header itself, since PocketBase's API does not accept the
cookie as credentials directly. That is the trade-off the two bullets above
describe, made explicit: this accepts "readable by the page's own scripts" as
the cost of keeping the server able to gate a page render before any
JavaScript runs.

`internal/web/auth.go` reads the cookie server-side and
`static/js/golftrack.js` writes it (through `authStore.exportToCookie`) and
attaches the token to every API call. Two details worth knowing:

- **Only the token is trusted.** `exportToCookie` writes `{"token": …,
  "record": {…}}`, and the record half is as forgeable as anything else the
  client holds. The server ignores it: it verifies the token and re-reads the
  user, so a cookie edited to `"role":"ADMIN"` opens no admin page
  (`TestTamperedCookieRecordCannotGrantAdmin`).
- **`Secure` follows the scheme.** The cookie is written `Secure` over HTTPS and
  not over plain HTTP, because a `Secure` cookie is silently dropped on
  `http://localhost` and development would have no session at all. Production is
  HTTPS, so production gets `Secure`.

Sign-in itself uses the SDK's popup OAuth2 flow
(`authWithOAuth2({provider})`), which completes against `/api/oauth2-redirect` —
the redirect URI this document already tells you to register. The alternative
noted above (a frontend that drives the redirect itself and registers its own
callback) stays available if the popup proves awkward in a particular mobile
browser; it would need a second redirect URI added to each OAuth app.

What the frontend needs:

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
| `TestFirstOAuth2SignInCreatesTheUserWithRoleUser` | `role = USER` through the real OAuth path |
| `TestSecondOAuth2SignInUpdatesTheExistingUser` | a repeat sign-in links, and does not duplicate |
| `TestAdminEmailsGrantsAdminOnFirstSignIn` | promotion of a brand-new account |
| `TestAdminEmailsPromotesAnExistingUser` | a `USER` whose address was added later |
| `TestAdminEmailsRevokesAdminWhenTheAddressIsRemoved` | the revocation half |
| `TestEmptyAdminEmailsLeavesRolesAlone` | the safety valve |
| `TestAdminEmailsIgnoresCaseAndSurroundingSpace` | parsing normalises case and whitespace |
| `TestOAuth2CreateDataCannotMintAnAdmin` / `…SetAKnownPassword` / `…ChooseAnotherAddress` | the three `createData` payloads |
| `internal/authenv/authenv_test.go` | the parsing itself, as plain unit tests |
| `acl_test.go` — `TestRoleIsNotSelfAssignable`, `TestSignupIsOAuth2Only` | a non-admin still cannot self-assign `ADMIN`, and sign-up is still OAuth2-only |

What the automated tests deliberately do **not** cover is the token exchange
itself — a real round trip to Google and to Microsoft. That is a live-credential
check, listed in `pocketbase/README.md` among the things the owner verifies.
