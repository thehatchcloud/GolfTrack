# PocketBase deployment

> **This is the deployed app.** CI builds the container documented here from
> the `pocketbase/` context and both the dev server and the exe.dev production
> VM run it. The pipeline and the cutover runbook live in the repository
> root's [`DEPLOYMENT.md`](../DEPLOYMENT.md); this file stays the reference for
> the container itself — what it needs, how it starts, and how its backups
> work.

## What the container adds

- `Dockerfile` — multi-stage: `go build` in a `golang:1.25-bookworm` builder,
  then a `debian:bookworm-slim` runtime image with the static binary,
  Litestream, and a non-root `app` user (uid/gid 1001).
- `litestream.yml` — replicates both of PocketBase's SQLite databases
  (`data.db`, `auxiliary.db`) to an S3-compatible bucket.
- `entrypoint.sh` — restores from the replica on first boot, then starts
  `golftrack-pb serve` directly or under `litestream replicate -exec`.
- `.env.example` — the container's environment variables.
- `/api/health` — **already implemented**, by the PocketBase framework
  itself. There is no route to add; see "Health check" below.

## Why the container is lean

- **Schema sync happens in-process**, at every startup, before it starts
  serving (`syncSchemaOnServe` in `main.go`/`schema.go`, reconciling the
  database to the embedded `pb_schema.json`). There is no separate migration
  command to run first, so `entrypoint.sh` never chains a shell pipeline
  inside `litestream replicate -exec` (Litestream v0.5's `-exec` does not
  invoke `sh -c`) — there's only ever one command to run.
- **The frontend is `go:embed`'d.** The Tailwind build
  (`internal/web/static/css/app.css`), the vendored Alpine/PocketBase SDK, and
  `pb_schema.json` are all committed source, so `go build` alone produces
  something that serves every page — no separate static-asset build step at
  image build time. Re-run `make pb-css` before rebuilding the image if a
  template's classes changed; the compiled CSS is committed precisely so a
  checkout (and this Dockerfile) can build with no Tailwind toolchain
  installed.

## Manual Docker run (local testing)

Build from the `pocketbase/` directory (the Go module root):

```bash
docker build -t golftrack-pb pocketbase/
```

Without `LITESTREAM_BUCKET`, the container runs standalone — no replication.
Create the bind-mount directory first: Docker Desktop (macOS/Windows) does not
auto-create a missing host path for `-v`, unlike the Linux engine, and fails
with `statfs ...: no such file or directory` if it's missing.

```bash
mkdir -p pb-data
docker run --rm -p 8090:8090 \
  -e GOLFTRACK_ALLOW_PASSWORD_LOGIN=true \
  -e ADMIN_EMAILS="you@example.com" \
  -v "$(pwd)/pb-data:/data" \
  golftrack-pb
```

Then create a superuser to reach the Admin UI (`docker exec` into the
container isn't set up with the `superuser` command pre-baked as a step, but
the binary supports it the same as `dev.sh` does locally):

```bash
docker exec golftrack-pb ./golftrack-pb superuser upsert admin@example.com change-me --dir /data
```

With `GOLFTRACK_ALLOW_PASSWORD_LOGIN=true` and no OAuth app configured, sign
in at `http://localhost:8090/accounts/login/` with an account created in the
Admin UI (`http://localhost:8090/_/`), same as local dev — see
[`README.md`](README.md) § "Quick start".

> **Maintainer checks (per `../CLAUDE.md`):** verify the image builds, the
> container runs as the non-root `app` user (`docker exec golftrack-pb
> whoami` → `app`), and — with `LITESTREAM_*` set — that restore/replicate
> works end-to-end. These need Docker and live S3 access this environment
> doesn't have, so they're run by the maintainer, not in CI.

## Environment variables

| Variable | Effect |
|---|---|
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | registers the Google OAuth2 provider |
| `MICROSOFT_CLIENT_ID` / `MICROSOFT_CLIENT_SECRET` | registers the Microsoft Entra ID provider |
| `ADMIN_EMAILS` | comma-separated addresses granted `role = ADMIN` on sign-in; empty is a no-op, not a mass demotion |
| `GOLFTRACK_ALLOW_PASSWORD_LOGIN` | email+password login for accounts that already exist; self-service signup stays OAuth2-only regardless |
| `GOLFTRACK_SCHEMA_SYNC` | set to `0` to skip the embedded-schema sync at startup (dev/debugging only — see `README.md` § "Schema changes") |
| `LITESTREAM_BUCKET` / `LITESTREAM_ENDPOINT` / `LITESTREAM_ACCESS_KEY_ID` / `LITESTREAM_SECRET_ACCESS_KEY` | S3-compatible replication; unset `LITESTREAM_BUCKET` disables it entirely |

PocketBase needs no secret-key or allowed-hosts style variable: it keeps its
own encryption key in the data directory rather than an env-supplied secret,
and it does not validate the request `Host` header. It also needs no
CSRF-trusted-origins variable, since every write is a JSON API call carrying
an `Authorization` header rather than a cookie-authenticated form post
(`README.md` § "Frontend"). There is no `DATABASE_URL` either — the data
directory (`--dir`, hardcoded to `/data` in `entrypoint.sh`) plays that role.

Redirect URI to register with both OAuth providers: `{origin}/api/oauth2-redirect`
(see `AUTH.md`).

## Health check

`GET /api/health` needs no route added — it's PocketBase's own endpoint,
answering `{"code":200,"message":"API is healthy.",...}`
(`TestHealthEndpointShape` in `parity_test.go` pins the shape). Any consumer
of this endpoint — the `Dockerfile`'s own `HEALTHCHECK`, and the deploy
scripts — only needs the `200`, not the body shape.

## Backups (Litestream)

Litestream runs as the container's supervising process, continuously
streaming SQLite changes to an S3-compatible bucket, and `entrypoint.sh`
restores from the replica on first boot if the local database files are
missing.

PocketBase keeps **two** SQLite databases in its data directory — `data.db`
(the six GolfTrack collections) and `auxiliary.db` (request/cron logs).
`litestream.yml` replicates both, each under its own bucket path
(`pocketbase/data`, `pocketbase/auxiliary`). Losing `auxiliary.db` between
backups costs log history, not application data — `data.db` is the one that
matters for recovery.

### Endpoint format gotcha

`LITESTREAM_ENDPOINT` must be the **region root**
(`https://nyc3.digitaloceanspaces.com`), not the full bucket URL DO Spaces
shows in its dashboard (`https://<bucket>.<region>.digitaloceanspaces.com`).
Litestream prepends the bucket itself in virtual-hosted style; pasting the
bucket-qualified URL makes every `ListObjectsV2` 404.

### DO Spaces compatibility

`litestream.yml` must **not** set `force-path-style: true` — DO Spaces only
supports virtual-hosted-style URLs. Other providers (e.g. B2) may need it set
instead.

### Restore procedure

Wipe the persistent volume and restart the container; `entrypoint.sh`
restores both databases from the replica before `golftrack-pb serve` starts.

```bash
docker stop golftrack-pb && docker rm golftrack-pb
sudo rm -f /data/golftrack-pb/data.db /data/golftrack-pb/data.db-wal /data/golftrack-pb/data.db-shm
sudo rm -f /data/golftrack-pb/auxiliary.db /data/golftrack-pb/auxiliary.db-wal /data/golftrack-pb/auxiliary.db-shm
# restart the container
```

To restore manually without restarting the app:

```bash
docker run --rm \
  -e LITESTREAM_ACCESS_KEY_ID="..." \
  -e LITESTREAM_SECRET_ACCESS_KEY="..." \
  -e LITESTREAM_BUCKET="golftrack-backup" \
  -e LITESTREAM_ENDPOINT="https://nyc3.digitaloceanspaces.com" \
  -v $(pwd):/out \
  --entrypoint litestream \
  golftrack-pb \
  restore -config /app/litestream.yml /out/data.db
```

## How it ships

- **CI builds and pushes it.** The `build` job in
  `.github/workflows/deploy.yml` builds the `pocketbase/` context and pushes
  `ghcr.io/<owner>/golftrack:latest` plus an immutable `:pocketbase-<sha>`.
  The `pocketbase` job (vet, build, test) gates that build. Dev branches
  get `:pocketbase-dev` from `.github/workflows/deploy-dev.yml`.
- **The deploy scripts run it.** `bin/deploy-dev.sh` and `bin/deploy-prod.sh`
  start `golftrack-pb` (8090, published as 8000 on dev and 3000 in
  production) and health-check `/api/version` against the deploying commit's
  short SHA.
