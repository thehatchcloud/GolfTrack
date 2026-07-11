from django.db import transaction

from core.exceptions import NotFoundError
from courses.models import Course, CourseHole


def list_courses(include_archived=False):
    qs = Course.objects.prefetch_related("holes").order_by("name")
    if not include_archived:
        qs = qs.filter(is_archived=False)
    return list(qs)


def get_course_by_id(course_id):
    try:
        return Course.objects.prefetch_related("holes").get(pk=course_id)
    except Course.DoesNotExist:
        return None


def create_course(name, hole_count, holes):
    """holes: list of dicts with 'hole_number' and 'par' keys."""
    with transaction.atomic():
        course = Course.objects.create(name=name, hole_count=hole_count)
        CourseHole.objects.bulk_create([
            CourseHole(course=course, hole_number=h["hole_number"], par=h["par"])
            for h in holes
        ])
    return course


def update_course(course_id, name, hole_count, holes):
    """
    holes: list of dicts with 'hole_number' and 'par' keys.
    Holes absent from the incoming list are deleted (mirrors course edit behaviour).
    """
    with transaction.atomic():
        try:
            course = Course.objects.get(pk=course_id)
        except Course.DoesNotExist:
            raise NotFoundError("Course not found") from None

        course.name = name
        course.hole_count = hole_count
        course.save(update_fields=["name", "hole_count", "updated_at"])

        existing = list(CourseHole.objects.filter(course=course))
        existing_by_number = {h.hole_number: h for h in existing}
        incoming_by_number = {h["hole_number"]: h for h in holes}

        to_create = []
        updates_by_par: dict[int, list[int]] = {}

        for hole_data in holes:
            ex = existing_by_number.get(hole_data["hole_number"])
            if ex:
                if ex.par != hole_data["par"]:
                    updates_by_par.setdefault(hole_data["par"], []).append(ex.id)
            else:
                to_create.append(
                    CourseHole(
                        course=course,
                        hole_number=hole_data["hole_number"],
                        par=hole_data["par"],
                    )
                )

        if to_create:
            CourseHole.objects.bulk_create(to_create)

        for par, ids in updates_by_par.items():
            CourseHole.objects.filter(pk__in=ids).update(par=par)

        removed_ids = [h.id for h in existing if h.hole_number not in incoming_by_number]
        if removed_ids:
            CourseHole.objects.filter(pk__in=removed_ids).delete()

    return Course.objects.prefetch_related("holes").get(pk=course_id)


def archive_course(course_id):
    try:
        course = Course.objects.get(pk=course_id)
    except Course.DoesNotExist:
        raise NotFoundError("Course not found") from None
    course.is_archived = True
    course.save(update_fields=["is_archived", "updated_at"])
    return course


def unarchive_course(course_id):
    try:
        course = Course.objects.get(pk=course_id)
    except Course.DoesNotExist:
        raise NotFoundError("Course not found") from None
    course.is_archived = False
    course.save(update_fields=["is_archived", "updated_at"])
    return course
