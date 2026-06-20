from ninja import Schema
from pydantic.alias_generators import to_camel


class CamelSchema(Schema):
    """Base schema that serializes to camelCase to match the existing API contract.

    Field names stay snake_case (so they resolve from Django model attributes),
    while the JSON aliases are camelCase. Operations return these with
    ``by_alias=True``; ``populate_by_name`` lets inputs accept either form.
    """

    model_config = {"alias_generator": to_camel, "populate_by_name": True}


class IdOut(Schema):
    id: int


class CancelOut(Schema):
    id: int
    cancelled: bool


class ErrorOut(Schema):
    error: str
