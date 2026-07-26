#!/bin/sh
# Dev deploy, run on the dev server over SSH by .github/workflows/deploy-dev.yml.
#
# Lives in the repo rather than inline in the workflow so the deploy step can be
# retried without a second copy of the script drifting from the first — see the
# retry note in that workflow. It is also `sh -n`-checkable here, which an
# inline YAML block is not.
#
# Environment comes from the workflow's `envs:` list: OWNER, GHCR_TOKEN,
# DJANGO_SECRET_KEY, ADMIN_EMAILS, BRANCH.
#
# Re-running this is safe: it stops and replaces the container each time.

set -e
IMAGE="ghcr.io/${OWNER}/golftrack:django-dev"

echo "$GHCR_TOKEN" | docker login ghcr.io -u "$OWNER" --password-stdin
docker pull "$IMAGE"

docker stop golftrack-dev 2>/dev/null || true
docker rm   golftrack-dev 2>/dev/null || true

docker run -d \
  --name golftrack-dev \
  --restart unless-stopped \
  -p 8000:8000 \
  -e DATABASE_URL="file:/data/dev.db" \
  -e DJANGO_SECRET_KEY="$DJANGO_SECRET_KEY" \
  -e DJANGO_DEBUG=true \
  -e DJANGO_ALLOW_PASSWORD_LOGIN=true \
  -e DJANGO_ALLOWED_HOSTS="golftrack-dev.exe.xyz" \
  -e DJANGO_CSRF_TRUSTED_ORIGINS="https://golftrack-dev.exe.xyz:8000" \
  -e ADMIN_EMAILS="$ADMIN_EMAILS" \
  -v golftrack-dev-data:/data \
  "$IMAGE"

# docker run -d returns before the app boots; verify the container
# actually serves /api/health and fail loudly with logs if it does not.
echo "Waiting for golftrack-dev to become healthy..."
healthy=0
for _ in $(seq 1 20); do
  sleep 3
  if docker exec golftrack-dev curl -fsS -H "Host: golftrack-dev.exe.xyz" \
       http://127.0.0.1:8000/api/health >/dev/null 2>&1; then
    healthy=1
    break
  fi
done
if [ "$healthy" != "1" ]; then
  echo "::error::golftrack-dev failed its health check after ~60s. Recent logs:"
  docker logs --tail 120 golftrack-dev 2>&1 || true
  exit 1
fi

docker image prune -f
echo "Deployed branch '${BRANCH}' — https://golftrack-dev.exe.xyz:8000"
