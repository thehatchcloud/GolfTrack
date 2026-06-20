def get_current_hole_position(hole_numbers: list[int], current_hole: int) -> int:
    """Return the 1-based position of current_hole within hole_numbers."""
    try:
        return hole_numbers.index(current_hole) + 1
    except ValueError:
        return 1


def get_round_play_label_from_mode(play_mode: str, course_hole_count: int) -> str:
    if play_mode == "front9":
        return "Front 9"
    if play_mode == "back9":
        return "Back 9"
    return "9 Holes" if course_hole_count == 9 else "Full Course"
