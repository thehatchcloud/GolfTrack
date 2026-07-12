FROM python:3.13-slim
# pyproject.toml declares requires-python>=3.14, but pydantic 2.x is not yet
# compatible with Python 3.14. Using 3.13 until that is resolved upstream.

WORKDIR /app

RUN pip install --no-cache-dir uv

# Install deps before copying source so this layer is cached on code-only changes
COPY pyproject.toml .
RUN uv pip install --system --no-cache \
    "django>=6.0,<6.1" \
    "django-ninja>=1.4,<2.0" \
    "django-allauth[socialaccount]>=65.0,<66.0" \
    "python-dotenv>=1.0" \
    "whitenoise>=6.7" \
    "gunicorn>=23.0" \
    "uvicorn>=0.30"

COPY . .

# bin/build-css.sh downloads the tailwindcss standalone CLI over HTTPS.
RUN apt-get update \
 && apt-get install -y --no-install-recommends curl ca-certificates \
 && rm -rf /var/lib/apt/lists/*

# Compile the real Tailwind stylesheet — static/css/app.css is committed as a
# placeholder (see its header comment) so collectstatic has something to find
# locally without running Tailwind first; this overwrites it with the real build.
RUN ./bin/build-css.sh

# Collect static files at build time so whitenoise serves them at runtime.
# DATABASE_URL points at a throwaway path — collectstatic doesn't need the DB.
RUN DJANGO_SECRET_KEY=build-placeholder \
    DJANGO_DEBUG=false \
    DATABASE_URL=/tmp/build.db \
    python manage.py collectstatic --noinput

# Litestream streams the SQLite DB to S3-compatible storage at runtime. The
# entrypoint restores from the replica on first boot and supervises gunicorn
# via `litestream replicate -exec`. Only active when LITESTREAM_BUCKET is set.
COPY --from=litestream/litestream:latest /usr/local/bin/litestream /usr/local/bin/litestream

RUN groupadd --system --gid 1001 app \
 && useradd --system --uid 1001 --gid 1001 --no-create-home app \
 && mkdir -p /data && chown app:app /data

# litestream.yml and entrypoint.sh arrive via `COPY . .` above; just make the
# entrypoint executable.
RUN chmod +x entrypoint.sh

USER app
EXPOSE 8000
CMD ["./entrypoint.sh"]
