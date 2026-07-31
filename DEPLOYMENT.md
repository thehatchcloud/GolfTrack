# Deployment Guide

Pushes to `main` automatically test, build, and deploy the **PocketBase** app to the exe.dev VM via the workflow in `.github/workflows/deploy.yml`.

Non-`main` branches deploy to the **dev server** (`golftrack-dev.exe.xyz`) via `.github/workflows/deploy-dev.yml`. See [Dev server setup](#dev-server-setup-golftrack-devexexyz) below.

> **Stack note:** The deployed artifact is the **PocketBase** app — `pocketbase/Dockerfile` → a single static Go binary (PocketBase as a framework, embedded schema, hooks and frontend) plus Litestream. CI builds that image from the `pocketbase/` context and both servers run it. This is the only app in the repository — see [`POCKETBASE.md`](POCKETBASE.md) for the architecture. PocketBase's own container reference — environment variables and what each one does — is [`pocketbase/DEPLOYMENT.md`](pocketbase/DEPLOYMENT.md).

---

## Dev server setup (`golftrack-dev.exe.xyz`)

The dev server runs the PocketBase app in Docker (`pocketbase/Dockerfile`) — same image as
production, but **without Litestream** (`LITESTREAM_BUCKET` unset) and with OAuth
disabled — sign in with email + password. The image is tagged
`ghcr.io/<owner>/golftrack:pocketbase-dev` and rebuilt on every push.

The container is named `golftrack-pb-dev` and listens on **8090**, published as host
port 8000 so the public URL is unchanged.

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

### How it works

Every push to a non-`main` branch triggers the workflow:

1. Builds the PocketBase Docker image from the `pocketbase/` context (static Go binary, embedded schema/frontend, Litestream)
2. Pushes to `ghcr.io/<owner>/golftrack:pocketbase-dev`
3. SSHs to the dev VM: pulls the image, stops/removes the old container, starts a new one
4. The container entrypoint starts `golftrack-pb serve` on port 8090 (no Litestream, since `LITESTREAM_BUCKET` is unset). There is no separate migrate step — the binary reconciles the database to its embedded `pb_schema.json` during startup
5. The script polls `/api/version` inside the container until it reports this commit's short SHA, then prints `docker ps`

The dev database persists in the `golftrack-pb-dev-data` Docker named volume across deploys — branch switches don't wipe it. For a clean slate: `docker stop golftrack-pb-dev && docker volume rm golftrack-pb-dev-data && docker start golftrack-pb-dev`.

---

## Deployment model (production)

- PocketBase app served by the **`golftrack-pb` binary** inside a Docker container — it is its own HTTP server, and the frontend (templates, Tailwind CSS, vendored JS) is `go:embed`'d into it, so there is no WSGI server and no static-file middleware
- SQLite databases on a persistent directory on the VM (`/data/golftrack-pb/` → `/data` in the container). PocketBase keeps **two**: `data.db` (the six collections) and `auxiliary.db` (request/cron logs)
- **Litestream** runs as the container's supervising process, continuously replicating both databases to an S3-compatible bucket and restoring them on first boot
- Container restarts automatically when the VM reboots (`--restart unless-stopped`)
- There is no migrate step: the binary reconciles the database to its embedded `pb_schema.json` during startup, in the same process Litestream supervises
- The binary listens on port **8090** inside the container; the deploy maps host port **3000 → 8090**. **The host port is not part of the public URL** — exe.dev fronts the VM and serves the app at `https://<vmname>.exe.xyz/` on the default HTTPS port. That distinction matters when registering OAuth redirect URIs; see [Getting `{origin}` right](#getting-origin-right)
- The deploy script health-checks `/api/version` inside the container after `docker run` and **fails the deploy (with `docker logs`) if the app doesn't come up serving this commit's SHA**. `/api/version` rather than `/api/health` because PocketBase's own health endpoint carries no build identity, so it cannot distinguish the new container from a stale one
- The container is named `golftrack-pb`

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
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | Google OAuth client (see [OAuth provider setup](#oauth-provider-setup)) |
| `MICROSOFT_CLIENT_ID` / `MICROSOFT_CLIENT_SECRET` | Microsoft Entra ID app |
| `ADMIN_EMAILS` | Comma-separated emails to grant `ADMIN` role at sign-in |
| `LITESTREAM_ACCESS_KEY_ID` / `LITESTREAM_SECRET_ACCESS_KEY` / `LITESTREAM_BUCKET` / `LITESTREAM_ENDPOINT` | See [Backups (Litestream)](#backups-litestream) |

### 5. Make the GHCR package visible to the VM

After the first successful workflow run, a package named `golftrack` will appear under
your GitHub org/account. The `GHCR_TOKEN` PAT must belong to a user with access to
pull from it. If you prefer, you can set the package visibility to **Public** in
**GitHub → Packages → golftrack → Package settings**, which eliminates the need for
`GHCR_TOKEN` authentication (remove the login line in the workflow script).

---

## What happens on each push to `main`

1. **PocketBase (Go)** — `gofmt`, `go vet`, `go build` and the full Go test suite (schema, access rules, hooks, API parity, frontend). This job **gates the build**
2. **Build** — builds the PocketBase Docker image from the `pocketbase/` context and pushes it to `ghcr.io/<owner>/golftrack:latest` and `:pocketbase-<sha>`
3. **Deploy** — SSHes into the exe.dev VM (`bin/deploy-prod.sh`):
   - pulls the new image
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

PocketBase keeps its encryption key in the data directory rather than an
env-supplied secret, does not validate the request `Host` header, and every
write is a token-authenticated JSON API call rather than a cookie-authenticated
form post, so there is no CSRF token to protect. The data directory is passed
as `--dir /data`, so there is no `DATABASE_URL` either. The full variable
mapping, including `GOLFTRACK_ALLOW_PASSWORD_LOGIN` and
`GOLFTRACK_SCHEMA_SYNC`, is in
[`pocketbase/DEPLOYMENT.md`](pocketbase/DEPLOYMENT.md) § "Environment variables".

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
not matter. It is read **per sign-in** rather than cached at startup — but the
container still only sees the value it was started with, so the recreate step
above is unchanged.

`syncAdminRole` in `pocketbase/internal/hooks/adminrole.go` runs on **every OAuth2
sign-in** and both grants *and* revokes: a user whose provider-verified email is in
the list is promoted to `ADMIN`, and any other user is demoted to `USER`. Two
consequences follow:

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

PocketBase's redirect URI needs to be registered on both OAuth client apps:

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

---

## Local development

```bash
make pb-css        # compile Tailwind into the binary's embedded stylesheet
make pb-dev        # serve at http://127.0.0.1:8090
make pb-test       # vet + the full Go suite
```

See [`pocketbase/README.md`](pocketbase/README.md) for the full command list.

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

PocketBase keeps **two** SQLite databases: `data.db` (the six collections) and `auxiliary.db` (request/cron logs). Both are replicated, under `pocketbase/data` and `pocketbase/auxiliary`. Losing `auxiliary.db` between backups costs log history, not application data.

> **Litestream v0.5 gotcha:** `-exec` does *not* run the command through `sh -c`. Pass a single executable, not a shell pipeline. The schema sync happens inside the serve process, so there is only ever one command to wrap — preserve that when editing `pocketbase/entrypoint.sh`.

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
