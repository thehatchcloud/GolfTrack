import json

import pytest
from django.contrib.auth import get_user_model
from django.test import Client

from courses.models import Course, CourseHole
from rounds.models import Round, Shot
from rounds.services import add_shot, complete_round, create_round

User = get_user_model()

SAMPLE_COURSE = {
    "name": "Test Valley",
    "holeCount": 9,
    "holes": [
        {"holeNumber": 1, "par": 4},
        {"holeNumber": 2, "par": 3},
        {"holeNumber": 3, "par": 5},
        {"holeNumber": 4, "par": 4},
        {"holeNumber": 5, "par": 4},
        {"holeNumber": 6, "par": 3},
        {"holeNumber": 7, "par": 5},
        {"holeNumber": 8, "par": 4},
        {"holeNumber": 9, "par": 4},
    ],
}


@pytest.fixture
def user(db):
    return User.objects.create_user(username="alice", password="x")


@pytest.fixture
def admin(db):
    return User.objects.create_user(username="boss", password="x", role=User.Role.ADMIN)


@pytest.fixture
def client():
    return Client()


@pytest.fixture
def auth_client(user):
    c = Client()
    c.force_login(user)
    return c


@pytest.fixture
def admin_client(admin):
    c = Client()
    c.force_login(admin)
    return c


@pytest.fixture
def course(db):
    c = Course.objects.create(name="Test Valley", hole_count=18)
    for n in range(1, 19):
        CourseHole.objects.create(course=c, hole_number=n, par=4)
    return c


def post_json(client, url, body):
    return client.post(url, data=json.dumps(body), content_type="application/json")


def put_json(client, url, body):
    return client.put(url, data=json.dumps(body), content_type="application/json")


def patch_json(client, url, body):
    return client.patch(url, data=json.dumps(body), content_type="application/json")


# --- health ---------------------------------------------------------------

def test_health(client):
    res = client.get("/api/health")
    assert res.status_code == 200
    assert res.json() == {"status": "ok"}


# --- GET /api/courses -----------------------------------------------------

def test_list_courses_returns_total_par(client, course):
    res = client.get("/api/courses/")
    assert res.status_code == 200
    body = res.json()
    assert len(body) == 1
    assert body[0]["holeCount"] == 18
    assert body[0]["totalPar"] == 72
    assert len(body[0]["holes"]) == 18


def test_get_course_detail(client, course):
    res = client.get(f"/api/courses/{course.id}")
    assert res.status_code == 200
    assert res.json()["id"] == course.id
    assert res.json()["isArchived"] is False


def test_list_courses_excludes_archived(client, course):
    course.is_archived = True
    course.save(update_fields=["is_archived"])
    res = client.get("/api/courses/")
    assert res.status_code == 200
    assert res.json() == []


def test_get_course_not_found(client, db):
    res = client.get("/api/courses/99999")
    assert res.status_code == 404
    assert res.json()["error"] == "Course not found"


# --- POST /api/courses ----------------------------------------------------

def test_create_course_as_admin(admin_client):
    res = post_json(admin_client, "/api/courses/", SAMPLE_COURSE)
    assert res.status_code == 201
    assert isinstance(res.json()["id"], int)


def test_create_course_unauthenticated(client, db):
    res = post_json(client, "/api/courses/", SAMPLE_COURSE)
    assert res.status_code == 401
    assert res.json()["error"] == "Unauthorized"


def test_create_course_non_admin(auth_client):
    res = post_json(auth_client, "/api/courses/", SAMPLE_COURSE)
    assert res.status_code == 403
    assert res.json()["error"] == "Forbidden"


def test_create_course_invalid_data(admin_client):
    res = post_json(admin_client, "/api/courses/", {"name": "", "holeCount": 9, "holes": []})
    assert res.status_code == 400
    assert res.json()["error"]


def test_create_course_wrong_hole_count(admin_client):
    bad = {**SAMPLE_COURSE, "holeCount": 18}  # only 9 holes supplied
    res = post_json(admin_client, "/api/courses/", bad)
    assert res.status_code == 400
    assert "Expected 18 holes" in res.json()["error"]


# --- PUT /api/courses/{id} ------------------------------------------------

def test_update_course_as_admin(admin_client, course):
    body = {
        "name": "Renamed",
        "holeCount": 18,
        "holes": [{"holeNumber": n, "par": 4} for n in range(1, 19)],
    }
    res = put_json(admin_client, f"/api/courses/{course.id}", body)
    assert res.status_code == 200
    assert res.json()["id"] == course.id


def test_update_course_not_found(admin_client, db):
    body = {
        "name": "Renamed",
        "holeCount": 18,
        "holes": [{"holeNumber": n, "par": 4} for n in range(1, 19)],
    }
    res = put_json(admin_client, "/api/courses/99999", body)
    assert res.status_code == 404


def test_update_course_non_admin(auth_client, course):
    body = {
        "name": "Renamed",
        "holeCount": 18,
        "holes": [{"holeNumber": n, "par": 4} for n in range(1, 19)],
    }
    res = put_json(auth_client, f"/api/courses/{course.id}", body)
    assert res.status_code == 403


# --- POST /api/rounds -----------------------------------------------------

def test_create_round(auth_client, course):
    res = post_json(auth_client, "/api/rounds/", {"courseId": course.id})
    assert res.status_code == 201
    assert isinstance(res.json()["id"], int)


def test_create_round_course_not_found(auth_client, db):
    res = post_json(auth_client, "/api/rounds/", {"courseId": 99999})
    assert res.status_code == 404
    assert "Course not found" in res.json()["error"]


def test_create_round_conflict(auth_client, user, course):
    create_round(user, course.id)
    res = post_json(auth_client, "/api/rounds/", {"courseId": course.id})
    assert res.status_code == 409
    assert "already in progress" in res.json()["error"]


def test_create_round_unauthenticated(client, course):
    res = post_json(client, "/api/rounds/", {"courseId": course.id})
    assert res.status_code == 401


def test_create_round_front9_on_9_hole_course_returns_400(auth_client, db):
    nine_hole_course = Course.objects.create(name="Nine Hole", hole_count=9)
    for n in range(1, 10):
        CourseHole.objects.create(course=nine_hole_course, hole_number=n, par=4)
    res = post_json(
        auth_client, "/api/rounds/", {"courseId": nine_hole_course.id, "playMode": "front9"}
    )
    assert res.status_code == 400
    assert "18-hole" in res.json()["error"]


# --- GET /api/rounds & in-progress ---------------------------------------

def test_list_completed_rounds(auth_client, user, course):
    rnd = create_round(user, course.id)
    complete_round(user, rnd.id)
    res = auth_client.get("/api/rounds/")
    assert res.status_code == 200
    body = res.json()
    assert len(body) == 1
    assert body[0]["status"] == "completed"
    assert body[0]["course"]["id"] == course.id


def test_in_progress_returns_round(auth_client, user, course):
    rnd = create_round(user, course.id)
    res = auth_client.get("/api/rounds/in-progress")
    assert res.status_code == 200
    body = res.json()
    assert body["id"] == rnd.id
    assert body["currentHole"] == 1
    assert len(body["holes"]) == 18


def test_in_progress_returns_null_when_none(auth_client, db):
    res = auth_client.get("/api/rounds/in-progress")
    assert res.status_code == 200
    assert res.json() is None


def test_get_round_detail(auth_client, user, course):
    rnd = create_round(user, course.id)
    add_shot(user, rnd.id, 1, "Driver")
    res = auth_client.get(f"/api/rounds/{rnd.id}")
    assert res.status_code == 200
    body = res.json()
    assert body["id"] == rnd.id
    hole_1 = next(h for h in body["holes"] if h["holeNumber"] == 1)
    assert hole_1["shots"][0]["club"] == "Driver"
    assert hole_1["shots"][0]["shotNumber"] == 1


def test_get_round_detail_wrong_user(client, user, course):
    rnd = create_round(user, course.id)
    other = User.objects.create_user(username="bob", password="x")
    c = Client()
    c.force_login(other)
    res = c.get(f"/api/rounds/{rnd.id}")
    assert res.status_code == 404


# --- shots ----------------------------------------------------------------

def test_add_shot_round_not_found(auth_client, db):
    res = post_json(auth_client, "/api/rounds/99999/holes/1/shots", {"club": "Driver"})
    assert res.status_code == 404


def test_add_shot(auth_client, user, course):
    rnd = create_round(user, course.id)
    res = post_json(auth_client, f"/api/rounds/{rnd.id}/holes/1/shots", {"club": "Driver"})
    assert res.status_code == 200
    body = res.json()
    assert body["strokes"] == 1
    assert body["shots"][0]["club"] == "Driver"


def test_add_shot_invalid_club(auth_client, user, course):
    rnd = create_round(user, course.id)
    res = post_json(auth_client, f"/api/rounds/{rnd.id}/holes/1/shots", {"club": ""})
    assert res.status_code == 400


def test_add_shot_club_too_long(auth_client, user, course):
    rnd = create_round(user, course.id)
    res = post_json(auth_client, f"/api/rounds/{rnd.id}/holes/1/shots", {"club": "x" * 33})
    assert res.status_code == 400


def test_update_shot(auth_client, user, course):
    rnd = create_round(user, course.id)
    add_shot(user, rnd.id, 1, "Driver")
    shot = Shot.objects.get(round_hole__round=rnd, shot_number=1)
    res = patch_json(auth_client, f"/api/rounds/{rnd.id}/holes/1/shots/{shot.id}", {"club": "3W"})
    assert res.status_code == 200
    assert res.json()["club"] == "3W"


def test_delete_shot_renumbers(auth_client, user, course):
    rnd = create_round(user, course.id)
    add_shot(user, rnd.id, 1, "Driver")
    add_shot(user, rnd.id, 1, "7i")
    add_shot(user, rnd.id, 1, "PW")
    shot_2 = Shot.objects.get(round_hole__round=rnd, shot_number=2)
    res = auth_client.delete(f"/api/rounds/{rnd.id}/holes/1/shots/{shot_2.id}")
    assert res.status_code == 200
    body = res.json()
    assert body["strokes"] == 2
    assert [s["shotNumber"] for s in body["shots"]] == [1, 2]


def test_undo_shot(auth_client, user, course):
    rnd = create_round(user, course.id)
    add_shot(user, rnd.id, 1, "Driver")
    res = post_json(auth_client, f"/api/rounds/{rnd.id}/holes/1/undo", {})
    assert res.status_code == 200
    assert res.json()["strokes"] == 0


# --- current-hole, complete, cancel --------------------------------------

def test_set_current_hole(auth_client, user, course):
    rnd = create_round(user, course.id)
    res = patch_json(auth_client, f"/api/rounds/{rnd.id}/current-hole", {"currentHole": 5})
    assert res.status_code == 200
    assert res.json() == {"id": rnd.id, "currentHole": 5}


def test_set_current_hole_invalid(auth_client, user, course):
    rnd = create_round(user, course.id)
    res = patch_json(auth_client, f"/api/rounds/{rnd.id}/current-hole", {"currentHole": 99})
    assert res.status_code == 400


def test_set_current_hole_round_not_found(auth_client, db):
    res = patch_json(auth_client, "/api/rounds/99999/current-hole", {"currentHole": 1})
    assert res.status_code == 404


def test_complete_round(auth_client, user, course):
    rnd = create_round(user, course.id)
    res = post_json(auth_client, f"/api/rounds/{rnd.id}/complete", {"note": "Good day"})
    assert res.status_code == 200
    assert res.json()["id"] == rnd.id
    rnd.refresh_from_db()
    assert rnd.status == Round.Status.COMPLETED
    assert rnd.note == "Good day"


def test_complete_round_empty_body(auth_client, user, course):
    rnd = create_round(user, course.id)
    res = post_json(auth_client, f"/api/rounds/{rnd.id}/complete", {})
    assert res.status_code == 200


def test_complete_round_note_too_long(auth_client, user, course):
    rnd = create_round(user, course.id)
    res = post_json(auth_client, f"/api/rounds/{rnd.id}/complete", {"note": "a" * 1001})
    assert res.status_code == 400


def test_complete_round_not_found(auth_client, db):
    res = post_json(auth_client, "/api/rounds/99999/complete", {})
    assert res.status_code == 404


def test_cancel_round(auth_client, user, course):
    rnd = create_round(user, course.id)
    res = post_json(auth_client, f"/api/rounds/{rnd.id}/cancel", {})
    assert res.status_code == 200
    assert res.json() == {"id": rnd.id, "cancelled": True}
    assert not Round.objects.filter(pk=rnd.id).exists()


def test_cancel_round_not_found(auth_client, db):
    res = post_json(auth_client, "/api/rounds/99999/cancel", {})
    assert res.status_code == 404


def test_cancel_completed_round_conflict(auth_client, user, course):
    rnd = create_round(user, course.id)
    complete_round(user, rnd.id)
    res = post_json(auth_client, f"/api/rounds/{rnd.id}/cancel", {})
    assert res.status_code == 409
