# Deployment Guide

Pushes to `main` automatically test, build, and deploy to the exe.dev VM via the workflow in `.github/workflows/deploy.yml`.

Non-`main` branches deploy to the **dev server** (`golftrack-dev.exe.xyz`) via `.github/workflows/deploy-dev.yml`. See [Dev server setup](#dev-server-setup-golftrack-devexexyz) below.

---

## Dev server setup (`golftrack-dev.exe.xyz`)

The dev server runs the Django app directly (no Docker) using gunicorn under systemd.
It uses Python 3.13 until pydantic gains Python 3.14 support.
OAuth is disabled — sign in with email + password.

### 1. Create the dev VM

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

SSH into the dev VM and run:

```bash
ssh ubuntu@golftrack-dev.exe.xyz

# Install uv
curl -LsSf https://astral.sh/uv/install.sh | sh
source $HOME/.local/bin/env

# Create app directory
sudo mkdir -p /srv/golftrack
sudo chown ubuntu:ubuntu /srv/golftrack

# Add systemd service
sudo tee /etc/systemd/system/golftrack-dev.service > /dev/null <<'EOF'
[Unit]
Description=GolfTrack Dev Server
After=network.target

[Service]
User=ubuntu
WorkingDirectory=/srv/golftrack
EnvironmentFile=/home/ubuntu/golftrack-dev.env
ExecStart=/srv/golftrack/.venv/bin/gunicorn config.wsgi:application --bind 0.0.0.0:8000 --workers 2 --timeout 60
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable golftrack-dev

# Allow ubuntu to restart the service without a password
echo "ubuntu ALL=(ALL) NOPASSWD: /bin/systemctl restart golftrack-dev, /bin/systemctl start golftrack-dev, /bin/systemctl stop golftrack-dev" \
  | sudo tee /etc/sudoers.d/golftrack-dev
```

After the first workflow run completes, create a test account:

```bash
cd /srv/golftrack
set -a; source /home/ubuntu/golftrack-dev.env; set +a
.venv/bin/python manage.py shell -c "
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

`ADMIN_EMAILS` is already set from the production setup — reused as-is.

### How it works

Every push to a non-`main` branch (and every PR onto `main`) triggers the workflow:

1. Rsyncs the working tree to `/srv/golftrack/` on the dev VM (excludes `.git`, `.venv`, databases, built assets)
2. Installs/updates Python deps with `uv pip install`
3. Runs `manage.py migrate`
4. Runs `manage.py collectstatic`
5. Restarts the gunicorn service

The dev database (`/srv/golftrack/dev.db`) persists across deploys — branch switches don't wipe it. If you need a clean slate: `sudo systemctl stop golftrack-dev && rm /srv/golftrack/dev.db && sudo systemctl start golftrack-dev`.

---

## Deployment model

- Next.js standalone output running in a Docker container
- SQLite database on a persistent directory on the VM (`/data/golftrack/`)
- Container restarts automatically when the VM reboots (`--restart unless-stopped`)
- Migrations run automatically inside the container at startup before the server process starts
- exe.dev transparently proxies port 3000 at `https://<vmname>.exe.xyz:3000/`

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

### 5. Make the GHCR package visible to the VM

After the first successful workflow run, a package named `golftrack` will appear under
your GitHub org/account. The `GHCR_TOKEN` PAT must belong to a user with access to
pull from it. If you prefer, you can set the package visibility to **Public** in
**GitHub → Packages → golftrack → Package settings**, which eliminates the need for
`GHCR_TOKEN` authentication (remove the login line in the workflow script).

---

## What happens on each push to `main`

1. **Test** — runs `npm test` against a fresh SQLite test database
2. **Build** — builds the Docker image and pushes to `ghcr.io/<owner>/golftrack:latest`
3. **Deploy** — SSHes into the exe.dev VM:
   - pulls the new image
   - stops and removes the old container
   - starts the new container (migrations run automatically before the server starts)
   - prunes old images

The workflow also supports manual triggering: **Actions → CI / Deploy → Run workflow**. Use this when you've rotated a secret (e.g. `LITESTREAM_*`) and need to restart the container to pick up the new value without pushing a code change.

---

## Required environment variables

| Variable | Value |
|----------|-------|
| `NODE_ENV` | `production` |
| `PORT` | `3000` |
| `DATABASE_URL` | `file:/data/prod.db` |
| `AUTH_SECRET` | 32+ random bytes (generate with `openssl rand -base64 32`) |
| `AUTH_URL` | Public origin, e.g. `https://golftrack.exe.xyz:3000` |
| `AUTH_TRUST_HOST` | `true` (exe.dev terminates TLS upstream) |
| `AUTH_GOOGLE_ID` / `AUTH_GOOGLE_SECRET` | Google OAuth client credentials |
| `AUTH_MICROSOFT_ENTRA_ID_ID` / `AUTH_MICROSOFT_ENTRA_ID_SECRET` | Microsoft Entra ID app credentials |
| `ADMIN_EMAILS` | Comma-separated emails to grant `ADMIN` role at sign-in |

These are set directly in the `docker run` command in the deploy workflow. No `.env` file is needed on the VM.

### OAuth provider setup

Both providers must have the production callback URL registered:

- **Google** — [GolfTrack OAuth client](https://console.cloud.google.com/apis/credentials?project=prime-agency-199418)
  Callback URL: `{AUTH_URL}/api/auth/callback/google`
- **Microsoft Entra ID** — [GolfTrack app registration](https://entra.microsoft.com/#view/Microsoft_AAD_RegisteredApps/ApplicationMenuBlade/~/Overview/quickStartType~/null/sourceType/Microsoft_AAD_IAM/appId/d75517a5-078b-45b3-87e5-fecdac856e2a/objectId/ca0c6eee-61a4-43cb-949c-bd46b7be3a1f/isMSAApp~/false/defaultBlade/Overview/appSignInAudience/AzureADandPersonalMicrosoftAccount/servicePrincipalCreated~/true)
  Callback URL: `{AUTH_URL}/api/auth/callback/microsoft-entra-id`
  Audience: "Accounts in any organizational directory and personal Microsoft accounts" (required for the default `common` issuer to accept both work and personal accounts)

For local development, also register `http://localhost:3000/api/auth/callback/{google,microsoft-entra-id}` on the same OAuth clients (or use separate dev-only credentials).

---

## Local development

```bash
npm install
npm run db:migrate
npm run dev
```

## Local production build

```bash
npm run build
npm start
```

## Manual Docker run (local testing)

```bash
docker build -t golf-track .
docker run --rm -p 3000:3000 \
  -e DATABASE_URL="file:/data/prod.db" \
  -v $(pwd)/data:/data \
  golf-track
```

## Prisma commands

```bash
npm run db:migrate   # create and apply migration (dev)
npm run db:deploy    # apply existing migrations (production)
npm run db:generate  # regenerate client after schema changes
npm run db:seed      # seed with sample data
```

---

## Backups (Litestream)

The container runs [Litestream](https://litestream.io) alongside the app, continuously streaming SQLite changes to an S3-compatible bucket. Production currently uses **DigitalOcean Spaces**; any S3-compatible provider works (B2, AWS S3, R2, etc.) by adjusting the endpoint.

### How it runs in the container

`entrypoint.sh` orchestrates startup:

1. If `LITESTREAM_BUCKET` is unset, runs `prisma migrate deploy` and starts `node server.js` directly (no replication — safe for local dev).
2. If `LITESTREAM_BUCKET` is set:
   - Restores `/data/prod.db` from the replica if it doesn't exist.
   - Runs `prisma migrate deploy`.
   - Starts `litestream replicate -exec "node server.js"`, which supervises the Node process and streams WAL changes to the bucket.

> **Litestream v0.5 gotcha:** `-exec` does *not* run the command through `sh -c`. Pass a single executable, not a shell pipeline. Migrations must run as a separate step before `litestream replicate`, not chained inside `-exec` with `&&`.

### One-time bucket setup

Using DigitalOcean Spaces:

1. Create a private Space (e.g. `golftrack-backup`) in the region of your choice.
2. Create an Spaces access key (**API → Spaces Keys**) with read/write access. Note the **Key**, **Secret**, and **region**.

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

If traffic grows, swap `DATABASE_URL` for a Postgres connection string and update
`prisma/schema.prisma` to use `provider = "postgresql"`. The rest of the app is
unchanged.
