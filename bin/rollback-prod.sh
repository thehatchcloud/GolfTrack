#!/bin/sh
# Roll production back from the PocketBase app to Django (Phase 10, #131).
#
# This is the rollback half of bin/deploy-prod.sh. It is run **by hand on the
# production VM**, not by CI: rolling back is a decision, and the whole point
# is that it works when the pipeline is what you have lost confidence in.
#
#   ssh <prod-vmhost>
#   GHCR_TOKEN=… OWNER=thehatchcloud \
#   DJANGO_SECRET_KEY=… DEPLOY_HOST=… \
#   GOOGLE_CLIENT_ID=… GOOGLE_CLIENT_SECRET=… \
#   MICROSOFT_CLIENT_ID=… MICROSOFT_CLIENT_SECRET=… \
#   ADMIN_EMAILS=… \
#   LITESTREAM_ACCESS_KEY_ID=… LITESTREAM_SECRET_ACCESS_KEY=… \
#   LITESTREAM_BUCKET=… LITESTREAM_ENDPOINT=… \
#   sh rollback-prod.sh
#
# (Copy this file over with scp, or curl it from the repo. The values are the
# same GitHub Actions secrets deploy.yml already passes — see DEPLOYMENT.md
# § "Rollback to Django".)
#
# Why this restores a *working* app and not an empty one: the cutover deploy
# only removed the Django container. /data/golftrack — its SQLite database —
# and the `django` Litestream replica path are both untouched by the PocketBase
# deploy, which uses /data/golftrack-pb and the `pocketbase/*` replica paths.
# So this is a container swap, not a data restore, and it costs a pull plus a
# boot rather than the 30 minutes budgeted in #131.
#
# What it does NOT do: roll back the repository. `main` still builds the
# PocketBase image, so the next push to main re-deploys it and undoes this.
# Revert the cutover commit (or disable the deploy workflow) if the rollback is
# meant to hold.

set -e

: "${OWNER:?set OWNER to the GitHub org/user that owns the GHCR package}"
: "${GHCR_TOKEN:?set GHCR_TOKEN to a token with read:packages}"
: "${DJANGO_SECRET_KEY:?set DJANGO_SECRET_KEY — the Django app will not boot without it}"
: "${DEPLOY_HOST:?set DEPLOY_HOST to the public hostname, e.g. golftrack.exe.xyz}"

# The image the `build` job copied aside before it first overwrote :latest with
# the PocketBase build — see "Preserve the Django image for rollback" in
# .github/workflows/deploy.yml. The local copy is gone (the cutover deploy's
# `docker image prune -f` collected it once :latest moved), so this pulls.
IMAGE="ghcr.io/${OWNER}/golftrack:django-latest"

echo "$GHCR_TOKEN" | docker login ghcr.io -u "$OWNER" --password-stdin
if ! docker pull "$IMAGE"; then
  echo "::error::docker pull $IMAGE failed. If the tag does not exist, the cutover build never ran its preserve step — recover the Django image by digest from the GHCR package's version history instead."
  exit 1
fi

echo "Stopping the PocketBase container (golftrack-pb) — Django takes back port 3000."
docker stop golftrack-pb >/dev/null 2>&1 || true
docker rm   golftrack-pb >/dev/null 2>&1 || true

# /data/golftrack-pb is deliberately left in place: it holds whatever the
# PocketBase app recorded while it was live, which is exactly what you want to
# inspect after a rollback. Litestream also still has it under `pocketbase/*`.

docker stop golftrack >/dev/null 2>&1 || true
docker rm   golftrack >/dev/null 2>&1 || true

sudo chown -R 1001:1001 /data/golftrack

if ! docker run -d \
  --name golftrack \
  --restart unless-stopped \
  -p 3000:8000 \
  -e DATABASE_URL="file:/data/prod.db" \
  -e DJANGO_SECRET_KEY="$DJANGO_SECRET_KEY" \
  -e DJANGO_DEBUG=false \
  -e DJANGO_ALLOWED_HOSTS="$DEPLOY_HOST" \
  -e DJANGO_CSRF_TRUSTED_ORIGINS="https://$DEPLOY_HOST:3000" \
  -e LITESTREAM_ACCESS_KEY_ID="$LITESTREAM_ACCESS_KEY_ID" \
  -e LITESTREAM_SECRET_ACCESS_KEY="$LITESTREAM_SECRET_ACCESS_KEY" \
  -e LITESTREAM_BUCKET="$LITESTREAM_BUCKET" \
  -e LITESTREAM_ENDPOINT="$LITESTREAM_ENDPOINT" \
  -e GOOGLE_CLIENT_ID="$GOOGLE_CLIENT_ID" \
  -e GOOGLE_CLIENT_SECRET="$GOOGLE_CLIENT_SECRET" \
  -e MICROSOFT_CLIENT_ID="$MICROSOFT_CLIENT_ID" \
  -e MICROSOFT_CLIENT_SECRET="$MICROSOFT_CLIENT_SECRET" \
  -e ADMIN_EMAILS="$ADMIN_EMAILS" \
  -v /data/golftrack:/data \
  "$IMAGE"; then
  echo "::error::docker run failed to start the Django container."
  exit 1
fi

# Django's health endpoint, not PocketBase's /api/version: different app, and
# its Host header has to satisfy ALLOWED_HOSTS. No SHA comparison here — the
# point of a rollback is "whatever the last Django build was", so any healthy
# response from this image is the success condition.
echo "Waiting for golftrack (Django) to become healthy..."
healthy=0
for _ in $(seq 1 20); do
  sleep 3
  if docker exec golftrack curl -fsS -H "Host: ${DEPLOY_HOST}" \
       http://127.0.0.1:8000/api/health >/dev/null 2>&1; then
    healthy=1
    break
  fi
done
if [ "$healthy" != "1" ]; then
  echo "::error::golftrack did not answer /api/health after ~60s. Recent logs:"
  docker logs --tail 120 golftrack 2>&1 || true
  exit 1
fi

echo "Containers running on the production host:"
docker ps --format '  {{.Names}}  {{.Image}}  {{.Status}}  {{.Ports}}'
echo "Rolled back to Django (${IMAGE}). Revert the cutover commit on main, or the next push re-deploys PocketBase."
