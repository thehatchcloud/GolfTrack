from datetime import datetime
from typing import Literal

from pydantic import Field, field_validator, model_validator

from core.schemas import CamelSchema


class HoleIn(CamelSchema):
    hole_number: int = Field(ge=1, le=18)
    par: int

    @field_validator("par")
    @classmethod
    def par_in_range(cls, value: int) -> int:
        if value not in (3, 4, 5, 6):
            raise ValueError("Par must be between 3 and 6")
        return value


class CourseIn(CamelSchema):
    name: str
    hole_count: Literal[9, 18]
    holes: list[HoleIn] = Field(max_length=18)

    @field_validator("name")
    @classmethod
    def clean_name(cls, value: str) -> str:
        value = value.strip()
        if not value:
            raise ValueError("Course name is required")
        if len(value) > 100:
            raise ValueError("Course name must be 100 characters or fewer")
        return value

    @model_validator(mode="after")
    def validate_holes(self):
        if len(self.holes) != self.hole_count:
            raise ValueError(f"Expected {self.hole_count} holes")

        numbers = [h.hole_number for h in self.holes]
        if len(set(numbers)) != len(numbers):
            raise ValueError("Hole numbers must be unique")

        for index, hole in enumerate(sorted(self.holes, key=lambda h: h.hole_number)):
            if hole.hole_number != index + 1:
                raise ValueError("Hole numbers must be sequential starting at 1")

        return self


class CourseHoleOut(CamelSchema):
    id: int
    hole_number: int
    par: int


class CourseOut(CamelSchema):
    id: int
    name: str
    hole_count: int
    total_par: int
    is_archived: bool
    holes: list[CourseHoleOut]
    created_at: datetime
    updated_at: datetime
