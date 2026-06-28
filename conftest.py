from pathlib import Path

import pytest


def pytest_configure(config):
    # WhiteNoise warns on startup if STATIC_ROOT doesn't exist.
    # staticfiles/ is gitignored and removed by `make clean`, so create it here.
    (Path(__file__).parent / "staticfiles").mkdir(exist_ok=True)


@pytest.fixture(autouse=True)
def _clear_admin_emails(settings):
    # The sync_admin_role signal rewrites user.role on every login based on
    # ADMIN_EMAILS.  If ADMIN_EMAILS is non-empty in the developer's shell
    # (e.g. from a .env file), force_login() in tests would immediately strip
    # the ADMIN role from any test user whose email isn't listed.  Clear it so
    # the signal's early-exit guard always fires and test-set roles are stable.
    settings.ADMIN_EMAILS = set()
