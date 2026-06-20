from django.db import transaction
from django.db.models import F
from django.utils import timezone

from core.exceptions import ConflictError, NotFoundError, ValidationError
from courses.models import Course
from rounds.models import Round, RoundHole, Shot
from rounds.scoring import calculate_round_totals


def get_in_progress_round(user):
    return (
        Round.objects.select_related("course")
        .filter(user=user, status=Round.Status.IN_PROGRESS)
        .order_by("-started_at")
        .first()
    )


def list_completed_rounds(user, limit=20, page=0):
    offset = page * limit
    return list(
        Round.objects.select_related("course")
        .filter(user=user, status=Round.Status.COMPLETED)
        .order_by("-finished_at", "-started_at")[offset : offset + limit]
    )


def get_round_by_id(user, round_id):
    try:
        return Round.objects.select_related("course").get(pk=round_id, user=user)
    except Round.DoesNotExist:
        return None


def create_round(user, course_id, play_mode="full"):
    if play_mode not in Round.PlayMode.values:
        raise ValidationError(f"Invalid play mode: {play_mode}")

    try:
        course = Course.objects.get(pk=course_id)
    except Course.DoesNotExist:
        raise NotFoundError("Course not found") from None

    if course.hole_count != 18 and play_mode != Round.PlayMode.FULL:
        raise ValidationError("Front 9 or back 9 is only available on 18-hole courses")

    all_holes = list(course.holes.order_by("hole_number"))

    if play_mode == Round.PlayMode.FRONT9:
        selected = [h for h in all_holes if 1 <= h.hole_number <= 9]
    elif play_mode == Round.PlayMode.BACK9:
        selected = [h for h in all_holes if 10 <= h.hole_number <= 18]
    else:
        selected = all_holes

    starting_hole = selected[0].hole_number if selected else 1

    with transaction.atomic():
        if Round.objects.filter(user=user, status=Round.Status.IN_PROGRESS).exists():
            raise ConflictError("A round is already in progress")

        round_ = Round.objects.create(
            user=user,
            course=course,
            status=Round.Status.IN_PROGRESS,
            current_hole=starting_hole,
            play_mode=play_mode,
        )
        RoundHole.objects.bulk_create([
            RoundHole(round=round_, hole_number=h.hole_number, par=h.par, strokes=0)
            for h in selected
        ])

    return round_


def set_current_hole(user, round_id, current_hole):
    with transaction.atomic():
        try:
            round_ = Round.objects.select_for_update().get(pk=round_id, user=user)
        except Round.DoesNotExist:
            raise NotFoundError("Round not found") from None

        if round_.status != Round.Status.IN_PROGRESS:
            raise ConflictError("Round is already completed")

        valid_holes = list(round_.holes.values_list("hole_number", flat=True))
        if current_hole not in valid_holes:
            raise ValidationError("Invalid hole number")

        round_.current_hole = current_hole
        round_.save(update_fields=["current_hole", "updated_at"])

    return round_


def add_shot(user, round_id, hole_number, club):
    with transaction.atomic():
        try:
            round_hole = (
                RoundHole.objects.select_for_update()
                .select_related("round")
                .get(round__pk=round_id, hole_number=hole_number, round__user=user)
            )
        except RoundHole.DoesNotExist:
            raise NotFoundError("Round hole not found") from None

        if round_hole.round.status != Round.Status.IN_PROGRESS:
            raise ConflictError("Round is already completed")

        last_shot = round_hole.shots.order_by("shot_number").last()
        next_shot_number = (last_shot.shot_number if last_shot else 0) + 1

        Shot.objects.create(round_hole=round_hole, shot_number=next_shot_number, club=club)
        round_hole.strokes = next_shot_number
        round_hole.save(update_fields=["strokes", "updated_at"])

    return round_hole


def undo_last_shot(user, round_id, hole_number):
    with transaction.atomic():
        try:
            round_hole = (
                RoundHole.objects.select_for_update()
                .select_related("round")
                .get(round__pk=round_id, hole_number=hole_number, round__user=user)
            )
        except RoundHole.DoesNotExist:
            raise NotFoundError("Round hole not found") from None

        if round_hole.round.status != Round.Status.IN_PROGRESS:
            raise ConflictError("Round is already completed")

        last_shot = round_hole.shots.order_by("shot_number").last()
        if last_shot:
            last_shot.delete()
            round_hole.strokes = last_shot.shot_number - 1
            round_hole.save(update_fields=["strokes", "updated_at"])

    return round_hole


def update_shot(user, round_id, hole_number, shot_id, club):
    with transaction.atomic():
        try:
            round_hole = (
                RoundHole.objects.select_related("round")
                .get(round__pk=round_id, hole_number=hole_number, round__user=user)
            )
        except RoundHole.DoesNotExist:
            raise NotFoundError("Round hole not found") from None

        if round_hole.round.status != Round.Status.IN_PROGRESS:
            raise ConflictError("Round is already completed")

        try:
            shot = Shot.objects.get(pk=shot_id, round_hole=round_hole)
        except Shot.DoesNotExist:
            raise NotFoundError("Shot not found") from None

        shot.club = club
        shot.save(update_fields=["club"])

    return shot


def delete_shot(user, round_id, hole_number, shot_id):
    with transaction.atomic():
        try:
            round_hole = (
                RoundHole.objects.select_for_update()
                .select_related("round")
                .get(round__pk=round_id, hole_number=hole_number, round__user=user)
            )
        except RoundHole.DoesNotExist:
            raise NotFoundError("Round hole not found") from None

        if round_hole.round.status != Round.Status.IN_PROGRESS:
            raise ConflictError("Round is already completed")

        try:
            shot = Shot.objects.get(pk=shot_id, round_hole=round_hole)
        except Shot.DoesNotExist:
            raise NotFoundError("Shot not found") from None

        deleted_number = shot.shot_number
        shot.delete()

        # Keep shot_number gap-free after a mid-hole delete.
        Shot.objects.filter(
            round_hole=round_hole, shot_number__gt=deleted_number
        ).update(shot_number=F("shot_number") - 1)

        round_hole.strokes = round_hole.shots.count()
        round_hole.save(update_fields=["strokes", "updated_at"])

    return round_hole


def complete_round(user, round_id, note=""):
    with transaction.atomic():
        try:
            round_ = Round.objects.select_for_update().get(pk=round_id, user=user)
        except Round.DoesNotExist:
            raise NotFoundError("Round not found") from None

        if round_.status != Round.Status.IN_PROGRESS:
            raise ConflictError("Round is already completed")

        totals = calculate_round_totals(round_.holes.only("par", "strokes"))

        round_.status = Round.Status.COMPLETED
        round_.finished_at = timezone.now()
        round_.note = note or ""
        round_.total_strokes = totals["total_strokes"]
        round_.total_par = totals["total_par"]
        round_.relative_to_par = totals["relative_to_par"]
        round_.save()

    return round_


def cancel_round(user, round_id):
    with transaction.atomic():
        deleted_count, _ = Round.objects.filter(
            pk=round_id, user=user, status=Round.Status.IN_PROGRESS
        ).delete()

        if not deleted_count:
            if Round.objects.filter(pk=round_id, user=user).exists():
                raise ConflictError("Only in-progress rounds can be cancelled")
            raise NotFoundError("Round not found") from None

    return {"id": round_id, "cancelled": True}
