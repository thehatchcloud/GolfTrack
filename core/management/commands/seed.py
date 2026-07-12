"""Wipe and reseed the database with sample data for local development.

Port of prisma/seed.ts (see #92). Same guard rules and sample data: a test
user, a 9-hole and an 18-hole course, one completed round, and one
in-progress round.
"""
import datetime as dt

from django.conf import settings
from django.contrib.auth import get_user_model
from django.core.management.base import BaseCommand, CommandError
from django.db import transaction

from courses.models import Course, CourseHole
from rounds.models import Round, RoundHole, Shot

User = get_user_model()

# Paths that look like a local dev/test database. Anything else (e.g. the
# production "/data/prod.db" contract from DATABASE_URL) is refused.
_DEV_DB_MARKERS = ("db.sqlite3", "dev.db", "test.db", ":memory:")

SAMPLE_MUNICIPAL_PARS = [4, 3, 5, 4, 4, 3, 5, 4, 4]
LAKESIDE_PARS = [4, 5, 3, 4, 4, 5, 3, 4, 4, 4, 3, 5, 4, 4, 3, 5, 4, 4]


class SeedGuardError(CommandError):
    """Raised when seeding would run against a non-development database."""


def check_seed_guards(*, debug: bool | None = None, db_name: str | None = None) -> None:
    if debug is None:
        debug = settings.DEBUG
    if db_name is None:
        db_name = str(settings.DATABASES["default"]["NAME"])

    if not debug:
        raise SeedGuardError(
            "Refusing to seed: DJANGO_DEBUG is not enabled (looks like production)"
        )

    if not any(marker in db_name for marker in _DEV_DB_MARKERS):
        raise SeedGuardError(f"Refusing to seed: database does not look like a dev DB ({db_name})")


def _create_shots(hole: RoundHole, clubs: list[str]) -> None:
    Shot.objects.bulk_create(
        [Shot(round_hole=hole, shot_number=n, club=club) for n, club in enumerate(clubs, start=1)]
    )


class Command(BaseCommand):
    help = "Wipe and reseed the database with sample data for local development."

    def handle(self, *args, **options):
        check_seed_guards()

        with transaction.atomic():
            Shot.objects.all().delete()
            RoundHole.objects.all().delete()
            Round.objects.all().delete()
            CourseHole.objects.all().delete()
            Course.objects.all().delete()
            User.objects.all().delete()

            test_user = User.objects.create_user(
                username="test@example.com",
                email="test@example.com",
                first_name="Test",
                last_name="User",
            )

            sample_municipal = Course.objects.create(name="Sample Municipal", hole_count=9)
            CourseHole.objects.bulk_create(
                CourseHole(course=sample_municipal, hole_number=n, par=par)
                for n, par in enumerate(SAMPLE_MUNICIPAL_PARS, start=1)
            )

            lakeside = Course.objects.create(name="Lakeside Country Club", hole_count=18)
            CourseHole.objects.bulk_create(
                CourseHole(course=lakeside, hole_number=n, par=par)
                for n, par in enumerate(LAKESIDE_PARS, start=1)
            )

            completed_strokes = [5, 3, 6, 4, 5, 3, 6, 5, 4]
            total_strokes = sum(completed_strokes)
            total_par = sum(SAMPLE_MUNICIPAL_PARS)
            completed_round = Round.objects.create(
                user=test_user,
                course=sample_municipal,
                status=Round.Status.COMPLETED,
                current_hole=9,
                started_at=dt.datetime(2026, 5, 10, 14, 0, tzinfo=dt.UTC),
                finished_at=dt.datetime(2026, 5, 10, 16, 10, tzinfo=dt.UTC),
                note="Driver was solid. Putting needs work.",
                total_strokes=total_strokes,
                total_par=total_par,
                relative_to_par=total_strokes - total_par,
            )
            completed_holes = {
                n: RoundHole.objects.create(
                    round=completed_round, hole_number=n, par=par, strokes=strokes
                )
                for n, (par, strokes) in enumerate(
                    zip(SAMPLE_MUNICIPAL_PARS, completed_strokes, strict=True), start=1
                )
            }
            _create_shots(completed_holes[1], ["Driver", "7i", "PW", "Putter", "Putter"])
            _create_shots(completed_holes[2], ["8i", "SW", "Putter"])
            _create_shots(completed_holes[3], ["Driver", "5i", "9i", "SW", "Putter", "Putter"])

            in_progress_strokes = {1: 4, 2: 5, 3: 3, 4: 2}
            in_progress_round = Round.objects.create(
                user=test_user,
                course=lakeside,
                status=Round.Status.IN_PROGRESS,
                current_hole=4,
                started_at=dt.datetime(2026, 5, 12, 13, 30, tzinfo=dt.UTC),
            )
            in_progress_holes = {
                n: RoundHole.objects.create(
                    round=in_progress_round,
                    hole_number=n,
                    par=par,
                    strokes=in_progress_strokes.get(n, 0),
                )
                for n, par in enumerate(LAKESIDE_PARS, start=1)
            }
            _create_shots(in_progress_holes[1], ["Driver", "8i", "GW", "Putter"])
            _create_shots(in_progress_holes[2], ["Driver", "5i", "7i", "SW", "Putter"])
            _create_shots(in_progress_holes[3], ["6i", "Putter", "Putter"])
            _create_shots(in_progress_holes[4], ["Driver", "9i"])

        self.stdout.write(
            self.style.SUCCESS(
                "Seed complete: "
                f"courses=[{sample_municipal.name!r}, {lakeside.name!r}] "
                f"completed_round_id={completed_round.id} "
                f"in_progress_round_id={in_progress_round.id}"
            )
        )
