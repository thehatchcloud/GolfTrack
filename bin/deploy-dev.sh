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

# Registry calls get a few attempts with a growing backoff, because a dropped
# connection or a 5xx from ghcr.io is worth waiting out — the workflow's own
# retry step fires a second after the first, too soon to help.
#
# A `denied`/`unauthorized` answer is different in kind: it is the registry's
# verdict on the credential, and it will be the same verdict in five seconds
# and in five minutes. Retrying it only buries the real cause under a minute
# of identical failures that read like a flake. So that case stops immediately
# and says what to actually go and fix.
#
# Duplicated verbatim in bin/deploy-prod.sh: appleboy/ssh-action's script_path
# copies exactly one file to the host, so a shared helper file would never
# arrive. Keep the two copies in sync.
retry_registry() {
  what="$1"
  shift
  attempt=1
  out="$(mktemp)"
  while :; do
    if "$@" >"$out" 2>&1; then
      cat "$out"
      rm -f "$out"
      return 0
    fi
    cat "$out"

    if grep -Eqi 'denied|unauthorized|authentication required|forbidden' "$out"; then
      echo "::error::ghcr.io refused the credential for '${what}'. This is an authorization failure, not a transient one — retrying will not help. The GHCR_TOKEN secret is almost certainly an expired or revoked PAT; issue a new classic PAT with read:packages and update the repository secret. See DEPLOYMENT.md § 'Rotating GHCR_TOKEN'."
      rm -f "$out"
      return 1
    fi

    if [ "$attempt" -ge 4 ]; then
      echo "::error::${what} still failing after ${attempt} attempts against ghcr.io — giving up. The output above is from the last attempt."
      rm -f "$out"
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

# A failed login is not fatal on its own — the pull below is what decides,
# since a package whose visibility is Public pulls fine with no credential at
# all (DEPLOYMENT.md offers that as an alternative to GHCR_TOKEN). Same order
# as bin/deploy-prod.sh.
retry_registry "docker login ghcr.io" ghcr_login || true
if ! retry_registry "docker pull $IMAGE" docker pull "$IMAGE"; then
  echo "::error::docker pull $IMAGE failed — aborting rather than running whatever is already cached locally."
  exit 1
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

echo "Containers running on the dev host:"
docker ps --format '  {{.Names}}  {{.Image}}  {{.Status}}  {{.Ports}}'

# The workflow greps this exact marker out of the SSH step's captured stdout
# to prove the script ran at all — see "Verify the deploy script ran" in
# .github/workflows/deploy-dev.yml. Keep the wording in sync with it.
echo "golftrack-pb-dev is healthy and running ${short_sha}."
echo "Deployed branch '${BRANCH}' — https://golftrack-dev.exe.xyz:8000"
