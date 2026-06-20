import pytest
from django.contrib.auth import get_user_model

from core.exceptions import ConflictError, NotFoundError, ValidationError
from courses.models import Course, CourseHole
from courses.services import create_course, get_course_by_id, list_courses, update_course
from rounds.clubs import DEFAULT_CLUBS
from rounds.models import Round, Shot
from rounds.round_play import get_current_hole_position, get_round_play_label_from_mode
from rounds.scoring import calculate_round_totals, format_relative_to_par
from rounds.services import (
    add_shot,
    cancel_round,
    complete_round,
    create_round,
    delete_shot,
    get_in_progress_round,
    set_current_hole,
    undo_last_shot,
    update_shot,
)

User = get_user_model()


@pytest.fixture
def user(db):
    return User.objects.create_user(username="alice", password="x")


@pytest.fixture
def user2(db):
    return User.objects.create_user(username="bob", password="x")


@pytest.fixture
def course(db):
    c = Course.objects.create(name="Test Course", hole_count=18)
    for n in range(1, 19):
        CourseHole.objects.create(course=c, hole_number=n, par=4)
    return c


@pytest.fixture
def nine_hole_course(db):
    c = Course.objects.create(name="Nine Hole", hole_count=9)
    for n in range(1, 10):
        CourseHole.objects.create(course=c, hole_number=n, par=3)
    return c


@pytest.fixture
def round_(user, course):
    return create_round(user, course.id)


# ---------------------------------------------------------------------------
# scoring helpers
# ---------------------------------------------------------------------------

class _Hole:
    def __init__(self, par, strokes):
        self.par = par
        self.strokes = strokes


def test_calculate_round_totals_even():
    totals = calculate_round_totals([_Hole(4, 4), _Hole(3, 3), _Hole(5, 5)])
    assert totals == {"total_par": 12, "total_strokes": 12, "relative_to_par": 0}


def test_calculate_round_totals_over_under():
    totals = calculate_round_totals([_Hole(4, 5), _Hole(3, 2)])
    assert totals["total_strokes"] == 7
    assert totals["total_par"] == 7
    assert totals["relative_to_par"] == 0


def test_format_relative_to_par():
    assert format_relative_to_par(0) == "E"
    assert format_relative_to_par(3) == "+3"
    assert format_relative_to_par(-2) == "-2"


# ---------------------------------------------------------------------------
# round_play helpers
# ---------------------------------------------------------------------------

def test_get_current_hole_position():
    assert get_current_hole_position([1, 2, 3, 4], 3) == 3
    assert get_current_hole_position([10, 11, 12], 10) == 1


def test_get_current_hole_position_missing_returns_1():
    assert get_current_hole_position([1, 2, 3], 99) == 1


def test_get_round_play_label_from_mode():
    assert get_round_play_label_from_mode("front9", 18) == "Front 9"
    assert get_round_play_label_from_mode("back9", 18) == "Back 9"
    assert get_round_play_label_from_mode("full", 9) == "9 Holes"
    assert get_round_play_label_from_mode("full", 18) == "Full Course"


# ---------------------------------------------------------------------------
# clubs
# ---------------------------------------------------------------------------

def test_default_clubs_non_empty():
    assert "Driver" in DEFAULT_CLUBS
    assert "Putter" in DEFAULT_CLUBS


# ---------------------------------------------------------------------------
# create_round
# ---------------------------------------------------------------------------

def test_create_round_snapshots_par(user, course):
    round_ = create_round(user, course.id)
    assert round_.holes.get(hole_number=1).par == 4


def test_create_round_creates_18_holes(user, course):
    round_ = create_round(user, course.id)
    assert round_.holes.count() == 18


def test_create_round_sets_starting_hole(user, course):
    assert create_round(user, course.id).current_hole == 1


def test_create_round_front9(user, course):
    round_ = create_round(user, course.id, play_mode="front9")
    assert round_.holes.count() == 9
    assert list(round_.holes.values_list("hole_number", flat=True)) == list(range(1, 10))


def test_create_round_back9(user, course):
    round_ = create_round(user, course.id, play_mode="back9")
    assert round_.holes.count() == 9
    assert list(round_.holes.values_list("hole_number", flat=True)) == list(range(10, 19))


def test_create_round_front9_on_9hole_course_raises(user, nine_hole_course):
    with pytest.raises(ValidationError):
        create_round(user, nine_hole_course.id, play_mode="front9")


def test_create_round_conflict_raises(user, course, round_):
    with pytest.raises(ConflictError):
        create_round(user, course.id)


def test_create_round_missing_course_raises(user):
    with pytest.raises(NotFoundError):
        create_round(user, 99999)


# ---------------------------------------------------------------------------
# get_in_progress_round
# ---------------------------------------------------------------------------

def test_get_in_progress_round_found(user, round_):
    result = get_in_progress_round(user)
    assert result is not None and result.id == round_.id


def test_get_in_progress_round_none_after_complete(user, round_):
    complete_round(user, round_.id)
    assert get_in_progress_round(user) is None


# ---------------------------------------------------------------------------
# add_shot
# ---------------------------------------------------------------------------

def test_add_shot_increments_strokes(user, round_):
    hole = add_shot(user, round_.id, 1, "Driver")
    assert hole.strokes == 1
    assert hole.shots.count() == 1


def test_add_shot_sequential_numbers(user, round_):
    add_shot(user, round_.id, 1, "Driver")
    hole = add_shot(user, round_.id, 1, "7i")
    assert list(hole.shots.values_list("shot_number", flat=True).order_by("shot_number")) == [1, 2]


def test_add_shot_wrong_user_raises(user2, round_):
    with pytest.raises(NotFoundError):
        add_shot(user2, round_.id, 1, "Driver")


def test_add_shot_on_completed_round_raises(user, round_):
    complete_round(user, round_.id)
    with pytest.raises(ConflictError):
        add_shot(user, round_.id, 1, "Driver")


# ---------------------------------------------------------------------------
# undo_last_shot
# ---------------------------------------------------------------------------

def test_undo_last_shot(user, round_):
    add_shot(user, round_.id, 1, "Driver")
    hole = undo_last_shot(user, round_.id, 1)
    assert hole.strokes == 0
    assert hole.shots.count() == 0


def test_undo_on_empty_hole_is_noop(user, round_):
    hole = undo_last_shot(user, round_.id, 1)
    assert hole.strokes == 0


def test_undo_removes_highest_shot_only(user, round_):
    add_shot(user, round_.id, 1, "Driver")
    add_shot(user, round_.id, 1, "7i")
    hole = undo_last_shot(user, round_.id, 1)
    assert hole.strokes == 1
    assert hole.shots.get().shot_number == 1


# ---------------------------------------------------------------------------
# delete_shot (mid-hole, renumbering)
# ---------------------------------------------------------------------------

def test_delete_shot_renumbers_subsequent(user, round_):
    add_shot(user, round_.id, 1, "Driver")  # 1
    add_shot(user, round_.id, 1, "7i")      # 2
    add_shot(user, round_.id, 1, "PW")      # 3
    shot_2 = Shot.objects.get(round_hole__round=round_, shot_number=2)
    hole = delete_shot(user, round_.id, 1, shot_2.id)
    numbers = list(hole.shots.values_list("shot_number", flat=True).order_by("shot_number"))
    assert numbers == [1, 2]
    assert hole.strokes == 2


def test_delete_shot_updates_strokes_cache(user, round_):
    add_shot(user, round_.id, 1, "Driver")
    add_shot(user, round_.id, 1, "7i")
    shot_1 = Shot.objects.get(round_hole__round=round_, shot_number=1)
    hole = delete_shot(user, round_.id, 1, shot_1.id)
    assert hole.strokes == 1


def test_delete_shot_not_found_raises(user, round_):
    with pytest.raises(NotFoundError):
        delete_shot(user, round_.id, 1, 99999)


# ---------------------------------------------------------------------------
# update_shot
# ---------------------------------------------------------------------------

def test_update_shot_changes_club(user, round_):
    add_shot(user, round_.id, 1, "Driver")
    shot = Shot.objects.get(round_hole__round=round_, shot_number=1)
    updated = update_shot(user, round_.id, 1, shot.id, "3W")
    assert updated.club == "3W"


def test_update_shot_on_completed_round_raises(user, round_):
    add_shot(user, round_.id, 1, "Driver")
    shot = Shot.objects.get(round_hole__round=round_, shot_number=1)
    complete_round(user, round_.id)
    with pytest.raises(ConflictError):
        update_shot(user, round_.id, 1, shot.id, "3W")


# ---------------------------------------------------------------------------
# set_current_hole
# ---------------------------------------------------------------------------

def test_set_current_hole(user, round_):
    updated = set_current_hole(user, round_.id, 5)
    assert updated.current_hole == 5


def test_set_current_hole_invalid_raises(user, round_):
    with pytest.raises(ValidationError):
        set_current_hole(user, round_.id, 99)


def test_set_current_hole_on_completed_round_raises(user, round_):
    complete_round(user, round_.id)
    with pytest.raises(ConflictError):
        set_current_hole(user, round_.id, 1)


# ---------------------------------------------------------------------------
# complete_round
# ---------------------------------------------------------------------------

def test_complete_round_sets_totals(user, round_):
    add_shot(user, round_.id, 1, "Driver")  # 1 stroke on par-4 hole
    completed = complete_round(user, round_.id)
    assert completed.status == Round.Status.COMPLETED
    assert completed.total_strokes == 1
    assert completed.total_par == 72
    assert completed.relative_to_par == 1 - 72
    assert completed.finished_at is not None


def test_complete_round_note(user, round_):
    completed = complete_round(user, round_.id, note="Great round")
    assert completed.note == "Great round"


def test_complete_round_twice_raises(user, round_):
    complete_round(user, round_.id)
    with pytest.raises(ConflictError):
        complete_round(user, round_.id)


def test_complete_round_wrong_user_raises(user2, round_):
    with pytest.raises(NotFoundError):
        complete_round(user2, round_.id)


# ---------------------------------------------------------------------------
# cancel_round
# ---------------------------------------------------------------------------

def test_cancel_round_removes_round_and_holes(user, round_):
    add_shot(user, round_.id, 1, "Driver")
    result = cancel_round(user, round_.id)
    assert result["cancelled"] is True
    assert not Round.objects.filter(pk=round_.id).exists()
    assert not Shot.objects.filter(round_hole__round__pk=round_.id).exists()


def test_cancel_completed_round_raises(user, round_):
    complete_round(user, round_.id)
    with pytest.raises(ConflictError):
        cancel_round(user, round_.id)


def test_cancel_nonexistent_round_raises(user):
    with pytest.raises(NotFoundError):
        cancel_round(user, 99999)


def test_cancel_wrong_user_raises(user2, round_):
    with pytest.raises(NotFoundError):
        cancel_round(user2, round_.id)


# ---------------------------------------------------------------------------
# courses service
# ---------------------------------------------------------------------------

def test_create_course_service(db):
    holes = [{"hole_number": n, "par": 4} for n in range(1, 10)]
    course = create_course("My Course", 9, holes)
    assert course.holes.count() == 9
    assert course.total_par == 36


def test_get_course_by_id(db):
    holes = [{"hole_number": n, "par": 4} for n in range(1, 10)]
    course = create_course("My Course", 9, holes)
    fetched = get_course_by_id(course.id)
    assert fetched is not None
    assert fetched.name == "My Course"


def test_get_course_by_id_missing_returns_none(db):
    assert get_course_by_id(99999) is None


def test_list_courses_ordered_by_name(db):
    create_course("Zebra Links", 9, [{"hole_number": 1, "par": 4}])
    create_course("Alpha Golf", 9, [{"hole_number": 1, "par": 4}])
    names = [c.name for c in list_courses()]
    assert names == sorted(names)


def test_update_course_changes_par(db):
    holes = [{"hole_number": n, "par": 4} for n in range(1, 10)]
    course = create_course("Nine", 9, holes)
    updated_holes = [{"hole_number": n, "par": 3 if n == 1 else 4} for n in range(1, 10)]
    updated = update_course(course.id, "Nine", 9, updated_holes)
    assert updated.holes.get(hole_number=1).par == 3


def test_update_course_removes_holes(db):
    holes = [{"hole_number": n, "par": 4} for n in range(1, 10)]
    course = create_course("Nine", 9, holes)
    updated_holes = [{"hole_number": n, "par": 4} for n in range(1, 9)]  # drop hole 9
    updated = update_course(course.id, "Nine", 8, updated_holes)
    assert updated.holes.count() == 8
    assert not updated.holes.filter(hole_number=9).exists()


def test_update_course_not_found_raises(db):
    with pytest.raises(NotFoundError):
        update_course(99999, "X", 9, [])
