from django.http import Http404
from django.shortcuts import redirect, render

from core.decorators import admin_required
from core.exceptions import ValidationError
from courses.services import create_course, get_course_by_id, list_courses, update_course

MAX_HOLES = 18
DEFAULT_PAR = 4
PAR_OPTIONS = [3, 4, 5, 6]


def _build_hole_rows(pars_by_number):
    return [
        {"hole_number": n, "par": pars_by_number.get(n, DEFAULT_PAR)}
        for n in range(1, MAX_HOLES + 1)
    ]


def _parse_course_post(post):
    name = post.get("name", "").strip()
    hole_count = int(post.get("hole_count", 18))
    holes = []
    pars_by_number = {}
    for n in range(1, hole_count + 1):
        par = int(post.get(f"hole_{n}", DEFAULT_PAR))
        holes.append({"hole_number": n, "par": par})
        pars_by_number[n] = par
    return name, hole_count, holes, pars_by_number


def course_list(request):
    courses = list_courses()
    return render(request, "courses/list.html", {"courses": courses})


def course_detail(request, pk):
    course = get_course_by_id(pk)
    if course is None:
        raise Http404
    return render(request, "courses/detail.html", {"course": course})


@admin_required
def course_new(request):
    if request.method == "POST":
        name, hole_count, holes, pars_by_number = _parse_course_post(request.POST)
        try:
            course = create_course(name, hole_count, holes)
            return redirect("course_detail", pk=course.id)
        except (ValidationError, ValueError) as exc:
            return render(
                request,
                "courses/form.html",
                {
                    "error": str(exc),
                    "name": name,
                    "hole_count": hole_count,
                    "hole_rows": _build_hole_rows(pars_by_number),
                    "par_options": PAR_OPTIONS,
                    "editing": False,
                },
            )
    return render(
        request,
        "courses/form.html",
        {
            "hole_count": 18,
            "hole_rows": _build_hole_rows({}),
            "par_options": PAR_OPTIONS,
            "editing": False,
        },
    )


@admin_required
def course_edit(request, pk):
    course = get_course_by_id(pk)
    if course is None:
        raise Http404
    if request.method == "POST":
        name, hole_count, holes, pars_by_number = _parse_course_post(request.POST)
        try:
            update_course(pk, name, hole_count, holes)
            return redirect("course_detail", pk=pk)
        except (ValidationError, ValueError) as exc:
            return render(
                request,
                "courses/form.html",
                {
                    "error": str(exc),
                    "course": course,
                    "name": name,
                    "hole_count": hole_count,
                    "hole_rows": _build_hole_rows(pars_by_number),
                    "par_options": PAR_OPTIONS,
                    "editing": True,
                },
            )
    existing_pars = {h.hole_number: h.par for h in course.holes.all()}
    return render(
        request,
        "courses/form.html",
        {
            "course": course,
            "hole_count": course.hole_count,
            "hole_rows": _build_hole_rows(existing_pars),
            "par_options": PAR_OPTIONS,
            "editing": True,
        },
    )
