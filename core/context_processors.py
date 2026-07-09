from django.conf import settings


def auth_options(request):
    return {"allow_password_login": settings.ALLOW_PASSWORD_LOGIN}
