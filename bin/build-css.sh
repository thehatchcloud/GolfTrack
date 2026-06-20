#!/bin/sh
# Build Tailwind CSS using the standalone CLI (no Node runtime dependency).
#
# Downloads the tailwindcss standalone binary on first run, then compiles
# static/src/input.css -> static/css/app.css. In Docker (Phase 7) this runs
# at image build time, before `collectstatic`.
set -e

cd "$(dirname "$0")/.."

TAILWIND_VERSION="${TAILWIND_VERSION:-v3.4.13}"
BIN="./bin/tailwindcss"

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

"$BIN" -i ./static/src/input.css -o ./static/css/app.css --minify "$@"
