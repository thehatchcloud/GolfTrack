# Deployment Guide

Pushes to `main` automatically test, build, and deploy the **PocketBase** app to the exe.dev VM via the workflow in `.github/workflows/deploy.yml`.

Non-`main` branches deploy to the **dev server** (`golftrack-dev.exe.xyz`) via `.github/workflows/deploy-dev.yml`. See [Dev server setup](#dev-server-setup-golftrack-devexexyz) below.

> **Stack note:** As of the Phase 10 cutover (#131), the deployed artifact is the **PocketBase** app — `pocketbase/Dockerfile` → a single static Go binary (PocketBase as a framework, embedded schema, hooks and frontend) plus Litestream. CI builds that image from the `pocketbase/` context and both servers run it.
>
> The Django app it replaced (root `Dockerfile` → Python 3.13, gunicorn, WhiteNoise) is still in-tree until Phase 11 (#132) removes it, and its last image stays in GHCR as `ghcr.io/<owner>/golftrack:django-latest` for rollback — see [Rollback to Django](#rollback-to-django). The legacy Next.js app is neither built nor deployed.
>
> Sections below that describe Django specifics are kept only where the rollback path needs them; PocketBase's own container reference — environment variables and what each Django variable maps to (or doesn't) — is [`pocketbase/DEPLOYMENT.md`](pocketbase/DEPLOYMENT.md).

---

## Dev server setup (`golftrack-dev.exe.xyz`)

The dev server runs the PocketBase app in Docker (`pocketbase/Dockerfile`) — same image as
production, but **without Litestream** (`LITESTREAM_BUCKET` unset) and with OAuth
disabled — sign in with email + password. The image is tagged
`ghcr.io/<owner>/golftrack:pocketbase-dev` and rebuilt on every push.

The container is named `golftrack-pb-dev` and listens on **8090**, published as host
port 8000 so the public URL is unchanged. The deploy script removes the old Django
container (`golftrack-dev`) as its first act, so only one of the two apps can ever be
serving; the Django dev image (`:django-dev`) and its named volume
(`golftrack-dev-data`) are left in place as the dev-side rollback.

### 1. Create the dev VM

Use the `exeuntu` image (includes Docker):

```bash
ssh exe.dev new --name golftrack-dev
```

### 2. Generate a deploy SSH key

```bash
ssh-keygen -t ed25519 -C "github-actions-golftrack-dev" -f ~/.ssh/golftrack_dev_deploy
cat ~/.ssh/golftrack_dev_deploy.pub | ssh exe.dev ssh-key add
```

The private key (`~/.ssh/golftrack_dev_deploy`) becomes the `DEV_DEPLOY_SSH_KEY` secret.

### 3. One-time server setup

None — Docker is already installed on `exeuntu` and the workflow uses a named Docker volume (`golftrack-pb-dev-data`) that Docker creates automatically on first run.

After the first workflow run completes, create a test account. PocketBase keeps
self-service signup OAuth2-only regardless of `GOLFTRACK_ALLOW_PASSWORD_LOGIN`
(`pocketbase/AUTH.md` § "The password-login decision"), so the dev account is made by
hand: create a superuser, then add the app user from the Admin UI.

```bash
# 1. A superuser for the Admin UI, at /_/ on the dev server
docker exec golftrack-pb-dev ./golftrack-pb superuser upsert your@email.com 'choose-a-password' --dir /data

# 2. In the Admin UI, add a record to the `users` collection with your email,
#    a password, and role = ADMIN. Then sign in at /accounts/login/.
```

PocketBase also prints a one-time installer link (`{origin}/_/#/pbinstall/<token>`)
at startup when no superuser exists, but in a container it goes to `docker logs`,
names the container-internal origin `http://0.0.0.0:8090`, and the token expires
after 30 minutes — so `superuser upsert` above is the practical route. It is also
the password-reset path later.

> **`:8000` in the dev URLs below is the host port, not necessarily the public
> one.** Production turned out to be served by exe.dev on the default HTTPS port
> with no port in the URL (see [Getting `{origin}` right](#getting-origin-right));
> the dev entries here still carry `:8000` and have not been re-checked. It costs
> nothing on dev — no OAuth is registered there — but don't copy these as origins.

### 4. GitHub Actions secrets for dev

**Settings → Secrets and variables → Actions → New repository secret**

| Secret | Value |
|--------|-------|
| `DEV_DEPLOY_HOST` | `golftrack-dev.exe.xyz` |
| `DEV_DEPLOY_SSH_KEY` | Contents of `~/.ssh/golftrack_dev_deploy` (private key) |

`ADMIN_EMAILS` and `GHCR_TOKEN` are already set from the production setup — reused as-is.

`DEV_DJANGO_SECRET_KEY` was required by the Django dev container and is no longer
passed to anything: PocketBase keeps its encryption key in the data directory rather
than taking one from the environment. Leave the secret in place until the Django
rollback path is retired in Phase 11, then delete it.

### How it works

Every push to a non-`main` branch triggers the workflow:

1. Builds the PocketBase Docker image from the `pocketbase/` context (static Go binary, embedded schema/frontend, Litestream)
2. Pushes to `ghcr.io/<owner>/golftrack:pocketbase-dev`
3. SSHs to the dev VM: pulls the image, removes the Django container if it is still there, stops/removes the old PocketBase container, starts a new one
4. The container entrypoint starts `golftrack-pb serve` on port 8090 (no Litestream, since `LITESTREAM_BUCKET` is unset). There is no separate migrate step — the binary reconciles the database to its embedded `pb_schema.json` during startup
5. The script polls `/api/version` inside the container until it reports this commit's short SHA, then prints `docker ps` and asserts the Django container is gone

The dev database persists in the `golftrack-pb-dev-data` Docker named volume across deploys — branch switches don't wipe it. For a clean slate: `docker stop golftrack-pb-dev && docker volume rm golftrack-pb-dev-data && docker start golftrack-pb-dev`.

---

## Deployment model (production)

- PocketBase app served by the **`golftrack-pb` binary** inside a Docker container — it is its own HTTP server, and the frontend (templates, Tailwind CSS, vendored JS) is `go:embed`'d into it, so there is no WSGI server and no static-file middleware
- SQLite databases on a persistent directory on the VM (`/data/golftrack-pb/` → `/data` in the container). PocketBase keeps **two**: `data.db` (the six collections) and `auxiliary.db` (request/cron logs)
- **Litestream** runs as the container's supervising process, continuously replicating both databases to an S3-compatible bucket and restoring them on first boot
- Container restarts automatically when the VM reboots (`--restart unless-stopped`)
- There is no migrate step: the binary reconciles the database to its embedded `pb_schema.json` during startup, in the same process Litestream supervises
- The binary listens on port **8090** inside the container; the deploy maps host port **3000 → 8090**, unchanged from both the Next.js and Django eras. **The host port is not part of the public URL** — exe.dev fronts the VM and serves the app at `https://<vmname>.exe.xyz/` on the default HTTPS port. That distinction matters when registering OAuth redirect URIs; see [Getting `{origin}` right](#getting-origin-right)
- The deploy script health-checks `/api/version` inside the container after `docker run` and **fails the deploy (with `docker logs`) if the app doesn't come up serving this commit's SHA**. `/api/version` rather than `/api/health` because PocketBase's own health endpoint carries no build identity, so it cannot distinguish the new container from a stale one
- The container is named `golftrack-pb`. The deploy script removes the Django container (`golftrack`) before starting it and fails if anything named `golftrack` survives, so "both apps running" is not a reachable state

---

## Cutover to PocketBase (one-time, #131)

Per #127 (*"No data was ever entered into the database"*), there is **no data migration**: the PocketBase app comes up on an empty database built from its embedded schema. That makes the cutover a container swap.

`bin/deploy-prod.sh` does the whole thing on the first push to `main` after the cutover merges — no maintainer step is required:

1. Pulls `ghcr.io/<owner>/golftrack:latest`, which is now the PocketBase image.
2. Stops and removes the `golftrack` (Django) container. It holds host port 3000, so it cannot coexist with the new one.
3. Creates `/data/golftrack-pb`, `chown`ed to uid/gid 1001 — the same non-root `app` user constraint the Django image had on `/data/golftrack`, because a bind mount's host ownership overrides the image's.
4. Starts `golftrack-pb` on 3000 → 8090 and health-checks it.
5. Prints `docker ps` and fails if the Django container is still present.

Three things keep the two apps' state apart, which is what makes the rollback below a swap rather than a restore:

- **Separate data directory.** PocketBase uses `/data/golftrack-pb`; Django's `/data/golftrack/prod.db` is never touched.
- **Separate replica paths.** `pocketbase/litestream.yml` replicates to `pocketbase/data` and `pocketbase/auxiliary` in the bucket, distinct from the Django app's `django` path, so the two generations never collide.
- **Separate GHCR tags.** The `build` job copies the outgoing Django `:latest` to `:django-latest` before overwriting `:latest` — once, and never again, so the tag keeps pointing at the last Django image instead of drifting forward.

### Before merging the cutover

- [ ] Register the PocketBase redirect URI `{origin}/api/oauth2-redirect` on **both** OAuth client apps, where `{origin}` is the public origin **without** the deploy's host port — see [Getting `{origin}` right](#getting-origin-right), which gives the one-line console expression that produces the exact string. Add it alongside the Django callback URLs rather than replacing them, so a rollback still authenticates.
- [ ] Confirm the dev deploy is green and `golftrack-dev.exe.xyz:8000` serves the PocketBase app (this is what every branch push exercises).
- [ ] Take a final backup of the Django database if you want one for the archive — `/data/golftrack/prod.db` on the VM. It survives the cutover either way; this is belt and braces.

### After the deploy goes green

- [ ] Sign in with a real Google account and a real Microsoft account.
- [ ] End-to-end: create a course, start a round, record shots, complete it, view the review page.
- [ ] `docker ps` on the VM shows `golftrack-pb` and no `golftrack`.
- [ ] Check Litestream is replicating: `docker logs golftrack-pb | grep -i litestream`, and confirm objects appear under `pocketbase/data` in the bucket.
- [ ] Watch logs for 24 hours (`docker logs -f golftrack-pb`).

## Rollback to Django

Recovery is a container swap, well inside the <30 minute budget in #131: the Django database and its Litestream replica are untouched by the cutover, so nothing has to be restored.

```bash
ssh <prod-vmhost>
# copy bin/rollback-prod.sh over first (scp, or curl it from the repo)
GHCR_TOKEN=… OWNER=thehatchcloud \
DJANGO_SECRET_KEY=… DEPLOY_HOST=<the DEPLOY_HOST secret's value> \
GOOGLE_CLIENT_ID=… GOOGLE_CLIENT_SECRET=… \
MICROSOFT_CLIENT_ID=… MICROSOFT_CLIENT_SECRET=… \
ADMIN_EMAILS=… \
LITESTREAM_ACCESS_KEY_ID=… LITESTREAM_SECRET_ACCESS_KEY=… \
LITESTREAM_BUCKET=… LITESTREAM_ENDPOINT=… \
sh rollback-prod.sh
```

The values are the same GitHub Actions secrets `deploy.yml` passes; read them out of the Actions secret store (or your password manager) before you start, since a rollback is a bad time to go looking. The script pulls `:django-latest`, stops `golftrack-pb`, starts `golftrack` against `/data/golftrack` on port 3000 → 8000, and health-checks `/api/health`.

Two things it deliberately does not do:

- **It leaves `/data/golftrack-pb` in place**, so whatever the PocketBase app recorded while it was live is still there to inspect (and still in the bucket under `pocketbase/*`).
- **It does not roll back the repository.** `main` still builds the PocketBase image, so the next push re-deploys it. Revert the cutover commit, or disable the deploy workflow, if the rollback is meant to hold.

> **Test it in dev first.** The dev equivalent is smaller — `docker stop golftrack-pb-dev && docker rm golftrack-pb-dev`, then `docker run` the `:django-dev` image against the still-present `golftrack-dev-data` volume — but it exercises the same "the old container's state was never touched" assumption this plan rests on.

---

## Cutover from the Next.js app (one-time, historical)

> Kept for the record — this was done once, when Django replaced Next.js (#94). The PocketBase cutover above supersedes it; nothing here needs doing again.

Per the rewrite decision (#85: *start fresh, no data migration; Litestream history from the old app is abandoned*), the Django app must come up on a **clean database built from migrations**, not the old Next.js/Prisma SQLite file left on the VM.

Three things keep them separate:

- **Fresh replica path.** `litestream.yml` replicates to the `django` path in the bucket, not the Next.js app's `prod` path. The old `prod` objects are orphaned and can be deleted from the bucket whenever convenient.
- **Data-directory ownership.** The Django container runs as the non-root `app` user (uid/gid **1001**). On a bind mount (`-v /data/golftrack:/data`) the host directory's ownership wins over the image's, so if `/data/golftrack` is owned by another user the app can't write `/data/prod.db` and `migrate` crashes on boot with `attempt to write a readonly database`. The deploy script now runs `sudo chown -R 1001:1001 /data/golftrack` before `docker run`; the very first Django deploy on a VM that previously ran the Next.js app needs this to reclaim the old files. (This is why the dev server — which uses a Docker **named volume** that inherits the image's ownership — was unaffected.)
- **Wiping the stale local DB.** The persistent volume (`/data/golftrack/`) still holds the old Prisma `prod.db`. Remove it once so the container's entrypoint restores nothing (the `django` replica is empty on first boot) and `migrate` builds the schema from scratch.

One-time cleanup on the prod VM, then redeploy:

```bash
ssh <prod-vmhost>
docker stop golftrack 2>/dev/null || true
docker rm   golftrack 2>/dev/null || true
sudo rm -f /data/golftrack/prod.db /data/golftrack/prod.db-wal /data/golftrack/prod.db-shm
sudo chown -R 1001:1001 /data/golftrack   # let the container's app user write /data
```

Then redeploy (**Actions → CI / Deploy → Run workflow**). The container boots against an empty `django` replica → fresh `/data/prod.db` → `migrate` → gunicorn under Litestream. The deploy's health check confirms the app actually serves before the job goes green. Because the previous image stays in GHCR, rollback is re-deploying the prior tag; the abandoned `prod` replica is still there if the old app ever needs it.

> This is a **maintainer-run** step (SSH + Docker on the VM). The app image itself is validated in CI; wiping the volume is a deliberate, destructive action left to you.

---

## One-time setup

### 1. Create the exe.dev VM

Start a VM using the `exeuntu` image, which includes Docker:

```bash
ssh exe.dev new --name golftrack
```

Note the VM hostname (`golftrack.exe.xyz` or similar) — this is `DEPLOY_HOST`.

### 2. Generate a deploy SSH key

Generate a key pair for GitHub Actions to use:

```bash
ssh-keygen -t ed25519 -C "github-actions-golftrack" -f ~/.ssh/golftrack_deploy
```

Register the public key with exe.dev:

```bash
cat ~/.ssh/golftrack_deploy.pub | ssh exe.dev ssh-key add
```

The private key (`~/.ssh/golftrack_deploy`) becomes the `DEPLOY_SSH_KEY` secret.

### 3. Generate a GHCR token

Create a GitHub personal access token (classic) with the `read:packages` scope at
**GitHub → Settings → Developer settings → Personal access tokens**.

This is the `GHCR_TOKEN` secret — it lets the VM pull the container image from
GitHub Container Registry.

### 4. Set GitHub Actions secrets

In the repository: **Settings → Secrets and variables → Actions → New repository secret**

| Secret | Value |
|--------|-------|
| `DEPLOY_HOST` | exe.dev VM hostname, e.g. `golftrack.exe.xyz` |
| `DEPLOY_SSH_KEY` | Contents of `~/.ssh/golftrack_deploy` (the private key) |
| `GHCR_TOKEN` | PAT with `read:packages` scope |
| `DJANGO_SECRET_KEY` | Generate with `python3 -c "import secrets,base64;print(base64.urlsafe_b64encode(secrets.token_bytes(50)).decode())"` |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | Django's own Google OAuth client (see [OAuth provider setup](#oauth-provider-setup)) |
| `MICROSOFT_CLIENT_ID` / `MICROSOFT_CLIENT_SECRET` | Django's own Microsoft Entra ID app |
| `ADMIN_EMAILS` | Comma-separated emails to grant `ADMIN` role at sign-in |
| `LITESTREAM_ACCESS_KEY_ID` / `LITESTREAM_SECRET_ACCESS_KEY` / `LITESTREAM_BUCKET` / `LITESTREAM_ENDPOINT` | See [Backups (Litestream)](#backups-litestream) |

`DEPLOY_HOST` and `DJANGO_SECRET_KEY` are no longer passed to the deploy — the
PocketBase container needs neither. They are kept only for
[`bin/rollback-prod.sh`](bin/rollback-prod.sh), which still starts Django and
derives `DJANGO_ALLOWED_HOSTS` and `DJANGO_CSRF_TRUSTED_ORIGINS` from
`DEPLOY_HOST`. Delete both secrets with the rollback path in Phase 11.

> **Note on the rollback script's CSRF origin.** It sets
> `DJANGO_CSRF_TRUSTED_ORIGINS="https://$DEPLOY_HOST:3000"`, carried over verbatim
> from the Django deploy it replaced. Now that we know the public origin carries no
> port (see [Getting `{origin}` right](#getting-origin-right)), that value looks
> wrong — a rolled-back Django would reject form posts from the real origin. It is
> untested either way. If you ever exercise the rollback, set
> `DJANGO_CSRF_TRUSTED_ORIGINS` to the portless origin, or list both
> comma-separated.

> **Migrating from the Next.js deploy:** The old workflow used `AUTH_SECRET`,
> `AUTH_URL`, `AUTH_GOOGLE_*`, and `AUTH_MICROSOFT_ENTRA_ID_*`. The Django deploy
> does **not** use these — add the `DJANGO_SECRET_KEY` and `GOOGLE_*` /
> `MICROSOFT_*` secrets above before the first Django deploy. Django uses its own
> OAuth client apps (separate redirect URIs), so the old `AUTH_*` secrets can be
> left in place for rollback and removed after cutover.

### 5. Make the GHCR package visible to the VM

After the first successful workflow run, a package named `golftrack` will appear under
your GitHub org/account. The `GHCR_TOKEN` PAT must belong to a user with access to
pull from it. If you prefer, you can set the package visibility to **Public** in
**GitHub → Packages → golftrack → Package settings**, which eliminates the need for
`GHCR_TOKEN` authentication (remove the login line in the workflow script).

---

## What happens on each push to `main`

1. **PocketBase (Go)** — `gofmt`, `go vet`, `go build` and the full Go test suite (schema, access rules, hooks, API parity, frontend). This job **gates the build**
2. **Lint, build, and test (Django, legacy)** — the pytest suite, still run because the Django code is in-tree until Phase 11 (#132), but it no longer gates anything
3. **Build** — builds the PocketBase Docker image from the `pocketbase/` context and pushes it to `ghcr.io/<owner>/golftrack:latest` and `:pocketbase-<sha>`. On its first run it also copies the outgoing Django `:latest` to `:django-latest`
4. **Deploy** — SSHes into the exe.dev VM (`bin/deploy-prod.sh`):
   - pulls the new image
   - removes the Django container if it is still there
   - stops and removes the old PocketBase container
   - starts the new container (Litestream restores the DBs if missing, then `golftrack-pb serve` starts and syncs the schema itself)
   - health-checks `/api/version` for this commit's SHA, prints `docker ps`, prunes old images

The workflow also supports manual triggering: **Actions → CI / Deploy → Run workflow**. Use this when you've rotated a secret (e.g. `LITESTREAM_*`) and need to restart the container to pick up the new value without pushing a code change.

---

## Required environment variables

Set directly in the `docker run` command in the deploy workflow — no `.env` file is needed on the VM.

| Variable | Value |
|----------|-------|
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | Google OAuth client credentials |
| `MICROSOFT_CLIENT_ID` / `MICROSOFT_CLIENT_SECRET` | Microsoft Entra ID app credentials |
| `ADMIN_EMAILS` | Comma-separated emails to grant `ADMIN` role at sign-in |
| `LITESTREAM_*` | Replication credentials/endpoint — see [Backups](#backups-litestream) |

The cutover **removed** four variables the Django container needed. None has a
PocketBase equivalent, and passing them is a no-op rather than an error:
`DJANGO_SECRET_KEY` (PocketBase keeps its encryption key in the data directory),
`DJANGO_ALLOWED_HOSTS` (it does not validate the `Host` header),
`DJANGO_CSRF_TRUSTED_ORIGINS` (writes are token-authenticated JSON calls, not
cookie-authenticated form posts, so there is no CSRF token to protect) and
`DATABASE_URL` (the data directory passed as `--dir /data` plays that role). The
full mapping, including `GOLFTRACK_ALLOW_PASSWORD_LOGIN` and
`GOLFTRACK_SCHEMA_SYNC`, is in
[`pocketbase/DEPLOYMENT.md`](pocketbase/DEPLOYMENT.md) § "Environment variables".

They are still listed in the `deploy.yml` secrets store because
[`bin/rollback-prod.sh`](bin/rollback-prod.sh) needs them; delete them with the
rollback path in Phase 11.

### Changing an environment variable in production

Every variable in the table above is passed to `docker run` by the deploy
workflow, and the app reads it at process start. **Editing a GitHub Actions
secret does not change the running app** — the container keeps the value it was
started with until it is recreated. Two steps are always required:

1. Update the secret under **Settings → Secrets and variables → Actions**.
2. Recreate the container — **Actions → CI / Deploy → Run workflow**, or push to
   `main`.

There is no way to change these values by editing a file in the repo; the
production values live only in the Actions secrets store.

#### `ADMIN_EMAILS` specifically

`ADMIN_EMAILS` is a **comma-separated list**, not a single address
(`alice@example.com,bob@example.com`). It is parsed into a lowercased set by
`pocketbase/internal/authenv`, so whitespace around the commas and letter case do
not matter. Unlike Django's, it is read **per sign-in** rather than cached at
startup — but the container still only sees the value it was started with, so
the recreate step above is unchanged.

`syncAdminRole` in `pocketbase/internal/hooks/adminrole.go` runs on **every OAuth2
sign-in** and both grants *and* revokes: a user whose provider-verified email is in
the list is promoted to `ADMIN`, and any other user is demoted to `USER`. It is a
direct port of Django's `sync_admin_role` receiver, so the same two consequences
apply when editing the secret:

- **Replacing the value silently demotes anyone you drop from the list** — they
  lose `ADMIN` at their next sign-in. To keep an existing admin while adding a
  new one, append to the list rather than overwriting it.
- **Changes take effect per user at their next login**, not at deploy time. An
  already-signed-in admin keeps the role until their session ends and they sign
  in again.

As a safety valve, an empty `ADMIN_EMAILS` makes the receiver a no-op — it
leaves existing roles alone instead of demoting every user on the site.

Note that the dev deploy reads the **same** `ADMIN_EMAILS` repository secret
(see [GitHub Actions secrets for dev](#4-github-actions-secrets-for-dev)), so a
change applies to `golftrack-dev.exe.xyz` as well once that container is
recreated.

### OAuth provider setup

PocketBase reuses the **same OAuth client apps** as Django — same
`GOOGLE_*`/`MICROSOFT_*` credentials — but its redirect URI is its own, so the
client registrations need one more URI **added** (not replaced):

```text
{origin}/api/oauth2-redirect
```

- **Google** — [console.cloud.google.com/apis/credentials](https://console.cloud.google.com/apis/credentials),
  under **Authorized redirect URIs**. (Not "Authorized JavaScript origins" — that
  is a different field on the same page, and putting it there does nothing.)
- **Microsoft Entra ID** — [entra.microsoft.com → App registrations](https://entra.microsoft.com),
  under the **Web** platform. Not "Single-page application": PocketBase exchanges
  the code server-side with the client secret, and the SPA platform requires PKCE
  and rejects that.
  Audience: "Accounts in any organizational directory and personal Microsoft accounts" (required for the default `common` tenant to accept both work and personal accounts)

#### Getting `{origin}` right

**`{origin}` is the origin your browser shows on the sign-in page — which does
not include the deploy's `3000`.** That number is the *host* port on the VM;
exe.dev fronts it and serves the app on the default HTTPS port, so the public
origin is `https://<host>.exe.xyz`. Registering the URI with `:3000` in it is a
mismatch, and Google rejects the sign-in with `Error 400: redirect_uri_mismatch`.

Don't derive it by hand. The frontend builds the redirect URI from
`window.location.origin` (`internal/web/static/js/golftrack.js` calls
`authWithOAuth2` with no `redirectURL` override, so the JS SDK uses
`client.buildURL('/api/oauth2-redirect')` on a client constructed as
`new PocketBase(window.location.origin)`). So open the app's sign-in page,
open the browser console, and run the same expression the SDK does:

```js
window.location.origin + '/api/oauth2-redirect'
```

Register exactly what that prints. To debug a mismatch, compare it against the
`redirect_uri` Google reports under **Error details** on its error page.

Two things that look like a wrong URI but are not: Google takes anywhere from
5 minutes to a few hours to apply credential changes, and a URI added to a
different OAuth client in the same project has no effect — the app
authenticates with whatever `GOOGLE_CLIENT_ID` is in the Actions secret.

`/api/oauth2-redirect` is PocketBase's own endpoint — nothing in `pocketbase/`
implements it. Details and the local-development URI
(`http://127.0.0.1:8090/api/oauth2-redirect`, where the port *is* part of the
origin) are in [`pocketbase/AUTH.md`](pocketbase/AUTH.md).

The dev server needs no redirect URI at all: `bin/deploy-dev.sh` passes no
`GOOGLE_*`/`MICROSOFT_*` variables, so no providers are registered there and
sign-in is password-only.

**Keep the Django callback URLs registered** —
`{origin}/accounts/{google,microsoft}/login/callback/` — for as long as the
rollback path exists. Rolling back to a Django container whose provider apps no
longer accept its callback URL would trade an outage for a worse one. Retire
them with the rest of the Django path in Phase 11.

> **Cutover note (#94, done):** The Next.js callback URLs
> (`/api/auth/callback/google`, `/api/auth/callback/microsoft-entra-id`) can be
> removed from the provider apps; that rollback path is long gone.

---

## Local development

```bash
make pb-css        # compile Tailwind into the binary's embedded stylesheet
make pb-dev        # serve at http://127.0.0.1:8090
make pb-test       # vet + the full Go suite
```

See [`pocketbase/README.md`](pocketbase/README.md) for the full command list. The
Django commands (`make install` / `migrate` / `dev` / `test`, documented in
`DJANGO.md`) still work against the in-tree Django app until Phase 11 removes it.

## Manual Docker run (local testing)

Without `LITESTREAM_BUCKET`, the container runs standalone (no replication):

```bash
mkdir -p pb-data
docker build -t golftrack-pb pocketbase/
docker run --rm -p 8090:8090 \
  -e GOLFTRACK_ALLOW_PASSWORD_LOGIN=true \
  -e ADMIN_EMAILS="you@example.com" \
  -v "$(pwd)/pb-data:/data" \
  golftrack-pb
```

Then create a superuser and an app account as shown in the dev-server section, and sign in at `http://localhost:8090/`. More detail — including the Docker Desktop bind-mount gotcha — is in [`pocketbase/DEPLOYMENT.md`](pocketbase/DEPLOYMENT.md).

> **Maintainer checks (per `CLAUDE.md`):** verify the image builds, the container
> runs as the non-root `app` user (`docker exec golftrack-pb whoami` → `app`), and —
> with `LITESTREAM_*` set — that restore/replicate works end-to-end. These require
> Docker/live S3 access and are run by the maintainer, not in CI.

---

## Backups (Litestream)

The container runs [Litestream](https://litestream.io) as its supervising process, continuously streaming SQLite changes to an S3-compatible bucket. Production currently uses **DigitalOcean Spaces**; any S3-compatible provider works (B2, AWS S3, R2, etc.) by adjusting the endpoint.

### How it runs in the container

`pocketbase/entrypoint.sh` orchestrates startup:

1. If `LITESTREAM_BUCKET` is unset, starts `golftrack-pb serve` directly (no replication — safe for local dev and the dev server).
2. If `LITESTREAM_BUCKET` is set:
   - Restores `/data/data.db` and `/data/auxiliary.db` from the replica if they don't exist.
   - Starts `litestream replicate -exec "./golftrack-pb serve …"`, which supervises the app process and streams WAL changes to the bucket.

PocketBase keeps **two** SQLite databases: `data.db` (the six collections) and `auxiliary.db` (request/cron logs). Both are replicated, under `pocketbase/data` and `pocketbase/auxiliary` — distinct from the Django app's `django` path, so the two migrations' replica generations never collide in the same bucket. Losing `auxiliary.db` between backups costs log history, not application data.

> **Litestream v0.5 gotcha:** `-exec` does *not* run the command through `sh -c`. Pass a single executable, not a shell pipeline. The Django entrypoint had to keep `manage.py migrate` as a separate step ahead of `litestream replicate` for this reason; the PocketBase entrypoint sidesteps it entirely, because the schema sync happens inside the serve process and there is only ever one command to wrap. Preserve that when editing.

### One-time bucket setup

Using DigitalOcean Spaces:

1. Create a private Space (e.g. `golftrack-backup`) in the region of your choice.
2. Create a Spaces access key (**API → Spaces Keys**) with read/write access. Note the **Key**, **Secret**, and **region**.

Other providers work analogously — you just need a bucket, an access key pair, and the S3-compatible endpoint URL.

### Add GitHub Actions secrets

In **Settings → Secrets and variables → Actions**, add:

| Secret | Value | Notes |
|--------|-------|-------|
| `LITESTREAM_ACCESS_KEY_ID` | Spaces access key | |
| `LITESTREAM_SECRET_ACCESS_KEY` | Spaces secret key | |
| `LITESTREAM_BUCKET` | Bucket/Space name, e.g. `golftrack-backup` | Just the name — no URL, no path |
| `LITESTREAM_ENDPOINT` | Region endpoint, e.g. `https://nyc3.digitaloceanspaces.com` | **Region-only.** Do not include the bucket name in the hostname. See gotcha below. |

> **Endpoint format gotcha:** `LITESTREAM_ENDPOINT` must point at the region root, not the full bucket URL. DO Spaces shows your Space URL as `https://<bucket>.<region>.digitaloceanspaces.com` — do **not** paste that whole string. Use `https://<region>.digitaloceanspaces.com` instead. Litestream prepends the bucket itself in virtual-hosted style; if the bucket is already in the endpoint, every `ListObjectsV2` returns `404 NoSuchKey` because the bucket name effectively appears twice in the request URL.

> **DO Spaces compatibility:** `litestream.yml` must **not** set `force-path-style: true`. DO Spaces only supports virtual-hosted-style URLs; path-style requests are 404'd. Other providers (e.g. B2) may need path-style — adjust per-provider.

If `LITESTREAM_BUCKET` is not set, the container falls back to running without replication (safe for local dev / manual docker runs).

### Restore procedure

The entrypoint auto-restores on first boot if the database files do not exist. To recover from data loss, wipe the persistent directory on the VM and restart the container — the schema sync runs after restore, then the app serves against the recovered database:

```bash
ssh <vmhost>
docker stop golftrack-pb && docker rm golftrack-pb
sudo rm -f /data/golftrack-pb/data.db /data/golftrack-pb/data.db-wal /data/golftrack-pb/data.db-shm
sudo rm -f /data/golftrack-pb/auxiliary.db /data/golftrack-pb/auxiliary.db-wal /data/golftrack-pb/auxiliary.db-shm
# Re-run the deploy workflow (Actions → CI / Deploy → Run workflow) or restart the container manually.
```

To restore manually without restarting the app (e.g. to inspect a recovered DB locally):

```bash
docker run --rm \
  -e LITESTREAM_ACCESS_KEY_ID="..." \
  -e LITESTREAM_SECRET_ACCESS_KEY="..." \
  -e LITESTREAM_BUCKET="golftrack-backup" \
  -e LITESTREAM_ENDPOINT="https://nyc3.digitaloceanspaces.com" \
  -v $(pwd):/out \
  --entrypoint litestream \
  ghcr.io/<owner>/golftrack:latest \
  restore -config /app/litestream.yml /out/data.db
```

---

## Upgrading from SQLite to Postgres

PocketBase is built on SQLite and does not support Postgres as a drop-in backend, so
this is no longer a configuration change — it would mean moving off PocketBase. If
traffic ever grows that far, the realistic options are scaling SQLite vertically
(PocketBase's own guidance) or replacing the backend again. The load-test numbers
that informed the decision are in
[`pocketbase/performance_report.md`](pocketbase/performance_report.md).
