#!/bin/sh
# Dev deploy, run on the dev server over SSH by .github/workflows/deploy-dev.yml.
#
# As of the Phase 10 cutover (#131) this deploys the **PocketBase** app
# (pocketbase/Dockerfile), not the Django one. Differences that matter here:
#
#   - golftrack-pb listens on 8090, not 8000, so the host port mapping is
#     8000:8090 — the public URL (https://golftrack-dev.exe.xyz:8000) is
#     unchanged.
#   - It needs none of the DJANGO_* variables: no secret key (PocketBase keeps
#     its encryption key in the data directory), no ALLOWED_HOSTS (it does not
#     validate the Host header), no CSRF origins (writes are token-authenticated
#     JSON calls), no DATABASE_URL (--dir /data plays that role). See
#     pocketbase/DEPLOYMENT.md § "Environment variables".
#   - There is no migrate step: the binary reconciles the database to its
#     embedded schema during its own startup.
#
# Lives in the repo rather than inline in the workflow so the deploy step can be
# retried without a second copy of the script drifting from the first — see the
# retry note in that workflow. It is also `sh -n`-checkable here, which an
# inline YAML block is not.
#
# Environment comes from the workflow's `envs:` list: OWNER, GHCR_TOKEN,
# GIT_SHA, ADMIN_EMAILS, BRANCH.
#
# Re-running this is safe: it stops and replaces the container each time.

set -e
IMAGE="ghcr.io/${OWNER}/golftrack:pocketbase-dev"

echo "$GHCR_TOKEN" | docker login ghcr.io -u "$OWNER" --password-stdin
docker pull "$IMAGE"

# The Django container this replaces. Removing it is the cutover on this host:
# it holds host port 8000, so leaving it up would both fail the `docker run`
# below on a port conflict and leave the old app serving. Its named volume
# (golftrack-dev-data) is deliberately left alone — that, plus the still-present
# :django-dev image in GHCR, is the dev-side rollback.
if [ -n "$(docker ps -aq -f name='^golftrack-dev$')" ]; then
  echo "Removing the Django dev container (golftrack-dev) — PocketBase takes over port 8000."
  docker stop golftrack-dev >/dev/null 2>&1 || true
  docker rm   golftrack-dev >/dev/null 2>&1 || true
fi

docker stop golftrack-pb-dev 2>/dev/null || true
docker rm   golftrack-pb-dev 2>/dev/null || true

# No OAuth on dev, so password login is on. Self-service signup stays
# OAuth2-only regardless, which is why the dev account is created by hand once
# — see DEPLOYMENT.md § "Dev server setup".
docker run -d \
  --name golftrack-pb-dev \
  --restart unless-stopped \
  -p 8000:8090 \
  -e GOLFTRACK_ALLOW_PASSWORD_LOGIN=true \
  -e ADMIN_EMAILS="$ADMIN_EMAILS" \
  -v golftrack-pb-dev-data:/data \
  "$IMAGE"

# docker run -d returns before the app boots; verify the container actually
# serves before calling the deploy good, and fail loudly with logs if it does
# not. The endpoint is /api/version, not /api/health: PocketBase's own
# /api/health reports liveness but carries no build identity (its envelope is
# pinned by parity_test.go), and a bare 200 isn't proof it's the *new* image —
# set -e above stops this script on a failed pull/run, but compare against
# GIT_SHA's short form too so a leftover stale container (from outside this
# script) can't pass silently.
echo "Waiting for golftrack-pb-dev to become healthy..."
healthy=0
short_sha="${GIT_SHA%"${GIT_SHA#???????}"}" # first 7 chars, POSIX-sh only
for _ in $(seq 1 20); do
  sleep 3
  response="$(docker exec golftrack-pb-dev curl -fsS \
       http://127.0.0.1:8090/api/version 2>/dev/null)" || continue
  case "$response" in
    *"$short_sha"*) healthy=1 ;;
  esac
  [ "$healthy" = "1" ] && break
done
if [ "$healthy" != "1" ]; then
  echo "::error::golftrack-pb-dev never reported version containing ${short_sha} on /api/version after ~60s (last response: ${response:-<none>}). Recent logs:"
  docker logs --tail 120 golftrack-pb-dev 2>&1 || true
  exit 1
fi

docker image prune -f

# Cutover evidence, printed into the workflow log: what is actually running on
# this host, and that nothing named golftrack-dev (the Django container)
# survived it.
echo "Containers running on the dev host:"
docker ps --format '  {{.Names}}  {{.Image}}  {{.Status}}  {{.Ports}}'
if [ -n "$(docker ps -aq -f name='^golftrack-dev$')" ]; then
  echo "::error::the Django container golftrack-dev is still present after the cutover deploy."
  exit 1
fi
echo "Django container golftrack-dev: absent."

# The workflow greps this exact marker out of the SSH step's captured stdout
# to prove the script ran at all — see "Verify the deploy script ran" in
# .github/workflows/deploy-dev.yml. Keep the wording in sync with it.
echo "golftrack-pb-dev is healthy and running ${short_sha}."
echo "Deployed branch '${BRANCH}' — https://golftrack-dev.exe.xyz:8000"
