"""Concurrency-sensitive behaviour, mirroring the races exercised in
tests/rounds.test.ts against the Next.js/Prisma service layer.

These use real threads and django_db(transaction=True) so each thread opens
its own SQLite connection against the same on-disk test DB (see the `TEST`
`NAME` override in config/settings.py) rather than sharing one wrapped,
never-committed test transaction. That is what actually exercises SQLite's
single-writer locking the way concurrent requests would in production.
"""
import threading

import pytest
from django.contrib.auth import get_user_model
from django.db import connections

from core.exceptions import ConflictError
from courses.models import Course, CourseHole
from rounds.models import Round, Shot
from rounds.services import add_shot, complete_round, create_round, delete_shot, update_shot

User = get_user_model()

pytestmark = pytest.mark.django_db(transaction=True)


def run_concurrently(funcs):
    """Run zero-arg callables in parallel threads; returns (result, exc) pairs
    in the same order, similar to Promise.allSettled."""
    results = [None] * len(funcs)

    def _wrap(index, fn):
        try:
            results[index] = (fn(), None)
        except Exception as exc:  # noqa: BLE001 - surfaced to the caller
            results[index] = (None, exc)
        finally:
            connections.close_all()

    threads = [threading.Thread(target=_wrap, args=(i, fn)) for i, fn in enumerate(funcs)]
    for t in threads:
        t.start()
    for t in threads:
        t.join()
    return results


@pytest.fixture
def user():
    return User.objects.create_user(username="alice", password="x")


@pytest.fixture
def course():
    c = Course.objects.create(name="Test Course", hole_count=9)
    for n in range(1, 10):
        CourseHole.objects.create(course=c, hole_number=n, par=4)
    return c


def test_concurrent_add_shot_calls_both_succeed(user, course):
    round_ = create_round(user, course.id)

    results = run_concurrently(
        [
            lambda: add_shot(user, round_.id, 1, "Driver"),
            lambda: add_shot(user, round_.id, 1, "7i"),
        ]
    )

    assert all(exc is None for _, exc in results), results

    hole = round_.holes.get(hole_number=1)
    assert hole.strokes == 2
    assert list(hole.shots.values_list("shot_number", flat=True).order_by("shot_number")) == [1, 2]


def test_concurrent_create_round_only_allows_one_in_progress(user, course):
    results = run_concurrently(
        [
            lambda: create_round(user, course.id),
            lambda: create_round(user, course.id),
        ]
    )

    succeeded = [r for r, exc in results if exc is None]
    failed = [exc for _, exc in results if exc is not None]

    assert len(succeeded) == 1
    assert len(failed) == 1
    assert isinstance(failed[0], ConflictError)
    assert "already in progress" in str(failed[0])
    assert Round.objects.filter(user=user).count() == 1


def test_concurrent_update_and_delete_shot_leaves_consistent_state(user, course):
    round_ = create_round(user, course.id)
    add_shot(user, round_.id, 1, "Driver")
    add_shot(user, round_.id, 1, "7i")
    shot_id = round_.holes.get(hole_number=1).shots.get(shot_number=1).id

    update_result, delete_result = run_concurrently(
        [
            lambda: update_shot(user, round_.id, 1, shot_id, "5i"),
            lambda: delete_shot(user, round_.id, 1, shot_id),
        ]
    )

    # The delete targets a shot that exists when the race starts, so under
    # IMMEDIATE serialization it must always win. The concurrent update either
    # lands first (and is then deleted) or finds the shot already gone.
    assert delete_result[1] is None, f"delete should succeed: {delete_result[1]}"

    hole = round_.holes.get(hole_number=1)
    assert hole.strokes == 1
    assert hole.shots.count() == 1
    remaining = hole.shots.get()
    assert remaining.shot_number == 1
    assert remaining.club == "7i"


def test_concurrent_delete_shot_on_same_shot_allows_only_one_to_succeed(user, course):
    round_ = create_round(user, course.id)
    add_shot(user, round_.id, 1, "Driver")
    add_shot(user, round_.id, 1, "7i")
    shot_id = round_.holes.get(hole_number=1).shots.get(shot_number=1).id

    results = run_concurrently(
        [
            lambda: delete_shot(user, round_.id, 1, shot_id),
            lambda: delete_shot(user, round_.id, 1, shot_id),
        ]
    )

    succeeded = [r for r, exc in results if exc is None]
    failed = [exc for _, exc in results if exc is not None]
    assert len(succeeded) == 1
    assert len(failed) == 1
    assert "Shot not found" in str(failed[0])

    hole = round_.holes.get(hole_number=1)
    assert hole.strokes == 1
    assert list(hole.shots.values_list("shot_number", flat=True)) == [1]


def test_concurrent_complete_round_and_add_shot_never_adds_shots_after_finish(user, course):
    round_ = create_round(user, course.id)

    complete_result, add_result = run_concurrently(
        [
            lambda: complete_round(user, round_.id, "Done"),
            lambda: add_shot(user, round_.id, 1, "Driver"),
        ]
    )

    # completing an in-progress round has no precondition that the concurrent
    # add can invalidate, so it must always succeed. The add either lands first
    # (shot created before finish) or is rejected once the round is completed.
    assert complete_result[1] is None, f"complete should succeed: {complete_result[1]}"

    final_round = Round.objects.get(pk=round_.id)
    assert final_round.status == Round.Status.COMPLETED
    assert final_round.finished_at is not None

    for shot in Shot.objects.filter(round_hole__round=final_round):
        assert shot.created_at <= final_round.finished_at
