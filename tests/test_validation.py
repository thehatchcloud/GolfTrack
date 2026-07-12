import pytest
from pydantic import ValidationError

from courses.schemas import CourseIn
from rounds.schemas import CompleteRoundIn, SetCurrentHoleIn, ShotIn, StartRoundIn

SAMPLE_HOLES_9 = [{"holeNumber": n, "par": 4} for n in range(1, 10)]


def _course(**overrides):
    data = {"name": "Test Course", "holeCount": 9, "holes": SAMPLE_HOLES_9}
    data.update(overrides)
    return data


# --- CourseIn / HoleIn (mirrors tests/course-validation.test.ts) ------------

def test_course_in_accepts_a_valid_9_hole_course():
    course = CourseIn(**_course())
    assert course.name == "Test Course"
    assert course.hole_count == 9
    assert len(course.holes) == 9


def test_course_in_rejects_empty_name():
    with pytest.raises(ValidationError, match="Course name is required"):
        CourseIn(**_course(name="   "))


def test_course_in_rejects_name_over_100_chars():
    with pytest.raises(ValidationError, match="Course name must be 100 characters or fewer"):
        CourseIn(**_course(name="a" * 101))


def test_course_in_accepts_name_of_exactly_100_chars():
    course = CourseIn(**_course(name="a" * 100))
    assert len(course.name) == 100


def test_course_in_rejects_hole_count_mismatch():
    with pytest.raises(ValidationError, match="Expected 9 holes"):
        CourseIn(**_course(holes=SAMPLE_HOLES_9[:8]))


def test_course_in_rejects_duplicate_hole_numbers():
    holes = [{"holeNumber": 1, "par": 4}, {"holeNumber": 1, "par": 3}, *SAMPLE_HOLES_9[2:]]
    with pytest.raises(ValidationError, match="Hole numbers must be unique"):
        CourseIn(**_course(holes=holes))


def test_course_in_rejects_non_sequential_hole_numbers():
    holes = [h if h["holeNumber"] != 9 else {**h, "holeNumber": 10} for h in SAMPLE_HOLES_9]
    with pytest.raises(ValidationError, match="Hole numbers must be sequential starting at 1"):
        CourseIn(**_course(holes=holes))


def test_course_in_rejects_par_below_3():
    holes = [h if h["holeNumber"] != 2 else {**h, "par": 2} for h in SAMPLE_HOLES_9]
    with pytest.raises(ValidationError, match="Par must be between 3 and 6"):
        CourseIn(**_course(holes=holes))


def test_course_in_accepts_par_6():
    holes = [h if h["holeNumber"] != 2 else {**h, "par": 6} for h in SAMPLE_HOLES_9]
    course = CourseIn(**_course(holes=holes))
    assert next(h.par for h in course.holes if h.hole_number == 2) == 6


def test_course_in_rejects_par_7():
    holes = [h if h["holeNumber"] != 2 else {**h, "par": 7} for h in SAMPLE_HOLES_9]
    with pytest.raises(ValidationError, match="Par must be between 3 and 6"):
        CourseIn(**_course(holes=holes))


def test_course_in_rejects_more_than_18_holes():
    holes = [{"holeNumber": n, "par": 4} for n in range(1, 20)]
    with pytest.raises(ValidationError):
        CourseIn(name="Test Course", holeCount=18, holes=holes)


def test_course_in_rejects_hole_number_greater_than_18():
    holes = [h if h["holeNumber"] != 9 else {**h, "holeNumber": 19} for h in SAMPLE_HOLES_9]
    with pytest.raises(ValidationError):
        CourseIn(**_course(holes=holes))


def test_course_in_rejects_invalid_hole_count():
    with pytest.raises(ValidationError):
        CourseIn(**_course(holeCount=12, holes=[{"holeNumber": n, "par": 4} for n in range(1, 13)]))


# --- ShotIn / CompleteRoundIn / StartRoundIn / SetCurrentHoleIn ------------

def test_shot_in_rejects_blank_club():
    with pytest.raises(ValidationError, match="Club is required"):
        ShotIn(club="   ")


def test_shot_in_rejects_club_over_32_chars():
    with pytest.raises(ValidationError, match="Club name is too long"):
        ShotIn(club="x" * 33)


def test_shot_in_accepts_club_of_exactly_32_chars():
    shot = ShotIn(club="x" * 32)
    assert len(shot.club) == 32


def test_complete_round_in_accepts_missing_note():
    payload = CompleteRoundIn()
    assert payload.note is None


def test_complete_round_in_rejects_note_over_1000_chars():
    with pytest.raises(ValidationError, match="Note is too long"):
        CompleteRoundIn(note="a" * 1001)


def test_complete_round_in_accepts_note_of_exactly_1000_chars():
    payload = CompleteRoundIn(note="a" * 1000)
    assert len(payload.note) == 1000


def test_start_round_in_defaults_to_full_play_mode():
    payload = StartRoundIn(courseId=1)
    assert payload.play_mode == "full"


def test_start_round_in_rejects_invalid_play_mode():
    with pytest.raises(ValidationError):
        StartRoundIn(courseId=1, playMode="eighteen")


def test_set_current_hole_in_rejects_non_positive_hole():
    with pytest.raises(ValidationError):
        SetCurrentHoleIn(currentHole=0)
