from django.contrib.auth.models import AbstractUser
from django.db import models


class User(AbstractUser):
    """Application user.

    Extends Django's AbstractUser so we get session auth, admin, and a clean
    integration point for django-allauth (Phase 4, #90). The ``role`` field
    mirrors the USER/ADMIN distinction from the Next.js app; it is assigned at
    login from ``ADMIN_EMAILS`` once allauth is wired up.
    """

    class Role(models.TextChoices):
        USER = "USER", "User"
        ADMIN = "ADMIN", "Admin"

    role = models.CharField(max_length=5, choices=Role.choices, default=Role.USER)

    @property
    def is_admin(self) -> bool:
        return self.role == self.Role.ADMIN
