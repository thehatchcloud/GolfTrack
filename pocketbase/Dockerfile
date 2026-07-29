# PocketBase-based image for the deployment shape from Phase 9 (#130).
#
# Not yet the shipped artifact — see ../CLAUDE.md and ../DEPLOYMENT.md. The
# repository root Dockerfile (Django, gunicorn, WhiteNoise, Litestream)
# remains what CI builds and the exe.dev VM runs until the Phase 10 cutover
# (#131). This image lets the container be built and exercised ahead of that
# cutover; see DEPLOYMENT.md in this directory.
#
# Build from this directory (module root):
#   docker build -t golftrack-pb pocketbase/

FROM golang:1.25-bookworm AS builder
WORKDIR /src

# Install deps before copying source so this layer is cached on code-only
# changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# GIT_SHA is passed by the builder (--build-arg GIT_SHA=<commit>); .git itself
# isn't part of this build context. internal/version.Get() reads GitSHA/
# BuildDate first, and falls back to a live `git rev-parse` for local (non-
# Docker) development — see internal/version/version.go.
ARG GIT_SHA=dev

# modernc.org/sqlite is a pure-Go SQLite driver — no cgo, so CGO_ENABLED=0
# produces a fully static binary with no libc to match at runtime. The
# frontend's Tailwind output, vendored JS and pb_schema.json are all
# go:embed'd source (see main.go, schema.go, internal/web) and already
# committed, so `go build` is the whole asset pipeline; unlike the Django
# image, there is no separate collectstatic/Tailwind step to run here.
#
# GIT_SHA is truncated to 7 characters to match the short form the local
# `git rev-parse --short HEAD` fallback produces, and the Django image's
# `cut -c1-7` (root Dockerfile) — so /api/version reads the same either way,
# and the deploy scripts' short-SHA check has one shape to look for.
RUN CGO_ENABLED=0 go build -ldflags "\
      -X github.com/thehatchcloud/golftrack/pocketbase/internal/version.GitSHA=$(echo "${GIT_SHA}" | cut -c1-7) \
      -X github.com/thehatchcloud/golftrack/pocketbase/internal/version.BuildDate=$(date -u +%Y.%m.%d) \
    " -o /out/golftrack-pb .

FROM debian:bookworm-slim
WORKDIR /app

# ca-certificates: the OAuth2 token exchange (Google, Microsoft Entra ID) is
# HTTPS. curl: the HEALTHCHECK below.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl \
 && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/golftrack-pb ./golftrack-pb

# Litestream streams the SQLite databases to S3-compatible storage at
# runtime. entrypoint.sh restores from the replica on first boot and
# supervises golftrack-pb via `litestream replicate -exec`, only when
# LITESTREAM_BUCKET is set — see entrypoint.sh and litestream.yml.
COPY --from=litestream/litestream:latest /usr/local/bin/litestream /usr/local/bin/litestream

COPY litestream.yml entrypoint.sh ./

RUN groupadd --system --gid 1001 app \
 && useradd --system --uid 1001 --gid 1001 --no-create-home app \
 && mkdir -p /data && chown app:app /data \
 && chmod +x entrypoint.sh

USER app
EXPOSE 8090

# Matches parity_test.go's TestHealthEndpointShape: PocketBase's own health
# envelope ({"code":200,"message":"API is healthy.",...}), not the Django
# app's {"status":"ok"} — any 200 response is a pass here, so the difference
# in body shape doesn't need to be reproduced in the probe itself.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8090/api/health || exit 1

CMD ["./entrypoint.sh"]
