from django.urls import path

from courses import views

urlpatterns = [
    path("", views.course_list, name="course_list"),
    path("new/", views.course_new, name="course_new"),
    path("<int:pk>/", views.course_detail, name="course_detail"),
    path("<int:pk>/edit/", views.course_edit, name="course_edit"),
]
