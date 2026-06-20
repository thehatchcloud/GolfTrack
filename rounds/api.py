from ninja import Router, Status

from core.api_auth import require_user
from core.exceptions import NotFoundError
from core.schemas import CancelOut, IdOut
from rounds.schemas import (
    CompleteRoundIn,
    CurrentHoleOut,
    RoundDetailOut,
    RoundHoleOut,
    RoundOut,
    SetCurrentHoleIn,
    ShotIn,
    ShotOut,
    StartRoundIn,
)
from rounds.services import (
    add_shot,
    cancel_round,
    complete_round,
    create_round,
    delete_shot,
    get_in_progress_round,
    get_round_by_id,
    list_completed_rounds,
    set_current_hole,
    undo_last_shot,
    update_shot,
)

router = Router()


@router.get("/", response=list[RoundOut], by_alias=True)
def list_completed(request):
    user = require_user(request)
    return list_completed_rounds(user)


@router.post("/", response={201: IdOut}, by_alias=True)
def create(request, payload: StartRoundIn):
    user = require_user(request)
    round_ = create_round(user, payload.course_id, payload.play_mode)
    return Status(201, {"id": round_.id})


@router.get("/in-progress")
def in_progress(request):
    user = require_user(request)
    round_ = get_in_progress_round(user)
    if round_ is None:
        return None
    return RoundDetailOut.from_orm(round_).model_dump(by_alias=True, mode="json")


@router.get("/{int:round_id}", response=RoundDetailOut, by_alias=True)
def detail(request, round_id: int):
    user = require_user(request)
    round_ = get_round_by_id(user, round_id)
    if round_ is None:
        raise NotFoundError("Round not found")
    return round_


@router.post("/{int:round_id}/complete", response=IdOut, by_alias=True)
def complete(request, round_id: int, payload: CompleteRoundIn | None = None):
    user = require_user(request)
    note = payload.note if payload else ""
    round_ = complete_round(user, round_id, note or "")
    return {"id": round_.id}


@router.post("/{int:round_id}/cancel", response=CancelOut, by_alias=True)
def cancel(request, round_id: int):
    user = require_user(request)
    return cancel_round(user, round_id)


@router.patch("/{int:round_id}/current-hole", response=CurrentHoleOut, by_alias=True)
def patch_current_hole(request, round_id: int, payload: SetCurrentHoleIn):
    user = require_user(request)
    round_ = set_current_hole(user, round_id, payload.current_hole)
    return {"id": round_.id, "current_hole": round_.current_hole}


@router.post("/{int:round_id}/holes/{int:hole_number}/shots", response=RoundHoleOut, by_alias=True)
def add_shot_endpoint(request, round_id: int, hole_number: int, payload: ShotIn):
    user = require_user(request)
    return add_shot(user, round_id, hole_number, payload.club)


@router.post("/{int:round_id}/holes/{int:hole_number}/undo", response=RoundHoleOut, by_alias=True)
def undo_endpoint(request, round_id: int, hole_number: int):
    user = require_user(request)
    return undo_last_shot(user, round_id, hole_number)


@router.patch(
    "/{int:round_id}/holes/{int:hole_number}/shots/{int:shot_id}",
    response=ShotOut,
    by_alias=True,
)
def update_shot_endpoint(request, round_id: int, hole_number: int, shot_id: int, payload: ShotIn):
    user = require_user(request)
    return update_shot(user, round_id, hole_number, shot_id, payload.club)


@router.delete(
    "/{int:round_id}/holes/{int:hole_number}/shots/{int:shot_id}",
    response=RoundHoleOut,
    by_alias=True,
)
def delete_shot_endpoint(request, round_id: int, hole_number: int, shot_id: int):
    user = require_user(request)
    return delete_shot(user, round_id, hole_number, shot_id)
