"""The Django half of the Phase 8 (#129) performance comparison.

`pocketbase/perf_test.go` counts the statements each PocketBase read path
issues, and `pocketbase/bench_test.go` times them.  This does both for the
Django code those paths were ported from, so that
``pocketbase/performance_report.md`` can put the two side by side.

It is not a pass/fail test and asserts nothing about speed.  It is a measuring
instrument whose output is a table for a human to read, so it is skipped unless
``GOLFTRACK_BASELINE=1`` is set::

    GOLFTRACK_BASELINE=1 .venv/bin/pytest tests/test_django_baseline.py -s

# Why this measures the service layer and not the HTTP endpoints

The obvious shape for this file is Django's test client against `/api/rounds/…`,
matching the PocketBase side request for request.  That was not possible in the
container this phase was measured in, and the reason is worth stating precisely
so nobody goes looking for a bug that is not there.

`config/urls.py` imports the django-ninja API at module scope, so *every* Django
HTTP request depends on pydantic importing.  On CPython **3.14.0rc2** it does
not — pydantic's `eval_type_backport` trips an assertion — and rc2 was the only
3.14 build available in that container, so `uv venv --python 3.14` resolved to
it.  With that interpreter the repository's own suite fails too
(`tests/test_api.py` 40 of 40, `tests/test_views.py` 11 of 11), which is how we
know it is the interpreter and not this file.  CI runs `uv python install 3.14`
against the network and gets a released build, where this very likely does not
happen; **there is no evidence of a defect in the Django app itself.**

What runs regardless is `rounds/services.py` and `courses/services.py`, which is
the half worth comparing anyway.  Phase 8 changed how many queries a response
costs and how much work assembling one does; both live in the service layer on
the Django side and in `internal/hooks/domain` on the PocketBase side.
Serialisation — pydantic there, `encoding/json` here — is not compared, and the
report says so.

If you are running this on a released 3.14 and Django's HTTP layer imports
cleanly, extending this file to use `django.test.Client` would make the
comparison tighter still.

# What "the same work" means

A Django service function returns a queryset that has not been fully evaluated:
`prefetch_related` does the extra queries eagerly, but the Python objects are
built lazily.  Timing the call alone would therefore time less work than a
response does.  Each measurement below walks the whole object graph the
corresponding `RoundDetailOut`/`CourseOut` schema reads — course, holes, shots —
so that both stacks are timed having actually produced every value that ends up
in the JSON.
"""

import os
import statistics
import time

import pytest
from django.contrib.auth import get_user_model
from django.db import connection
from django.test.utils import CaptureQueriesContext

from courses.models import Course, CourseHole
from courses.services import get_course_by_id, list_courses
from rounds.models import Round, RoundHole, Shot
from rounds.services import (
    add_shot,
    get_in_progress_round,
    get_round_by_id,
    list_completed_rounds,
    set_current_hole,
    undo_last_shot,
)

User = get_user_model()

pytestmark = pytest.mark.skipif(
    not os.getenv("GOLFTRACK_BASELINE"),
    reason="set GOLFTRACK_BASELINE=1 to take the Django performance baseline",
)

# Matches pocketbase/routes_test.go's play fixture, so both sides answer
# questions of the same size: an 18-hole course, a round mid-play with every
# hole snapshotted and three shots down one of them, one completed round in the
# history.
HOLE_COUNT = 18
PAR = 4

# Enough iterations that the median is stable and a run is seconds.
ITERATIONS = 200
WARMUP = 20


@pytest.fixture
def fixture(db):
    user = User.objects.create_user(username="bench", password="x")

    course = Course.objects.create(name="Play Links", hole_count=HOLE_COUNT)
    CourseHole.objects.bulk_create(
        CourseHole(course=course, hole_number=n, par=PAR) for n in range(1, HOLE_COUNT + 1)
    )

    done = Round.objects.create(
        user=user,
        course=course,
        status=Round.Status.COMPLETED,
        play_mode=Round.PlayMode.FULL,
        current_hole=1,
        total_strokes=4,
        total_par=4,
        relative_to_par=0,
    )
    RoundHole.objects.create(round=done, hole_number=1, par=PAR, strokes=0)

    playing = Round.objects.create(
        user=user,
        course=course,
        status=Round.Status.IN_PROGRESS,
        play_mode=Round.PlayMode.FULL,
        current_hole=1,
    )
    RoundHole.objects.bulk_create(
        RoundHole(round=playing, hole_number=n, par=PAR, strokes=0)
        for n in range(1, HOLE_COUNT + 1)
    )

    hole3 = RoundHole.objects.get(round=playing, hole_number=3)
    for number, club in enumerate(["Driver", "7 Iron", "Wedge"], start=1):
        Shot.objects.create(round_hole=hole3, shot_number=number, club=club)
    RoundHole.objects.filter(pk=hole3.pk).update(strokes=3)

    return {"user": user, "round": playing, "course": course}


# -----------------------------------------------------------------------------
# walking the graph a response serialises
# -----------------------------------------------------------------------------


def walk_course(course):
    """Everything CourseOut reads, including the derived total par."""
    return (
        course.id,
        course.name,
        course.hole_count,
        course.is_archived,
        [(h.id, h.hole_number, h.par) for h in course.holes.all()],
    )


def walk_round(round_):
    """Everything RoundOut reads."""
    return (
        round_.id,
        round_.status,
        round_.play_mode,
        round_.started_at,
        round_.finished_at,
        round_.note,
        round_.current_hole,
        round_.total_strokes,
        round_.total_par,
        round_.relative_to_par,
        walk_course(round_.course),
    )


def walk_round_detail(round_):
    """Everything RoundDetailOut reads: RoundOut plus holes with their shots."""
    if round_ is None:
        return None
    return (
        walk_round(round_),
        [
            (
                h.id,
                h.hole_number,
                h.par,
                h.strokes,
                [(s.id, s.shot_number, s.club) for s in h.shots.all()],
            )
            for h in round_.holes.all()
        ],
    )


# -----------------------------------------------------------------------------
# measuring
# -----------------------------------------------------------------------------


def measure(name, call, reset=None):
    """Time and count queries for `call`.

    `reset` is run untimed after each iteration to undo a write, the same device
    benchRequest uses in bench_test.go: without it an accumulating write times a
    result that grows with the run.
    """
    for _ in range(WARMUP):
        call()
        if reset:
            reset()

    # Query count first, on its own iteration — CaptureQueriesContext adds
    # per-query bookkeeping that would inflate the timings.
    with CaptureQueriesContext(connection) as captured:
        call()
    queries = len(captured)
    if reset:
        reset()

    samples = []
    for _ in range(ITERATIONS):
        start = time.perf_counter()
        call()
        samples.append(time.perf_counter() - start)
        if reset:
            reset()

    samples.sort()
    return {
        "name": name,
        "queries": queries,
        "mean": statistics.mean(samples),
        "p50": samples[len(samples) // 2],
        "p95": samples[int(len(samples) * 0.95)],
    }


def report(title, results):
    print(f"\n\nDjango baseline — {title}, {ITERATIONS} iterations each\n")
    print(f"{'operation':<34} {'queries':>8} {'mean':>10} {'p50':>10} {'p95':>10}")
    for r in results:
        print(
            f"{r['name']:<34} {r['queries']:>8} "
            f"{r['mean'] * 1e6:>9.0f}µs {r['p50'] * 1e6:>9.0f}µs {r['p95'] * 1e6:>9.0f}µs"
        )
    print()


# -----------------------------------------------------------------------------
# the measurements
# -----------------------------------------------------------------------------


def test_django_read_baseline(fixture):
    """The reads behind every screen, each walked out to the values it serialises."""
    user, round_, course = fixture["user"], fixture["round"], fixture["course"]

    report(
        "reads (service layer + full object graph)",
        [
            measure(
                "in-progress round (detail)",
                lambda: walk_round_detail(get_in_progress_round(user)),
            ),
            measure(
                "round by id (detail)",
                lambda: walk_round_detail(get_round_by_id(user, round_.id)),
            ),
            measure(
                "completed rounds (list)",
                lambda: [walk_round(r) for r in list_completed_rounds(user)],
            ),
            measure(
                "courses (list)",
                lambda: [walk_course(c) for c in list_courses()],
            ),
            measure(
                "course by id",
                lambda: walk_course(get_course_by_id(course.id)),
            ),
        ],
    )


def test_django_write_baseline(fixture):
    """The two writes the play screen makes on every tap."""
    user, round_ = fixture["user"], fixture["round"]

    report(
        "writes (service layer)",
        [
            measure(
                "add shot",
                lambda: add_shot(user, round_.id, 1, "Driver"),
                reset=lambda: undo_last_shot(user, round_.id, 1),
            ),
            measure(
                "set current hole",
                lambda: set_current_hole(user, round_.id, 5),
            ),
        ],
    )
