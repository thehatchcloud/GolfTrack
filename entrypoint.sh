#!/bin/sh
set -e

# Single command string reused by both branches. Litestream v0.5's `-exec` does
# NOT run its argument through `sh -c`, so this must stay a single executable
# (gunicorn) with flags — never a shell pipeline chained with `&&`. Migrations
# therefore run as a separate step *before* `litestream replicate` starts.
GUNICORN="gunicorn config.wsgi:application --bind 0.0.0.0:8000 --workers 2 --timeout 60 --access-logfile -"

if [ -n "$LITESTREAM_BUCKET" ]; then
  if [ ! -f /data/prod.db ]; then
    echo "Restoring database from replica..."
    litestream restore -if-replica-exists -config /app/litestream.yml /data/prod.db \
      || echo "No replica found, starting fresh."
  fi
  python manage.py migrate --noinput
  exec litestream replicate -config /app/litestream.yml -exec "$GUNICORN"
else
  # No replication (dev server / local docker run) — run gunicorn directly.
  python manage.py migrate --noinput
  exec $GUNICORN
fi
