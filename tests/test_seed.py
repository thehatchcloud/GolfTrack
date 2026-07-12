import pytest
from django.contrib.auth import get_user_model
from django.core.management import call_command

from core.management.commands.seed import SeedGuardError, check_seed_guards
from courses.models import Course
from rounds.models import Round, Shot

User = get_user_model()


# --- guards -----------------------------------------------------------------

def test_seed_guard_refuses_when_debug_is_false():
    with pytest.raises(SeedGuardError, match="DJANGO_DEBUG is not enabled"):
        check_seed_guards(debug=False, db_name="/app/db.sqlite3")


def test_seed_guard_refuses_production_db_path():
    with pytest.raises(SeedGuardError, match="does not look like a dev DB"):
        check_seed_guards(debug=True, db_name="/data/prod.db")


def test_seed_guard_refuses_when_db_name_unset():
    with pytest.raises(SeedGuardError, match="does not look like a dev DB"):
        check_seed_guards(debug=True, db_name="")


def test_seed_guard_passes_for_default_dev_db():
    check_seed_guards(debug=True, db_name="/app/db.sqlite3")


def test_seed_guard_passes_for_dev_db():
    check_seed_guards(debug=True, db_name="/app/dev.db")


def test_seed_guard_passes_for_test_db():
    check_seed_guards(debug=True, db_name="/app/test.db")


def test_seed_guard_passes_for_in_memory_db():
    check_seed_guards(debug=True, db_name=":memory:")


def test_seed_command_refuses_in_production(settings):
    settings.DEBUG = False
    with pytest.raises(SeedGuardError):
        call_command("seed")


# --- data ---------------------------------------------------------------
#
# pytest-django's test environment forces settings.DEBUG = False (mirroring
# Django's own test runner), which the guard above correctly refuses to seed
# against. These tests opt back into DEBUG = True to exercise the command's
# happy path against the real (file-based) test database.

def test_seed_command_creates_sample_data(db, settings):
    settings.DEBUG = True
    call_command("seed")

    user = User.objects.get(email="test@example.com")
    assert user.get_full_name() == "Test User"

    municipal = Course.objects.get(name="Sample Municipal")
    assert municipal.hole_count == 9
    assert municipal.total_par == 36

    lakeside = Course.objects.get(name="Lakeside Country Club")
    assert lakeside.hole_count == 18
    assert lakeside.total_par == 72

    completed = Round.objects.get(status=Round.Status.COMPLETED)
    assert completed.course_id == municipal.id
    assert completed.total_strokes == 41
    assert completed.total_par == 36
    assert completed.relative_to_par == 5
    assert completed.note == "Driver was solid. Putting needs work."

    in_progress = Round.objects.get(status=Round.Status.IN_PROGRESS)
    assert in_progress.course_id == lakeside.id
    assert in_progress.current_hole == 4

    assert Shot.objects.filter(round_hole__round=completed).count() == 14
    assert Shot.objects.filter(round_hole__round=in_progress).count() == 14


def test_seed_command_is_idempotent(db, settings):
    settings.DEBUG = True
    call_command("seed")
    call_command("seed")

    assert User.objects.count() == 1
    assert Course.objects.count() == 2
    assert Round.objects.count() == 2
