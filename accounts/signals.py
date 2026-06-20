from django.conf import settings
from django.contrib.auth.signals import user_logged_in
from django.dispatch import receiver


@receiver(user_logged_in)
def sync_admin_role(sender, request, user, **kwargs):
    """Grant or revoke ADMIN role based on the ADMIN_EMAILS setting.

    Runs on every login so the role stays in sync when ADMIN_EMAILS changes
    without requiring a manual DB edit.
    """
    admin_emails: set[str] = getattr(settings, "ADMIN_EMAILS", set())
    if not admin_emails:
        return

    Role = user.__class__.Role
    desired = Role.ADMIN if user.email.lower() in admin_emails else Role.USER
    if user.role != desired:
        user.role = desired
        user.save(update_fields=["role"])
