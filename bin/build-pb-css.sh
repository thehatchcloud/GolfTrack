#!/bin/sh
# Build Tailwind CSS for the PocketBase frontend (Phase 7B).
#
# Scans pocketbase/internal/web and writes
# pocketbase/internal/web/static/css/app.css. The output is committed, because
# the binary embeds it with go:embed — a checkout has to be able to `go build`
# without a Tailwind toolchain.
set -e

cd "$(dirname "$0")/.."

TAILWIND_VERSION="${TAILWIND_VERSION:-v3.4.13}"
BIN="./bin/tailwindcss"
IN="./pocketbase/internal/web/static/src/input.css"
OUT="./pocketbase/internal/web/static/css/app.css"

# A local Node install can run the same compiler, which is the escape hatch when
# the standalone binary cannot be downloaded (a locked-down network, an
# unsupported platform).
if [ -n "$TAILWIND_NODE_CLI" ]; then
  node "$TAILWIND_NODE_CLI" -c tailwind.config.js -i "$IN" -o "$OUT" --minify "$@"
  exit 0
fi

if [ ! -x "$BIN" ]; then
  case "$(uname -s)-$(uname -m)" in
    Linux-x86_64)  TARGET="tailwindcss-linux-x64" ;;
    Linux-aarch64) TARGET="tailwindcss-linux-arm64" ;;
    Darwin-arm64)  TARGET="tailwindcss-macos-arm64" ;;
    Darwin-x86_64) TARGET="tailwindcss-macos-x64" ;;
    *) echo "Unsupported platform for tailwindcss standalone CLI" >&2; exit 1 ;;
  esac
  URL="https://github.com/tailwindlabs/tailwindcss/releases/download/${TAILWIND_VERSION}/${TARGET}"
  echo "Downloading tailwindcss ${TAILWIND_VERSION} (${TARGET})..."
  curl -sSL "$URL" -o "$BIN"
  chmod +x "$BIN"
fi

"$BIN" -c tailwind.config.js -i "$IN" -o "$OUT" --minify "$@"
