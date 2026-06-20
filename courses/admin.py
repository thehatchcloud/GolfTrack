from django.contrib import admin

from courses.models import Course, CourseHole


class CourseHoleInline(admin.TabularInline):
    model = CourseHole
    extra = 0
    ordering = ["hole_number"]


@admin.register(Course)
class CourseAdmin(admin.ModelAdmin):
    list_display = ("name", "hole_count", "total_par", "created_at")
    search_fields = ("name",)
    inlines = [CourseHoleInline]
