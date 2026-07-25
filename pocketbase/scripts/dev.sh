#!/usr/bin/env bash
#
# Download the pinned PocketBase binary (if needed) and run the local dev
# server against the repo's hooks directory.
#
#   pocketbase/scripts/dev.sh            # serve on 127.0.0.1:8090
#   PB_PORT=9000 pocketbase/scripts/dev.sh
#
# The binary and pb_data live in pocketbase/.local/, which is gitignored.
set -euo pipefail

PB_VERSION="${PB_VERSION:-0.39.9}"
PB_HOST="${PB_HOST:-127.0.0.1}"
PB_PORT="${PB_PORT:-8090}"
PB_SUPERUSER_EMAIL="${PB_SUPERUSER_EMAIL:-dev@golftrack.local}"
PB_SUPERUSER_PASSWORD="${PB_SUPERUSER_PASSWORD:-devdevdevdev}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOCAL="$ROOT/.local"
BIN="$LOCAL/pocketbase"

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

case "$(uname -s)" in
  Linux) OS=linux ;;
  Darwin) OS=darwin ;;
  *) echo "unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac

mkdir -p "$LOCAL"

if [ ! -x "$BIN" ] || [ "$("$BIN" --version 2>/dev/null | awk '{print $NF}')" != "$PB_VERSION" ]; then
  url="https://github.com/pocketbase/pocketbase/releases/download/v${PB_VERSION}/pocketbase_${PB_VERSION}_${OS}_${ARCH}.zip"
  echo "Downloading PocketBase ${PB_VERSION} (${OS}/${ARCH})..."
  curl -fsSL -o "$LOCAL/pocketbase.zip" "$url"
  unzip -o -q "$LOCAL/pocketbase.zip" -d "$LOCAL"
  rm -f "$LOCAL/pocketbase.zip"
  chmod +x "$BIN"
fi

# Idempotent: upsert leaves an existing superuser's password as given.
"$BIN" superuser upsert "$PB_SUPERUSER_EMAIL" "$PB_SUPERUSER_PASSWORD" \
  --dir "$LOCAL/pb_data" >/dev/null

echo "Admin UI:    http://${PB_HOST}:${PB_PORT}/_/"
echo "Superuser:   ${PB_SUPERUSER_EMAIL} / ${PB_SUPERUSER_PASSWORD}"
echo "Apply schema in another shell:"
echo "  python3 pocketbase/scripts/apply_schema.py --url http://${PB_HOST}:${PB_PORT}"
echo

# --automigrate=0: pb_schema.json is the source of truth for this repo, so the
# dev server must not write auto-migration files behind our back. See the
# "Schema changes" section of pocketbase/README.md.
exec "$BIN" serve \
  --http="${PB_HOST}:${PB_PORT}" \
  --dir "$LOCAL/pb_data" \
  --hooksDir "$ROOT/hooks" \
  --migrationsDir "$LOCAL/pb_migrations" \
  --automigrate=0
