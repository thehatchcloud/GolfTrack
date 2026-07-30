# Deployment Guide

Pushes to `main` automatically test, build, and deploy the **PocketBase** app to the exe.dev VM via the workflow in `.github/workflows/deploy.yml`.

Non-`main` branches deploy to the **dev server** (`golftrack-dev.exe.xyz`) via `.github/workflows/deploy-dev.yml`. See [Dev server setup](#dev-server-setup-golftrack-devexexyz) below.

> **Stack note:** The deployed artifact is the **PocketBase** app — `pocketbase/Dockerfile` → a single static Go binary (PocketBase as a framework, embedded schema, hooks and frontend) plus Litestream. CI builds that image from the `pocketbase/` context and both servers run it. This is the only app in the repository — see [`POCKETBASE.md`](POCKETBASE.md) for the architecture and the retired Django/Next.js history.
>
> The Django app PocketBase replaced was removed from this repository in Phase 11 (#132), once production had proven stable on PocketBase. Its last image stays in GHCR as `ghcr.io/<owner>/golftrack:django-latest` and the last commit that had it in-tree is tagged `django-final`, for manual disaster recovery — see [Rollback to Django](#rollback-to-django). PocketBase's own container reference — environment variables and what each removed Django variable used to map to (or didn't) — is [`pocketbase/DEPLOYMENT.md`](pocketbase/DEPLOYMENT.md).

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
# 1. A superuser for the Admin UI (https://golftrack-dev.exe.xyz:8000/_/)
docker exec golftrack-pb-dev ./golftrack-pb superuser upsert your@email.com 'choose-a-password' --dir /data

# 2. In the Admin UI, add a record to the `users` collection with your email,
#    a password, and role = ADMIN. Then sign in at /accounts/login/.
```

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
- The binary listens on port **8090** inside the container; the deploy maps host port **3000 → 8090**, so exe.dev proxies the app at `https://<vmname>.exe.xyz:3000/` (the public URL has been stable across every stack this app has run on)
- The deploy script health-checks `/api/version` inside the container after `docker run` and **fails the deploy (with `docker logs`) if the app doesn't come up serving this commit's SHA**. `/api/version` rather than `/api/health` because PocketBase's own health endpoint carries no build identity, so it cannot distinguish the new container from a stale one
- The container is named `golftrack-pb`

---

## Cutover history (historical record)

GolfTrack has changed backend stacks twice: Next.js → Django (#94), then Django →
PocketBase (#131). Both cutovers were container swaps on the exe.dev VMs, done
without downtime or data migration, using the same pattern this deploy still
uses: a distinct container name, a distinct `/data` subdirectory, and a distinct
Litestream replica path per stack, so the outgoing stack's state was never
touched by the incoming one. The runbooks for those one-time migrations are not
reproduced here — see git history around #94 and #131 (`d7e9038`, `1df688d`) if
you need the exact steps that were run. What survives from them:

- The `nextjs-final` and `django-final` git tags mark the last commit of each
  retired stack (see [`POCKETBASE.md`](POCKETBASE.md) § "Historical stacks").
- `ghcr.io/<owner>/golftrack:django-latest` is the last Django image, kept in
  GHCR for the rollback below. The equivalent Next.js image was not preserved —
  that rollback path was retired when Django's cutover completed (#94).
- The Django SQLite database and its Litestream replica (`django` bucket path)
  were left untouched by the PocketBase cutover, under `/data/golftrack` on the
  VM — distinct from PocketBase's own `/data/golftrack-pb` and `pocketbase/*`
  replica paths.

## Rollback to Django

There is no longer an in-repo script for this — `bin/rollback-prod.sh` was
removed in Phase 11 (#132) once production had run stably on PocketBase long
enough that the automated rollback path was no longer worth maintaining
against a deleted Django tree. Recovery is still possible by hand, since the
Django database and its Litestream replica were never touched by the
PocketBase cutover:

```bash
ssh <prod-vmhost>
echo "$GHCR_TOKEN" | docker login ghcr.io -u thehatchcloud --password-stdin
docker pull ghcr.io/thehatchcloud/golftrack:django-latest

docker stop golftrack-pb && docker rm golftrack-pb   # /data/golftrack-pb is left in place

sudo chown -R 1001:1001 /data/golftrack
docker run -d --name golftrack --restart unless-stopped \
  -p 3000:8000 \
  -e DATABASE_URL="file:/data/prod.db" \
  -e DJANGO_SECRET_KEY="…" -e DJANGO_DEBUG=false \
  -e DJANGO_ALLOWED_HOSTS="golftrack.exe.xyz" \
  -e DJANGO_CSRF_TRUSTED_ORIGINS="https://golftrack.exe.xyz:3000" \
  -e LITESTREAM_ACCESS_KEY_ID="…" -e LITESTREAM_SECRET_ACCESS_KEY="…" \
  -e LITESTREAM_BUCKET="…" -e LITESTREAM_ENDPOINT="…" \
  -e GOOGLE_CLIENT_ID="…" -e GOOGLE_CLIENT_SECRET="…" \
  -e MICROSOFT_CLIENT_ID="…" -e MICROSOFT_CLIENT_SECRET="…" \
  -e ADMIN_EMAILS="…" \
  -v /data/golftrack:/data \
  ghcr.io/thehatchcloud/golftrack:django-latest
```

The secret values are the same ones `deploy.yml` used before Phase 11 removed
Django from the workflow's `env:` block; read them out of the Actions secret
store (or your password manager). This is what `bin/rollback-prod.sh` did
before it was retired — see its last version at the `django-final` tag if you
want the full health-check/verification logic rather than the bare `docker
run` above.

Two things this deliberately does not do:

- **It leaves `/data/golftrack-pb` in place**, so whatever the PocketBase app recorded while it was live is still there to inspect (and still in the bucket under `pocketbase/*`).
- **It does not roll back the repository.** `main` still builds the PocketBase image, so the next push re-deploys it. Revert to the `django-final` tag, or disable the deploy workflow, if the rollback is meant to hold — and note that reverting the repo to `django-final` would also need the CI/CD workflow files restored from that tag, since the current ones no longer build a Django image at all.

> This is now a rare, deliberately manual recovery path — not something exercised in CI or automated in the repo.

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

`DJANGO_SECRET_KEY`, `DJANGO_ALLOWED_HOSTS`, and `DJANGO_CSRF_TRUSTED_ORIGINS`
are no longer read by anything in this repository. If they are still present
in the Actions secret store from before Phase 11 (#132), they are dead weight
kept only for the manual rollback in [Rollback to Django](#rollback-to-django)
and may be deleted once that rollback path is no longer needed.

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

PocketBase has no equivalent for four variables the Django container needed —
`DJANGO_SECRET_KEY` (PocketBase keeps its encryption key in the data directory),
`DJANGO_ALLOWED_HOSTS` (it does not validate the `Host` header),
`DJANGO_CSRF_TRUSTED_ORIGINS` (writes are token-authenticated JSON calls, not
cookie-authenticated form posts, so there is no CSRF token to protect) and
`DATABASE_URL` (the data directory passed as `--dir /data` plays that role). The
full mapping, including `GOLFTRACK_ALLOW_PASSWORD_LOGIN` and
`GOLFTRACK_SCHEMA_SYNC`, is in
[`pocketbase/DEPLOYMENT.md`](pocketbase/DEPLOYMENT.md) § "Environment variables".

None of the four are passed by `deploy.yml` any more. If they are still sitting
in the Actions secret store, they are only there for the manual [Rollback to
Django](#rollback-to-django).

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

PocketBase's redirect URI needs to be registered on both OAuth client apps:

- **Google** — [console.cloud.google.com/apis/credentials](https://console.cloud.google.com/apis/credentials)
  Redirect URI: `https://golftrack.exe.xyz:3000/api/oauth2-redirect`
- **Microsoft Entra ID** — [entra.microsoft.com → App registrations](https://entra.microsoft.com)
  Redirect URI: `https://golftrack.exe.xyz:3000/api/oauth2-redirect`
  Audience: "Accounts in any organizational directory and personal Microsoft accounts" (required for the default `common` tenant to accept both work and personal accounts)

`/api/oauth2-redirect` is PocketBase's own endpoint — nothing in `pocketbase/`
implements it. Details and the local-development URI
(`http://127.0.0.1:8090/api/oauth2-redirect`) are in
[`pocketbase/AUTH.md`](pocketbase/AUTH.md).

The Django callback URLs (`https://golftrack.exe.xyz:3000/accounts/{google,microsoft}/login/callback/`)
are still registered on the same client apps, kept alongside PocketBase's for
as long as the [manual Django rollback](#rollback-to-django) is meant to keep
working. The Next.js-era callback URLs from before that were removed when the
Django cutover completed (#94) and its own rollback path was retired.

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

PocketBase keeps **two** SQLite databases: `data.db` (the six collections) and `auxiliary.db` (request/cron logs). Both are replicated, under `pocketbase/data` and `pocketbase/auxiliary` — distinct from the retired Django app's `django` bucket path (still there, for the manual rollback), so the generations never collide. Losing `auxiliary.db` between backups costs log history, not application data.

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
