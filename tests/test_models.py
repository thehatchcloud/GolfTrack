import pytest
from django.contrib.auth import get_user_model
from django.db import IntegrityError, transaction
from django.db.models import ProtectedError

from courses.models import Course, CourseHole
from rounds.models import Round, RoundHole, Shot

User = get_user_model()


@pytest.fixture
def user(db):
    return User.objects.create_user(username="alice", password="x")


@pytest.fixture
def course(db):
    course = Course.objects.create(name="Pebble Beach", hole_count=18)
    for n in range(1, 19):
        CourseHole.objects.create(course=course, hole_number=n, par=4)
    return course


def test_user_role_defaults_to_user_and_is_admin_property(user):
    assert user.role == User.Role.USER
    assert user.is_admin is False
    user.role = User.Role.ADMIN
    assert user.is_admin is True


def test_course_total_par(course):
    assert course.total_par == 72


def test_course_hole_number_is_unique_per_course(course):
    with pytest.raises(IntegrityError), transaction.atomic():
        CourseHole.objects.create(course=course, hole_number=1, par=3)


def test_one_in_progress_round_per_user(user, course):
    Round.objects.create(user=user, course=course, status=Round.Status.IN_PROGRESS)

    with pytest.raises(IntegrityError), transaction.atomic():
        Round.objects.create(user=user, course=course, status=Round.Status.IN_PROGRESS)


def test_in_progress_unique_does_not_block_completed_rounds(user, course):
    Round.objects.create(user=user, course=course, status=Round.Status.COMPLETED)
    Round.objects.create(user=user, course=course, status=Round.Status.COMPLETED)
    # A fresh in-progress round is still allowed alongside completed ones.
    Round.objects.create(user=user, course=course, status=Round.Status.IN_PROGRESS)
    assert Round.objects.filter(user=user).count() == 3


def test_different_users_can_each_have_an_in_progress_round(course):
    a = User.objects.create_user(username="a", password="x")
    b = User.objects.create_user(username="b", password="x")
    Round.objects.create(user=a, course=course, status=Round.Status.IN_PROGRESS)
    Round.objects.create(user=b, course=course, status=Round.Status.IN_PROGRESS)
    assert Round.objects.filter(status=Round.Status.IN_PROGRESS).count() == 2


def test_round_hole_number_unique_per_round(user, course):
    rnd = Round.objects.create(user=user, course=course, status=Round.Status.IN_PROGRESS)
    RoundHole.objects.create(round=rnd, hole_number=1, par=4)
    with pytest.raises(IntegrityError), transaction.atomic():
        RoundHole.objects.create(round=rnd, hole_number=1, par=4)


def test_shot_number_unique_per_round_hole(user, course):
    rnd = Round.objects.create(user=user, course=course, status=Round.Status.IN_PROGRESS)
    hole = RoundHole.objects.create(round=rnd, hole_number=1, par=4)
    Shot.objects.create(round_hole=hole, shot_number=1, club="Driver")
    with pytest.raises(IntegrityError), transaction.atomic():
        Shot.objects.create(round_hole=hole, shot_number=1, club="7i")


def test_deleting_round_cascades_to_holes_and_shots(user, course):
    rnd = Round.objects.create(user=user, course=course, status=Round.Status.IN_PROGRESS)
    hole = RoundHole.objects.create(round=rnd, hole_number=1, par=4)
    Shot.objects.create(round_hole=hole, shot_number=1, club="Driver")

    rnd.delete()

    assert RoundHole.objects.count() == 0
    assert Shot.objects.count() == 0


def test_course_with_rounds_is_protected_from_deletion(user, course):
    Round.objects.create(user=user, course=course, status=Round.Status.IN_PROGRESS)
    with pytest.raises(ProtectedError):
        course.delete()
