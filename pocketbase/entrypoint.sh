#!/bin/sh
set -e

# Unlike the Django entrypoint (../entrypoint.sh), there is no separate
# migration step to sequence before the app starts: golftrack-pb reconciles
# the database to the embedded schema (main.go / schema.go) as part of its
# own startup, inside the same process litestream supervises below. So
# litestream's `-exec` — which, per its v0.5 contract, never runs its
# argument through `sh -c` — only ever has to wrap this one invocation, never
# a shell pipeline.
SERVE="./golftrack-pb serve --http=0.0.0.0:8090 --dir /data"

if [ -n "$LITESTREAM_BUCKET" ]; then
  if [ ! -f /data/data.db ]; then
    echo "Restoring database from replica..."
    litestream restore -if-replica-exists -config /app/litestream.yml /data/data.db \
      || echo "No replica found, starting fresh."
  fi
  if [ ! -f /data/auxiliary.db ]; then
    litestream restore -if-replica-exists -config /app/litestream.yml /data/auxiliary.db \
      || echo "No auxiliary replica found, starting fresh."
  fi
  exec litestream replicate -config /app/litestream.yml -exec "$SERVE"
else
  # No replication (dev server / local docker run) — run golftrack-pb directly.
  exec $SERVE
fi
