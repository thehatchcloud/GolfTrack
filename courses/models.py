from django.db import models


class Course(models.Model):
    name = models.CharField(max_length=100)
    hole_count = models.PositiveSmallIntegerField()
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)

    class Meta:
        ordering = ["name"]

    def __str__(self) -> str:
        return self.name

    @property
    def total_par(self) -> int:
        return sum(hole.par for hole in self.holes.all())


class CourseHole(models.Model):
    course = models.ForeignKey(Course, related_name="holes", on_delete=models.CASCADE)
    hole_number = models.PositiveSmallIntegerField()
    par = models.PositiveSmallIntegerField()

    class Meta:
        ordering = ["hole_number"]
        constraints = [
            models.UniqueConstraint(
                fields=["course", "hole_number"],
                name="uq_course_hole_number",
            ),
        ]

    def __str__(self) -> str:
        return f"{self.course.name} hole {self.hole_number} (par {self.par})"
