from django.contrib import admin

from courses.models import Course, CourseHole


class CourseHoleInline(admin.TabularInline):
    model = CourseHole
    extra = 0
    ordering = ["hole_number"]


@admin.register(Course)
class CourseAdmin(admin.ModelAdmin):
    list_display = ("name", "hole_count", "total_par", "is_archived", "created_at")
    list_filter = ("is_archived",)
    search_fields = ("name",)
    inlines = [CourseHoleInline]
