#!/bin/sh
# Production deploy, run on the production server over SSH by the `deploy` job
# in .github/workflows/deploy.yml.
#
# Lives in the repo rather than inline in the workflow so the deploy step can be
# retried without a second copy of the script drifting from the first — see the
# retry note in that workflow. It is also `sh -n`-checkable here, which an
# inline YAML block is not.
#
# Environment comes from the workflow's `envs:` list: GHCR_TOKEN, OWNER, the
# LITESTREAM_* secrets, DEPLOY_HOST, DJANGO_SECRET_KEY, the OAuth client
# id/secret pairs, and ADMIN_EMAILS.
#
# Re-running this is safe: it stops and replaces the container each time.
#
# Note: no `set -e`, carried over verbatim from the inline version this
# replaced. The health check at the end is what fails the deploy. Adding
# `set -e` would be a behaviour change to the production path and is
# deliberately left as a separate decision.

IMAGE="ghcr.io/${OWNER}/golftrack:latest"

echo "$GHCR_TOKEN" | docker login ghcr.io -u "$OWNER" --password-stdin
docker pull "$IMAGE"

docker stop golftrack 2>/dev/null || true
docker rm   golftrack 2>/dev/null || true

mkdir -p /data/golftrack
# The container runs as the non-root app user (uid/gid 1001). On a
# bind mount the host directory's ownership overrides the image's, so
# the app user must own /data/golftrack or SQLite writes fail with
# "attempt to write a readonly database" and migrate crashes on boot.
sudo chown -R 1001:1001 /data/golftrack

# Host port 3000 keeps the public URL (https://<host>:3000) stable
# across the Next.js -> Django cutover; gunicorn listens on 8000
# inside the container.
docker run -d \
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
  "$IMAGE"

# docker run -d returns as soon as the container starts, so a crash
# on boot still looks like a successful deploy. Poll /api/health from
# inside the container (Host header must satisfy ALLOWED_HOSTS) and
# fail the deploy with the container logs if it never comes up.
echo "Waiting for golftrack to become healthy..."
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
  echo "::error::golftrack failed its health check after ~60s. Recent logs:"
  docker logs --tail 120 golftrack 2>&1 || true
  exit 1
fi
echo "golftrack is healthy."

docker image prune -f
