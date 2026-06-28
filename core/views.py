from django.shortcuts import render


def home(request):
    ctx = {}
    if request.user.is_authenticated:
        from rounds.services import get_in_progress_round, list_completed_rounds
        ctx["in_progress"] = get_in_progress_round(request.user)
        ctx["recent"] = list_completed_rounds(request.user)[:3]
    return render(request, "home.html", ctx)
