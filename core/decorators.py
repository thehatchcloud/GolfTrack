from functools import wraps

from django.http import HttpResponseForbidden
from django.shortcuts import redirect


def login_required_view(view_func):
    @wraps(view_func)
    def wrapper(request, *args, **kwargs):
        if not request.user.is_authenticated:
            return redirect(f"/accounts/login/?next={request.path}")
        return view_func(request, *args, **kwargs)
    return wrapper


def admin_required(view_func):
    @wraps(view_func)
    def wrapper(request, *args, **kwargs):
        if not request.user.is_authenticated:
            return redirect(f"/accounts/login/?next={request.path}")
        if not request.user.is_admin:
            return HttpResponseForbidden("Forbidden")
        return view_func(request, *args, **kwargs)
    return wrapper
