from django.urls import path

from rounds import views

urlpatterns = [
    path("", views.round_list, name="round_list"),
    path("new/", views.round_new, name="round_new"),
    path("<int:pk>/", views.round_detail, name="round_detail"),
    path("<int:pk>/play/", views.round_play, name="round_play"),
    path("<int:pk>/review/", views.round_review, name="round_review"),
]
