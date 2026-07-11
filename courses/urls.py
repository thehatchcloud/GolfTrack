from django.urls import path

from courses import views

urlpatterns = [
    path("", views.course_list, name="course_list"),
    path("new/", views.course_new, name="course_new"),
    path("archived/", views.course_archived_list, name="course_archived_list"),
    path("<int:pk>/", views.course_detail, name="course_detail"),
    path("<int:pk>/edit/", views.course_edit, name="course_edit"),
    path("<int:pk>/archive/", views.course_archive, name="course_archive"),
    path("<int:pk>/unarchive/", views.course_unarchive, name="course_unarchive"),
]
