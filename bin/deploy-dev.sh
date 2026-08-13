#!/bin/sh
# Dev deploy, run on the dev server over SSH by .github/workflows/deploy-dev.yml.
#
# Deploys the **PocketBase** app (pocketbase/Dockerfile): golftrack-pb-dev
# listens on 8090, published as 8000:8090 so the public URL
# (https://golftrack-dev.exe.xyz:8000) is unchanged. No migrate step: the
# binary reconciles the database to its embedded schema during its own
# startup. See pocketbase/DEPLOYMENT.md § "Environment variables" for what it
# reads from the environment.
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

# ghcr.io intermittently refuses a perfectly valid credential with
# `Error response from daemon: Get "https://ghcr.io/v2/": denied: denied`, and
# the workflow's retry step fires a second later — inside the same bad window —
# so a momentary registry refusal reds the whole deploy. Give the registry
# calls a few attempts with a growing backoff instead. Anything that is a real
# authorization problem (expired or unscoped GHCR_TOKEN) still fails, just a
# minute later and with every attempt on the record.
retry_registry() {
  what="$1"
  shift
  attempt=1
  while :; do
    if "$@"; then
      return 0
    fi
    if [ "$attempt" -ge 4 ]; then
      echo "::error::${what} failed after ${attempt} attempts against ghcr.io. If every attempt said 'denied', check that the GHCR_TOKEN secret is a current PAT with read:packages — see DEPLOYMENT.md."
      return 1
    fi
    echo "${what} failed (attempt ${attempt}) — retrying in $((attempt * 5))s..."
    sleep $((attempt * 5))
    attempt=$((attempt + 1))
  done
}

ghcr_login() {
  echo "$GHCR_TOKEN" | docker login ghcr.io -u "$OWNER" --password-stdin
}

retry_registry "docker login ghcr.io" ghcr_login
retry_registry "docker pull $IMAGE" docker pull "$IMAGE"

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

echo "Containers running on the dev host:"
docker ps --format '  {{.Names}}  {{.Image}}  {{.Status}}  {{.Ports}}'

# The workflow greps this exact marker out of the SSH step's captured stdout
# to prove the script ran at all — see "Verify the deploy script ran" in
# .github/workflows/deploy-dev.yml. Keep the wording in sync with it.
echo "golftrack-pb-dev is healthy and running ${short_sha}."
echo "Deployed branch '${BRANCH}' — https://golftrack-dev.exe.xyz:8000"
