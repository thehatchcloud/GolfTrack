from datetime import datetime
from typing import Literal

from pydantic import Field, field_validator

from core.schemas import CamelSchema
from courses.schemas import CourseOut

# --- inputs ---------------------------------------------------------------

class StartRoundIn(CamelSchema):
    course_id: int
    play_mode: Literal["full", "front9", "back9"] = "full"


class SetCurrentHoleIn(CamelSchema):
    current_hole: int = Field(gt=0)


class ShotIn(CamelSchema):
    club: str

    @field_validator("club")
    @classmethod
    def clean_club(cls, value: str) -> str:
        value = value.strip()
        if not value:
            raise ValueError("Club is required")
        if len(value) > 32:
            raise ValueError("Club name is too long")
        return value


class CompleteRoundIn(CamelSchema):
    note: str | None = None

    @field_validator("note")
    @classmethod
    def clean_note(cls, value: str | None) -> str | None:
        if value is None:
            return value
        value = value.strip()
        if len(value) > 1000:
            raise ValueError("Note is too long")
        return value


# --- outputs --------------------------------------------------------------

class ShotOut(CamelSchema):
    id: int
    shot_number: int
    club: str


class RoundHoleOut(CamelSchema):
    id: int
    hole_number: int
    par: int
    strokes: int
    shots: list[ShotOut] = []


class RoundOut(CamelSchema):
    id: int
    status: str
    play_mode: str
    started_at: datetime
    finished_at: datetime | None
    note: str
    current_hole: int
    total_strokes: int | None
    total_par: int | None
    relative_to_par: int | None
    course: CourseOut


class RoundDetailOut(RoundOut):
    holes: list[RoundHoleOut] = []


class CurrentHoleOut(CamelSchema):
    id: int
    current_hole: int
