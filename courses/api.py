from ninja import Router, Status

from core.api_auth import require_admin
from core.exceptions import NotFoundError
from core.schemas import IdOut
from courses.schemas import CourseIn, CourseOut
from courses.services import create_course, get_course_by_id, list_courses, update_course

router = Router()


def _to_holes(payload: CourseIn) -> list[dict]:
    return [{"hole_number": h.hole_number, "par": h.par} for h in payload.holes]


@router.get("/", response=list[CourseOut], by_alias=True)
def list_all(request):
    return list_courses()


@router.post("/", response={201: IdOut}, by_alias=True)
def create(request, payload: CourseIn):
    require_admin(request)
    course = create_course(payload.name, payload.hole_count, _to_holes(payload))
    return Status(201, {"id": course.id})


@router.get("/{int:course_id}", response=CourseOut, by_alias=True)
def detail(request, course_id: int):
    course = get_course_by_id(course_id)
    if course is None:
        raise NotFoundError("Course not found")
    return course


@router.put("/{int:course_id}", response=IdOut, by_alias=True)
def update(request, course_id: int, payload: CourseIn):
    require_admin(request)
    course = update_course(course_id, payload.name, payload.hole_count, _to_holes(payload))
    return {"id": course.id}
