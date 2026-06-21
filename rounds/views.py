from django.http import Http404
from django.shortcuts import render

from core.decorators import login_required_view
from courses.services import list_courses
from rounds.clubs import DEFAULT_CLUBS
from rounds.schemas import RoundDetailOut
from rounds.services import (
    get_in_progress_round,
    get_round_by_id,
    list_completed_rounds,
)


@login_required_view
def round_list(request):
    in_progress = get_in_progress_round(request.user)
    completed = list_completed_rounds(request.user)
    return render(
        request,
        "rounds/list.html",
        {"in_progress": in_progress, "completed": completed},
    )


PLAY_MODES = [("full", "Full Course"), ("front9", "Front 9"), ("back9", "Back 9")]


@login_required_view
def round_new(request):
    in_progress = get_in_progress_round(request.user)
    courses = list_courses()
    return render(
        request,
        "rounds/new.html",
        {"in_progress": in_progress, "courses": courses, "play_modes": PLAY_MODES},
    )


@login_required_view
def round_detail(request, pk):
    round_ = get_round_by_id(request.user, pk)
    if round_ is None:
        raise Http404
    return render(request, "rounds/detail.html", {"round": round_})


@login_required_view
def round_play(request, pk):
    round_ = get_round_by_id(request.user, pk)
    if round_ is None:
        raise Http404
    round_data = RoundDetailOut.from_orm(round_).model_dump(by_alias=True, mode="json")
    return render(
        request,
        "rounds/play.html",
        {
            "round": round_,
            "round_data": round_data,
            "clubs": DEFAULT_CLUBS,
        },
    )


@login_required_view
def round_review(request, pk):
    round_ = get_round_by_id(request.user, pk)
    if round_ is None:
        raise Http404
    return render(request, "rounds/review.html", {"round": round_})
