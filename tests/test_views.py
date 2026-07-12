import re

import pytest
from django.contrib.auth import get_user_model
from django.test import Client

from courses.models import Course, CourseHole
from rounds.services import add_shot, create_round

User = get_user_model()


@pytest.fixture
def admin(db):
    return User.objects.create_user(username="boss", password="x", role=User.Role.ADMIN)


@pytest.fixture
def admin_client(admin):
    c = Client()
    c.force_login(admin)
    return c


@pytest.fixture
def user(db):
    return User.objects.create_user(username="alice", password="x")


@pytest.fixture
def auth_client(user):
    c = Client()
    c.force_login(user)
    return c


@pytest.fixture
def course(db):
    c = Course.objects.create(name="Test Valley", hole_count=9)
    for n in range(1, 10):
        CourseHole.objects.create(course=c, hole_number=n, par=4)
    return c


# --- GET /courses/new/ ------------------------------------------------------

def test_course_new_form_renders(admin_client):
    res = admin_client.get("/courses/new/")
    assert res.status_code == 200
    assert b"Hole pars" in res.content
    # All 18 hole selects must be real, server-rendered <select> elements —
    # not client-templated — so par entry works even before any JS executes.
    for n in range(1, 19):
        assert f'name="hole_{n}"'.encode() in res.content
    assert b'<option value="4" selected>4</option>' in res.content


def test_course_new_creates_course_with_hole_pars(admin_client):
    data = {"name": "Pebble Beach", "hole_count": "9"}
    for n in range(1, 10):
        data[f"hole_{n}"] = "5"
    res = admin_client.post("/courses/new/", data)
    assert res.status_code == 302

    course = Course.objects.get(name="Pebble Beach")
    assert course.hole_count == 9
    assert [h.par for h in course.holes.all()] == [5] * 9


# --- GET /courses/<id>/edit/ ------------------------------------------------

def test_course_edit_form_renders_existing_values(admin_client, course):
    CourseHole.objects.filter(course=course, hole_number=3).update(par=5)
    res = admin_client.get(f"/courses/{course.id}/edit/")
    assert res.status_code == 200
    assert b'value="Test Valley"' in res.content
    assert b"Hole pars" in res.content
    assert b'name="hole_3"' in res.content
    # hole 3's par (5) must be pre-selected in its <select>, not just present as text.
    content = res.content.decode()
    hole_3_select = content.split('name="hole_3"')[1].split("</select>")[0]
    assert '<option value="5" selected>5</option>' in hole_3_select


# --- POST /courses/<id>/archive/ and /unarchive/ ----------------------------

def test_course_archive_as_admin(admin_client, course):
    res = admin_client.post(f"/courses/{course.id}/archive/")
    assert res.status_code == 302
    course.refresh_from_db()
    assert course.is_archived is True


def test_archived_course_disappears_from_course_list(admin_client, course):
    admin_client.post(f"/courses/{course.id}/archive/")
    res = admin_client.get("/courses/")
    assert course.name.encode() not in res.content


def test_course_unarchive_as_admin(admin_client, course):
    admin_client.post(f"/courses/{course.id}/archive/")
    res = admin_client.post(f"/courses/{course.id}/unarchive/")
    assert res.status_code == 302
    course.refresh_from_db()
    assert course.is_archived is False


def test_course_archived_list_shows_archived_courses(admin_client, course):
    admin_client.post(f"/courses/{course.id}/archive/")
    res = admin_client.get("/courses/archived/")
    assert res.status_code == 200
    assert course.name.encode() in res.content


def test_course_archive_requires_admin(auth_client, course):
    res = auth_client.post(f"/courses/{course.id}/archive/")
    assert res.status_code == 403


def test_course_archive_requires_post(admin_client, course):
    res = admin_client.get(f"/courses/{course.id}/archive/")
    assert res.status_code == 405


def test_course_archive_not_found(admin_client, db):
    res = admin_client.post("/courses/99999/archive/")
    assert res.status_code == 404


# --- GET /rounds/<id>/review/ -----------------------------------------------

def _summary_tile_value(content: str, label: str) -> str:
    """Return the number rendered in the review page's <label> summary tile.

    Anchored to the tile markup (a value <p> immediately after the label <p>)
    so a bare substring elsewhere on the page — e.g. a matching value in the
    hole-by-hole list — can't satisfy the assertion.
    """
    match = re.search(
        rf">{re.escape(label)}</p>\s*<p class=\"text-2xl font-bold\">\s*([^<\s][^<]*?)\s*</p>",
        content,
    )
    assert match is not None, f"{label!r} summary tile not found"
    return match.group(1)


def test_round_review_shows_live_totals_before_submission(auth_client, user, course):
    round_ = create_round(user, course.id)
    add_shot(user, round_.id, 1, "Driver")
    add_shot(user, round_.id, 1, "Putter")

    res = auth_client.get(f"/rounds/{round_.id}/review/")

    assert res.status_code == 200
    content = res.content.decode()
    assert _summary_tile_value(content, "Par") == "36"  # 9 holes at par 4
    assert _summary_tile_value(content, "Strokes") == "2"  # two shots on hole 1
    assert _summary_tile_value(content, "To Par") == "-34"  # 2 - 36
