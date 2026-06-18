from django.contrib import admin
from django.urls import path

from config.api import api
from core import views as core_views

urlpatterns = [
    path("admin/", admin.site.urls),
    path("api/", api.urls),
    path("", core_views.home, name="home"),
]
