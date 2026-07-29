#!/bin/sh
# Production deploy, run on the production server over SSH by the `deploy` job
# in .github/workflows/deploy.yml.
#
# As of the Phase 10 cutover (#131) this deploys the **PocketBase** app
# (pocketbase/Dockerfile), not the Django one. What changed on this host:
#
#   - Container name golftrack-pb, replacing golftrack. The Django container is
#     stopped and removed below; it holds host port 3000, so it could not stay
#     up alongside this one even if we wanted it to.
#   - golftrack-pb listens on 8090, so the mapping is 3000:8090. The public URL
#     (https://<host>:3000) is unchanged, as it was across the Next.js → Django
#     cutover before it.
#   - Its own data directory, /data/golftrack-pb, so the Django database under
#     /data/golftrack survives untouched for rollback. Litestream likewise
#     replicates to the `pocketbase/*` bucket paths rather than `django`
#     (pocketbase/litestream.yml), so the two generations never collide.
#   - None of the DJANGO_* variables are needed: no secret key, no
#     ALLOWED_HOSTS, no CSRF origins, no DATABASE_URL — see
#     pocketbase/DEPLOYMENT.md § "Environment variables" for why each has no
#     equivalent.
#   - No migrate step: the binary reconciles the database to its embedded
#     schema during its own startup, inside the process Litestream supervises.
#
# Rollback is bin/rollback-prod.sh, which puts the Django container back from
# the ghcr.io/<owner>/golftrack:django-latest tag.
#
# Lives in the repo rather than inline in the workflow so the deploy step can be
# retried without a second copy of the script drifting from the first — see the
# retry note in that workflow. It is also `sh -n`-checkable here, which an
# inline YAML block is not.
#
# Environment comes from the workflow's `envs:` list: GHCR_TOKEN, OWNER,
# GIT_SHA, the LITESTREAM_* secrets, the OAuth client id/secret pairs, and
# ADMIN_EMAILS.
#
# Re-running this is safe: it stops and replaces the container each time.
#
# Note: no blanket `set -e`, carried over verbatim from the inline version
# this replaced. Adding it wholesale would be a behaviour change to the
# production path and is deliberately left as a separate decision. The two
# steps below whose silent failure would leave the *old* container running
# while the deploy still reports success — the pull and the run — check
# their own exit status explicitly instead.

IMAGE="ghcr.io/${OWNER}/golftrack:latest"

echo "$GHCR_TOKEN" | docker login ghcr.io -u "$OWNER" --password-stdin
if ! docker pull "$IMAGE"; then
  echo "::error::docker pull $IMAGE failed — aborting rather than running whatever is already cached locally."
  exit 1
fi

# The Django container this replaces. Removing it is the cutover on this host.
# Only the container goes: /data/golftrack (its SQLite database) and the
# `django` Litestream replica are both left in place, so a rollback restores a
# running app rather than an empty one.
if [ -n "$(docker ps -aq -f name='^golftrack$')" ]; then
  echo "Removing the Django container (golftrack) — PocketBase takes over port 3000."
  docker stop golftrack >/dev/null 2>&1 || true
  docker rm   golftrack >/dev/null 2>&1 || true
fi

docker stop golftrack-pb 2>/dev/null || true
docker rm   golftrack-pb 2>/dev/null || true

mkdir -p /data/golftrack-pb
# The container runs as the non-root app user (uid/gid 1001). On a bind mount
# the host directory's ownership overrides the image's, so the app user must own
# /data/golftrack-pb or SQLite writes fail with "attempt to write a readonly
# database" and the schema sync crashes on boot. Same constraint the Django
# image had on /data/golftrack, same uid.
sudo chown -R 1001:1001 /data/golftrack-pb

if ! docker run -d \
  --name golftrack-pb \
  --restart unless-stopped \
  -p 3000:8090 \
  -e LITESTREAM_ACCESS_KEY_ID="$LITESTREAM_ACCESS_KEY_ID" \
  -e LITESTREAM_SECRET_ACCESS_KEY="$LITESTREAM_SECRET_ACCESS_KEY" \
  -e LITESTREAM_BUCKET="$LITESTREAM_BUCKET" \
  -e LITESTREAM_ENDPOINT="$LITESTREAM_ENDPOINT" \
  -e GOOGLE_CLIENT_ID="$GOOGLE_CLIENT_ID" \
  -e GOOGLE_CLIENT_SECRET="$GOOGLE_CLIENT_SECRET" \
  -e MICROSOFT_CLIENT_ID="$MICROSOFT_CLIENT_ID" \
  -e MICROSOFT_CLIENT_SECRET="$MICROSOFT_CLIENT_SECRET" \
  -e ADMIN_EMAILS="$ADMIN_EMAILS" \
  -v /data/golftrack-pb:/data \
  "$IMAGE"; then
  echo "::error::docker run failed to start the golftrack-pb container — aborting rather than health-checking whatever container was already there."
  exit 1
fi

# docker run -d returns as soon as the container starts, so a crash on boot
# still looks like a successful deploy. Poll from inside the container and fail
# the deploy with the container logs if it never comes up.
#
# The endpoint is /api/version rather than /api/health: PocketBase's own
# /api/health answers liveness but carries no build identity (its envelope is
# pinned by parity_test.go), and a 200 alone isn't proof the *new* image is
# what answered — if the run above had somehow left the previous container
# standing, this would still see a 200 from the old code. GIT_SHA (the commit
# this deploy is for, from the workflow) is compared against the short SHA
# /api/version reports (pocketbase/internal/version), so a stale container
# fails the check instead of passing it silently.
echo "Waiting for golftrack-pb to become healthy..."
healthy=0
short_sha="${GIT_SHA%"${GIT_SHA#???????}"}" # first 7 chars, POSIX-sh only
for _ in $(seq 1 20); do
  sleep 3
  response="$(docker exec golftrack-pb curl -fsS \
       http://127.0.0.1:8090/api/version 2>/dev/null)" || continue
  case "$response" in
    *"$short_sha"*) healthy=1 ;;
  esac
  [ "$healthy" = "1" ] && break
done
if [ "$healthy" != "1" ]; then
  echo "::error::golftrack-pb never reported version containing ${short_sha} on /api/version after ~60s (last response: ${response:-<none>}). Recent logs:"
  docker logs --tail 120 golftrack-pb 2>&1 || true
  exit 1
fi

# Cutover evidence, printed into the workflow log: what is actually running on
# this host, and that nothing named golftrack (the Django container) survived
# it.
echo "Containers running on the production host:"
docker ps --format '  {{.Names}}  {{.Image}}  {{.Status}}  {{.Ports}}'
if [ -n "$(docker ps -aq -f name='^golftrack$')" ]; then
  echo "::error::the Django container golftrack is still present after the cutover deploy."
  exit 1
fi
echo "Django container golftrack: absent."

# The workflow greps this exact marker out of the SSH step's captured stdout
# to prove the script ran at all — see "Verify the deploy script ran" in
# .github/workflows/deploy.yml. Keep the wording in sync with it.
echo "golftrack-pb is healthy and running ${short_sha}."

docker image prune -f
