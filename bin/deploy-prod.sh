#!/bin/sh
# Production deploy, run on the production server over SSH by the `deploy` job
# in .github/workflows/deploy.yml.
#
# Deploys the **PocketBase** app (pocketbase/Dockerfile): container name
# golftrack-pb, listening on 8090 and published as 3000:8090 so the public URL
# (https://<host>:3000) is unchanged. Its data directory is /data/golftrack-pb;
# Litestream replicates to the `pocketbase/*` bucket paths
# (pocketbase/litestream.yml). No migrate step: the binary reconciles the
# database to its embedded schema during its own startup, inside the process
# Litestream supervises.
#
# Rollback to the pre-PocketBase Django app, if ever needed, is a manual
# recovery from the preserved `ghcr.io/<owner>/golftrack:django-latest` image
# and the `django-final` git tag — see DEPLOYMENT.md § "Rollback to Django".
# The in-repo rollback script was retired in Phase 11 (#132) once production
# had proven stable on PocketBase.
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
# Duplicated verbatim in bin/deploy-dev.sh: appleboy/ssh-action's script_path
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

# A failed login is deliberately not fatal on its own — the pull below is what
# decides, since a package whose visibility is Public pulls fine with no
# credential at all.
retry_registry "docker login ghcr.io" ghcr_login || true
if ! retry_registry "docker pull $IMAGE" docker pull "$IMAGE"; then
  echo "::error::docker pull $IMAGE failed — aborting rather than running whatever is already cached locally."
  exit 1
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

echo "Containers running on the production host:"
docker ps --format '  {{.Names}}  {{.Image}}  {{.Status}}  {{.Ports}}'

# The workflow greps this exact marker out of the SSH step's captured stdout
# to prove the script ran at all — see "Verify the deploy script ran" in
# .github/workflows/deploy.yml. Keep the wording in sync with it.
echo "golftrack-pb is healthy and running ${short_sha}."

docker image prune -f
