#!/bin/sh
set -e

if [ -n "$LITESTREAM_BUCKET" ]; then
  if [ ! -f /data/prod.db ]; then
    echo "No database found — attempting restore from replica..."
    litestream restore -if-replica-exists -config /app/litestream.yml /data/prod.db \
      || echo "No replica found, starting fresh."
  fi
  exec litestream replicate \
    -config /app/litestream.yml \
    -exec "node_modules/.bin/prisma migrate deploy && node server.js"
else
  node_modules/.bin/prisma migrate deploy
  exec node server.js
fi
