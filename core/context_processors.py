from django.conf import settings

from core.version import get_version


def auth_options(request):
    return {"allow_password_login": settings.ALLOW_PASSWORD_LOGIN}


def app_version(request):
    return {"app_version": get_version()}
