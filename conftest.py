from pathlib import Path


def pytest_configure(config):
    # WhiteNoise warns on startup if STATIC_ROOT doesn't exist.
    # staticfiles/ is gitignored and removed by `make clean`, so create it here.
    (Path(__file__).parent / "staticfiles").mkdir(exist_ok=True)
