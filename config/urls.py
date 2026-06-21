from django.contrib import admin
from django.urls import include, path

from config.api import api
from core import views as core_views

urlpatterns = [
    path("admin/", admin.site.urls),
    path("api/", api.urls),
    path("accounts/", include("allauth.urls")),
    path("", core_views.home, name="home"),
]
