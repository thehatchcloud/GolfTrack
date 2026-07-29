#!/bin/sh
# Dev deploy, run on the dev server over SSH by .github/workflows/deploy-dev.yml.
#
# Lives in the repo rather than inline in the workflow so the deploy step can be
# retried without a second copy of the script drifting from the first — see the
# retry note in that workflow. It is also `sh -n`-checkable here, which an
# inline YAML block is not.
#
# Environment comes from the workflow's `envs:` list: OWNER, GHCR_TOKEN,
# GIT_SHA, DJANGO_SECRET_KEY, ADMIN_EMAILS, BRANCH.
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

# docker run -d returns before the app boots; verify the container actually
# serves /api/health and fail loudly with logs if it does not. A bare 200
# isn't proof it's the *new* image — set -e above stops this script on a
# failed pull/run, but compare against GIT_SHA's short form too so a
# leftover stale container (from outside this script) can't pass silently.
echo "Waiting for golftrack-dev to become healthy..."
healthy=0
short_sha="${GIT_SHA%"${GIT_SHA#???????}"}" # first 7 chars, POSIX-sh only
for _ in $(seq 1 20); do
  sleep 3
  response="$(docker exec golftrack-dev curl -fsS -H "Host: golftrack-dev.exe.xyz" \
       http://127.0.0.1:8000/api/health 2>/dev/null)" || continue
  case "$response" in
    *"$short_sha"*) healthy=1 ;;
  esac
  [ "$healthy" = "1" ] && break
done
if [ "$healthy" != "1" ]; then
  echo "::error::golftrack-dev never reported version containing ${short_sha} on /api/health after ~60s (last response: ${response:-<none>}). Recent logs:"
  docker logs --tail 120 golftrack-dev 2>&1 || true
  exit 1
fi

docker image prune -f
echo "Deployed branch '${BRANCH}' — https://golftrack-dev.exe.xyz:8000"
