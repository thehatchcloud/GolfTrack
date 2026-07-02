import pytest
from django.contrib.auth import get_user_model
from django.test import Client

from courses.models import Course, CourseHole

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
    assert b'x-data="courseForm(' in res.content


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
    res = admin_client.get(f"/courses/{course.id}/edit/")
    assert res.status_code == 200
    assert b'value="Test Valley"' in res.content
    assert b"Hole pars" in res.content
