#!/usr/bin/env bash
#
# Build the GolfTrack PocketBase binary and run the local dev server.
#
#   pocketbase/scripts/dev.sh            # serve on 127.0.0.1:8090
#   PB_PORT=9000 pocketbase/scripts/dev.sh
#
# Requires a Go toolchain (go.mod pins the language version; go will fetch the
# matching toolchain itself if the installed one is older). The compiled
# binary and pb_data live in pocketbase/.local/, which is gitignored.
#
# The binary embeds pb_schema.json and reconciles the database to it on every
# startup, so no separate schema-apply step is needed. See "Schema changes" in
# pocketbase/README.md.
#
# Authentication is configured from the environment, which this script passes
# through — export GOOGLE_CLIENT_ID/SECRET, MICROSOFT_CLIENT_ID/SECRET,
# ADMIN_EMAILS or GOLFTRACK_ALLOW_PASSWORD_LOGIN before running it. With none
# of them set the app itself has no sign-in method; the Admin UI still opens.
# See pocketbase/AUTH.md.
set -euo pipefail

PB_HOST="${PB_HOST:-127.0.0.1}"
PB_PORT="${PB_PORT:-8090}"
PB_SUPERUSER_EMAIL="${PB_SUPERUSER_EMAIL:-dev@golftrack.local}"
PB_SUPERUSER_PASSWORD="${PB_SUPERUSER_PASSWORD:-devdevdevdev}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOCAL="$ROOT/.local"
BIN="$LOCAL/golftrack-pb"

command -v go >/dev/null || {
  echo "dev.sh needs the Go toolchain (https://go.dev/dl/)" >&2
  exit 1
}

mkdir -p "$LOCAL"

echo "Building golftrack-pb..."
(cd "$ROOT" && go build -o "$BIN" .)

# Idempotent: upsert leaves an existing superuser's password as given.
"$BIN" superuser upsert "$PB_SUPERUSER_EMAIL" "$PB_SUPERUSER_PASSWORD" \
  --dir "$LOCAL/pb_data" >/dev/null

# Mirrors internal/authenv: a provider needs both halves of its credentials,
# and the password switch takes the values Django's _bool_env accepts.
signin=""
if [ -n "${GOOGLE_CLIENT_ID:-}" ] && [ -n "${GOOGLE_CLIENT_SECRET:-}" ]; then
  signin="${signin}google "
fi
if [ -n "${MICROSOFT_CLIENT_ID:-}" ] && [ -n "${MICROSOFT_CLIENT_SECRET:-}" ]; then
  signin="${signin}microsoft "
fi
case "$(printf '%s' "${GOLFTRACK_ALLOW_PASSWORD_LOGIN:-}" | tr '[:upper:]' '[:lower:]')" in
  1 | true | yes | on) signin="${signin}password " ;;
esac

echo "Admin UI:    http://${PB_HOST}:${PB_PORT}/_/"
echo "Superuser:   ${PB_SUPERUSER_EMAIL} / ${PB_SUPERUSER_PASSWORD}"
echo "Schema:      embedded pb_schema.json, synced at startup"
echo "Sign-in:     ${signin:-none configured (see pocketbase/AUTH.md)}"
echo

exec "$BIN" serve \
  --http="${PB_HOST}:${PB_PORT}" \
  --dir "$LOCAL/pb_data"
