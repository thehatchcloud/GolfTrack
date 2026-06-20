from django.conf import settings
from django.db import models
from django.utils import timezone

from courses.models import Course


class Round(models.Model):
    class Status(models.TextChoices):
        IN_PROGRESS = "in_progress", "In progress"
        COMPLETED = "completed", "Completed"

    class PlayMode(models.TextChoices):
        FULL = "full", "Full"
        FRONT9 = "front9", "Front 9"
        BACK9 = "back9", "Back 9"

    user = models.ForeignKey(
        settings.AUTH_USER_MODEL,
        related_name="rounds",
        on_delete=models.CASCADE,
    )
    # Restrict deleting a course that has rounds (mirrors Prisma onDelete: Restrict).
    course = models.ForeignKey(Course, related_name="rounds", on_delete=models.PROTECT)
    status = models.CharField(max_length=11, choices=Status.choices)
    play_mode = models.CharField(
        max_length=6, choices=PlayMode.choices, default=PlayMode.FULL
    )
    started_at = models.DateTimeField(default=timezone.now)
    finished_at = models.DateTimeField(null=True, blank=True)
    note = models.TextField(blank=True, default="")
    current_hole = models.PositiveSmallIntegerField(default=1)
    # Cached totals, populated on completion (see scoring, Phase 2).
    total_strokes = models.IntegerField(null=True, blank=True)
    total_par = models.IntegerField(null=True, blank=True)
    relative_to_par = models.IntegerField(null=True, blank=True)
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)

    class Meta:
        ordering = ["-started_at"]
        indexes = [
            models.Index(fields=["user"]),
            models.Index(fields=["status"]),
            models.Index(fields=["course"]),
        ]
        constraints = [
            # One in-progress round per user. The Next.js service enforced this
            # per user in createRound; expressing it as a partial unique index
            # makes the database the source of truth too. Django emits a partial
            # index on SQLite.
            models.UniqueConstraint(
                fields=["user"],
                condition=models.Q(status="in_progress"),
                name="uq_user_one_in_progress_round",
            ),
        ]

    def __str__(self) -> str:
        return f"Round {self.pk} ({self.status})"


class RoundHole(models.Model):
    round = models.ForeignKey(Round, related_name="holes", on_delete=models.CASCADE)
    hole_number = models.PositiveSmallIntegerField()
    # Par is snapshotted from CourseHole at round creation so later course edits
    # never change past rounds.
    par = models.PositiveSmallIntegerField()
    # Cached count, kept equal to the number of shots in the same transaction.
    strokes = models.PositiveSmallIntegerField(default=0)
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)

    class Meta:
        ordering = ["hole_number"]
        constraints = [
            models.UniqueConstraint(
                fields=["round", "hole_number"],
                name="uq_round_hole_number",
            ),
        ]

    def __str__(self) -> str:
        return f"Round {self.round_id} hole {self.hole_number}"


class Shot(models.Model):
    round_hole = models.ForeignKey(
        RoundHole, related_name="shots", on_delete=models.CASCADE
    )
    shot_number = models.PositiveSmallIntegerField()
    club = models.CharField(max_length=32)
    created_at = models.DateTimeField(auto_now_add=True)

    class Meta:
        ordering = ["shot_number"]
        constraints = [
            models.UniqueConstraint(
                fields=["round_hole", "shot_number"],
                name="uq_round_hole_shot_number",
            ),
        ]

    def __str__(self) -> str:
        return f"Shot {self.shot_number} ({self.club})"
