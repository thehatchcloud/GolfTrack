def calculate_round_totals(holes):
    """holes: iterable of objects with .par and .strokes attributes."""
    total_par = sum(h.par for h in holes)
    total_strokes = sum(h.strokes for h in holes)
    return {
        "total_par": total_par,
        "total_strokes": total_strokes,
        "relative_to_par": total_strokes - total_par,
    }


def format_relative_to_par(value: int) -> str:
    if value == 0:
        return "E"
    return f"+{value}" if value > 0 else str(value)
