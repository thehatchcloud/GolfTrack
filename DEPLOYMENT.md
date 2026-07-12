# Deployment Guide

Pushes to `main` automatically test, build, and deploy the **Django** app to the exe.dev VM via the workflow in `.github/workflows/deploy.yml`.

Non-`main` branches deploy to the **dev server** (`golftrack-dev.exe.xyz`) via `.github/workflows/deploy-dev.yml`. See [Dev server setup](#dev-server-setup-golftrack-devexexyz) below.

> **Stack note:** As of the Phase 7 rewrite (#93), the deployed artifact is the Django app (`Dockerfile` → Python 3.13, gunicorn, WhiteNoise, Litestream). The legacy Next.js app remains in-tree until the Phase 8 cutover (#94) but is no longer built or deployed. The previous Next.js image stays in GHCR for rollback.

---

## Dev server setup (`golftrack-dev.exe.xyz`)

The dev server runs the Django app in Docker (the repo's `Dockerfile`) — same image as
production, but **without Litestream** (`LITESTREAM_BUCKET` unset) and with OAuth
disabled — sign in with email + password. The image is tagged
`ghcr.io/<owner>/golftrack:django-dev` and rebuilt on every push.

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

None — Docker is already installed on `exeuntu` and the workflow uses a named Docker volume (`golftrack-dev-data`) that Docker creates automatically on first run.

After the first workflow run completes, create a test account:

```bash
docker exec -it golftrack-dev python manage.py shell -c "
from accounts.models import User
u = User(username='admin', email='your@email.com', role='ADMIN', is_staff=True, is_superuser=True)
u.set_password('choose-a-password')
u.save()
print('Done')
"
```

### 4. GitHub Actions secrets for dev

**Settings → Secrets and variables → Actions → New repository secret**

| Secret | Value |
|--------|-------|
| `DEV_DEPLOY_HOST` | `golftrack-dev.exe.xyz` |
| `DEV_DEPLOY_SSH_KEY` | Contents of `~/.ssh/golftrack_dev_deploy` (private key) |
| `DEV_DJANGO_SECRET_KEY` | Generate with `python3 -c "import secrets,base64;print(base64.urlsafe_b64encode(secrets.token_bytes(50)).decode())"` |

`ADMIN_EMAILS` and `GHCR_TOKEN` are already set from the production setup — reused as-is.

### How it works

Every push to a non-`main` branch triggers the workflow:

1. Builds the Django Docker image (Python 3.13, gunicorn, collectstatic baked in)
2. Pushes to `ghcr.io/<owner>/golftrack:django-dev`
3. SSHs to the dev VM: pulls the image, stops/removes the old container, starts a new one
4. The container entrypoint runs `manage.py migrate` then starts gunicorn on port 8000 (no Litestream, since `LITESTREAM_BUCKET` is unset)

The dev database persists in the `golftrack-dev-data` Docker named volume across deploys — branch switches don't wipe it. For a clean slate: `docker stop golftrack-dev && docker volume rm golftrack-dev-data && docker start golftrack-dev`.

---

## Deployment model (production)

- Django app served by **gunicorn** (WSGI) inside a Docker container, static files served by **WhiteNoise**
- SQLite database on a persistent directory on the VM (`/data/golftrack/` → `/data` in the container)
- **Litestream** runs as the container's supervising process, continuously replicating SQLite to an S3-compatible bucket and restoring it on first boot
- Container restarts automatically when the VM reboots (`--restart unless-stopped`)
- Migrations (`manage.py migrate`) run automatically inside the container at startup, before gunicorn starts
- gunicorn listens on port **8000** inside the container; the deploy maps host port **3000 → 8000**, so exe.dev proxies the app at `https://<vmname>.exe.xyz:3000/` (the public URL is unchanged from the Next.js era)
- The deploy script health-checks `/api/health` inside the container after `docker run` and **fails the deploy (with `docker logs`) if the app doesn't come up** — a crash-looping container no longer reports a green deploy

---

## Cutover from the Next.js app (one-time)

Per the rewrite decision (#85: *start fresh, no data migration; Litestream history from the old app is abandoned*), the Django app must come up on a **clean database built from migrations**, not the old Next.js/Prisma SQLite file left on the VM.

Two things keep them separate:

- **Fresh replica path.** `litestream.yml` replicates to the `django` path in the bucket, not the Next.js app's `prod` path. The old `prod` objects are orphaned and can be deleted from the bucket whenever convenient.
- **Wiping the stale local DB.** The persistent volume (`/data/golftrack/`) still holds the old Prisma `prod.db`. Remove it once so the container's entrypoint restores nothing (the `django` replica is empty on first boot) and `migrate` builds the schema from scratch:

```bash
ssh <prod-vmhost>
docker stop golftrack 2>/dev/null || true
docker rm   golftrack 2>/dev/null || true
sudo rm -f /data/golftrack/prod.db /data/golftrack/prod.db-wal /data/golftrack/prod.db-shm
```

Then redeploy (**Actions → CI / Deploy → Run workflow**). The container boots against an empty `django` replica → fresh `/data/prod.db` → `migrate` → gunicorn under Litestream. Because the previous image stays in GHCR, rollback is re-deploying the prior tag; the abandoned `prod` replica is still there if the old app ever needs it.

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

`DJANGO_ALLOWED_HOSTS` and `DJANGO_CSRF_TRUSTED_ORIGINS` are not secrets — the
workflow derives them from `DEPLOY_HOST` (`<host>` and `https://<host>:3000`).

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

1. **Test** — `make install` then ruff lint, Tailwind + `collectstatic` build, and the pytest suite (`make test`) against a fresh SQLite test database
2. **Build** — builds the Django Docker image and pushes to `ghcr.io/<owner>/golftrack:latest`
3. **Deploy** — SSHes into the exe.dev VM:
   - pulls the new image
   - stops and removes the old container
   - starts the new container (Litestream restores the DB if missing, migrations run, then gunicorn starts)
   - prunes old images

The workflow also supports manual triggering: **Actions → CI / Deploy → Run workflow**. Use this when you've rotated a secret (e.g. `LITESTREAM_*`) and need to restart the container to pick up the new value without pushing a code change.

---

## Required environment variables

Set directly in the `docker run` command in the deploy workflow — no `.env` file is needed on the VM.

| Variable | Value |
|----------|-------|
| `DATABASE_URL` | `file:/data/prod.db` (the `file:` form is accepted for parity with the old deploy) |
| `DJANGO_SECRET_KEY` | 50+ random bytes (see secret-generation command above) |
| `DJANGO_DEBUG` | `false` |
| `DJANGO_ALLOWED_HOSTS` | VM hostname, e.g. `golftrack.exe.xyz` |
| `DJANGO_CSRF_TRUSTED_ORIGINS` | Public origin incl. port, e.g. `https://golftrack.exe.xyz:3000` |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | Google OAuth client credentials |
| `MICROSOFT_CLIENT_ID` / `MICROSOFT_CLIENT_SECRET` | Microsoft Entra ID app credentials |
| `ADMIN_EMAILS` | Comma-separated emails to grant `ADMIN` role at sign-in |
| `LITESTREAM_*` | Replication credentials/endpoint — see [Backups](#backups-litestream) |

Django trusts `X-Forwarded-Proto` from exe.dev's TLS-terminating proxy
(`SECURE_PROXY_SSL_HEADER` in `config/settings.py`), so cookies are marked secure
even though gunicorn speaks plain HTTP inside the container.

### OAuth provider setup

Django uses **its own OAuth client apps** (via django-allauth), separate from the
Next.js `AUTH_*` apps. Providers validate redirect URIs per client, so Django's
`/accounts/.../login/callback/` URIs must be registered on the Django apps:

- **Google** — [console.cloud.google.com/apis/credentials](https://console.cloud.google.com/apis/credentials)
  Callback URL: `https://golftrack.exe.xyz:3000/accounts/google/login/callback/`
- **Microsoft Entra ID** — [entra.microsoft.com → App registrations](https://entra.microsoft.com)
  Callback URL: `https://golftrack.exe.xyz:3000/accounts/microsoft/login/callback/`
  Audience: "Accounts in any organizational directory and personal Microsoft accounts" (required for the default `common` tenant to accept both work and personal accounts)

For local development, also register `http://localhost:8000/accounts/{google,microsoft}/login/callback/` on the same clients (or use separate dev-only credentials).

> **Cutover note (#94):** The old Next.js callback URLs
> (`/api/auth/callback/google`, `/api/auth/callback/microsoft-entra-id`) can be
> removed from the provider apps once the Django app is live and verified.

---

## Local development

```bash
make install       # create .venv (Python 3.14) and install deps
make migrate       # apply migrations (creates db.sqlite3)
make dev           # runserver at http://localhost:8000
```

See `DJANGO.md` for the full command list.

## Manual Docker run (local testing)

Without `LITESTREAM_BUCKET`, the container runs standalone (migrate → gunicorn, no replication):

```bash
docker build -t golftrack .
docker run --rm -p 8000:8000 \
  -e DATABASE_URL="file:/data/prod.db" \
  -e DJANGO_SECRET_KEY="local-dev-secret" \
  -e DJANGO_DEBUG=false \
  -e DJANGO_ALLOWED_HOSTS="localhost,127.0.0.1" \
  -e DJANGO_CSRF_TRUSTED_ORIGINS="http://localhost:8000" \
  -e DJANGO_ALLOW_PASSWORD_LOGIN=true \
  -v "$(pwd)/data:/data" \
  golftrack
```

Then create an account with `docker exec ... manage.py shell` as shown in the dev-server section, and sign in at `http://localhost:8000/`.

> **Maintainer checks (per `CLAUDE.md`):** verify the image builds, the container
> runs as the non-root `app` user (`docker exec golftrack whoami` → `app`), and —
> with `LITESTREAM_*` set — that restore/replicate works end-to-end. These require
> Docker/live S3 access and are run by the maintainer, not in CI.

---

## Backups (Litestream)

The container runs [Litestream](https://litestream.io) as its supervising process, continuously streaming SQLite changes to an S3-compatible bucket. Production currently uses **DigitalOcean Spaces**; any S3-compatible provider works (B2, AWS S3, R2, etc.) by adjusting the endpoint.

### How it runs in the container

`entrypoint.sh` orchestrates startup:

1. If `LITESTREAM_BUCKET` is unset, runs `manage.py migrate` and starts gunicorn directly (no replication — safe for local dev and the dev server).
2. If `LITESTREAM_BUCKET` is set:
   - Restores `/data/prod.db` from the replica if it doesn't exist.
   - Runs `manage.py migrate`.
   - Starts `litestream replicate -exec "gunicorn …"`, which supervises the gunicorn process and streams WAL changes to the bucket.

> **Litestream v0.5 gotcha:** `-exec` does *not* run the command through `sh -c`. Pass a single executable (gunicorn with its flags), not a shell pipeline. Migrations must run as a separate step before `litestream replicate`, not chained inside `-exec` with `&&`. `entrypoint.sh` keeps this structure — preserve it when editing.

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

The entrypoint auto-restores on first boot if `/data/prod.db` does not exist. To recover from data loss, wipe the persistent volume on the VM and restart the container — migrations run after restore, then the app starts against the recovered database:

```bash
ssh <vmhost>
docker stop golftrack && docker rm golftrack
sudo rm /data/golftrack/prod.db   # or move it aside for inspection
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
  restore -config /app/litestream.yml /out/prod.db
```

---

## Upgrading from SQLite to Postgres

If traffic grows, swap the `default` database in `config/settings.py` for a Postgres
backend (`django.db.backends.postgresql`) and point `DATABASE_URL` at the Postgres
connection string. Litestream is SQLite-specific, so drop it from the entrypoint and
use Postgres-native backups instead. The rest of the app is unchanged.
