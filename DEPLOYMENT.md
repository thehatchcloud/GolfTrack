# Deployment Guide

Pushes to `main` automatically test, build, and deploy to the exe.dev VM via the workflow in `.github/workflows/deploy.yml`.

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

Make it publicly accessible:

```bash
ssh exe.dev share set-public golftrack
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

---

## Required environment variables

| Variable | Value |
|----------|-------|
| `NODE_ENV` | `production` |
| `PORT` | `3000` |
| `DATABASE_URL` | `file:/data/prod.db` |

These are set directly in the `docker run` command in the deploy workflow. No `.env` file is needed on the VM.

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

The container runs [Litestream](https://litestream.io) alongside the app. Litestream continuously streams SQLite WAL changes to an S3-compatible bucket, giving near-zero RPO without scheduled jobs.

### One-time bucket setup (Backblaze B2 recommended)

1. Create a B2 account and a private bucket (e.g. `golftrack-backups`).
2. Create an application key scoped to that bucket with **Read and Write** permissions.
3. Note the **Key ID**, **Application Key**, and **Endpoint** (e.g. `https://s3.us-west-004.backblazeb2.com`).

### Add GitHub Actions secrets

In **Settings → Secrets and variables → Actions**, add:

| Secret | Value |
|--------|-------|
| `LITESTREAM_ACCESS_KEY_ID` | B2 Key ID |
| `LITESTREAM_SECRET_ACCESS_KEY` | B2 Application Key |
| `LITESTREAM_BUCKET` | Bucket name, e.g. `golftrack-backups` |
| `LITESTREAM_ENDPOINT` | B2 S3 endpoint, e.g. `https://s3.us-west-004.backblazeb2.com` |

On the next deploy, the container will start replicating automatically.

If `LITESTREAM_BUCKET` is not set, the container falls back to running without replication (safe for local dev / manual docker runs).

### Restore procedure

To recover from data loss:

```bash
# On a fresh VM (or after wiping /data/golftrack), run the container once with:
docker run --rm \
  -e LITESTREAM_ACCESS_KEY_ID="..." \
  -e LITESTREAM_SECRET_ACCESS_KEY="..." \
  -e LITESTREAM_BUCKET="golftrack-backups" \
  -e LITESTREAM_ENDPOINT="https://s3.us-west-004.backblazeb2.com" \
  -v /data/golftrack:/data \
  --entrypoint litestream \
  ghcr.io/<owner>/golftrack:latest \
  restore -config /app/litestream.yml /data/prod.db
```

Then start the container normally — migrations will run and the app will pick up from the restored database.

Alternatively, the entrypoint script auto-restores on first boot if the database file does not exist:

```bash
# Just start the container normally on a VM with no existing /data/golftrack/prod.db
docker run -d --name golftrack --restart unless-stopped \
  -p 3000:3000 \
  -e DATABASE_URL="file:/data/prod.db" \
  -e NODE_ENV=production \
  -e LITESTREAM_ACCESS_KEY_ID="..." \
  -e LITESTREAM_SECRET_ACCESS_KEY="..." \
  -e LITESTREAM_BUCKET="golftrack-backups" \
  -e LITESTREAM_ENDPOINT="https://s3.us-west-004.backblazeb2.com" \
  -v /data/golftrack:/data \
  ghcr.io/<owner>/golftrack:latest
```

The entrypoint will detect the missing database and restore from the latest replica before starting the app.

---

## Upgrading from SQLite to Postgres

If traffic grows, swap `DATABASE_URL` for a Postgres connection string and update
`prisma/schema.prisma` to use `provider = "postgresql"`. The rest of the app is
unchanged.
