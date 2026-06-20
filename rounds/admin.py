from django.contrib import admin

from rounds.models import Round, RoundHole, Shot


class ShotInline(admin.TabularInline):
    model = Shot
    extra = 0
    ordering = ["shot_number"]
    readonly_fields = ("created_at",)


class RoundHoleInline(admin.TabularInline):
    model = RoundHole
    extra = 0
    ordering = ["hole_number"]
    show_change_link = True


@admin.register(Round)
class RoundAdmin(admin.ModelAdmin):
    list_display = ("pk", "user", "course", "status", "play_mode", "started_at", "total_strokes")
    list_filter = ("status", "play_mode")
    search_fields = ("user__username", "course__name")
    readonly_fields = ("created_at", "updated_at")
    inlines = [RoundHoleInline]


@admin.register(RoundHole)
class RoundHoleAdmin(admin.ModelAdmin):
    list_display = ("pk", "round", "hole_number", "par", "strokes")
    list_filter = ("par",)
    inlines = [ShotInline]


@admin.register(Shot)
class ShotAdmin(admin.ModelAdmin):
    list_display = ("pk", "round_hole", "shot_number", "club", "created_at")
    search_fields = ("club",)
