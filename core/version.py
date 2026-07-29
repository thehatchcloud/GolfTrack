"""Resolves the running build's version.

The version is a calendar date plus the short git commit SHA it was built
from, e.g. ``2026.07.29+a3f9c2e`` — enough to answer "what's actually
running" without hand-maintained version bumps. Read order:

1. The ``VERSION`` file baked into the Docker image at build time (see
   ``Dockerfile``), which pins both the date and SHA to the actual build.
2. A live ``git`` lookup, for local development (``manage.py runserver``)
   where no VERSION file exists.
3. ``<today>+dev`` if neither is available (e.g. a source archive with no
   ``.git`` directory).
"""
import subprocess
from datetime import UTC, datetime
from functools import lru_cache
from pathlib import Path

_VERSION_FILE = Path(__file__).resolve().parent.parent / "VERSION"


@lru_cache(maxsize=1)
def get_version() -> str:
    if _VERSION_FILE.exists():
        version = _VERSION_FILE.read_text().strip()
        if version:
            return version

    date = datetime.now(UTC).strftime("%Y.%m.%d")
    return f"{date}+{_git_short_sha() or 'dev'}"


def _git_short_sha() -> str | None:
    try:
        result = subprocess.run(
            ["git", "rev-parse", "--short", "HEAD"],
            capture_output=True,
            text=True,
            timeout=2,
            check=True,
            cwd=_VERSION_FILE.parent,
        )
    except (OSError, subprocess.CalledProcessError):
        return None
    return result.stdout.strip() or None
