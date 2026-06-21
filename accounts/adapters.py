from allauth.account.adapter import DefaultAccountAdapter
from allauth.socialaccount.adapter import DefaultSocialAccountAdapter


class AccountAdapter(DefaultAccountAdapter):
    def is_open_for_signup(self, request):
        # Local email/password signup is disabled; use OAuth providers.
        return False


class SocialAccountAdapter(DefaultSocialAccountAdapter):
    def is_open_for_signup(self, request, sociallogin):
        return True

    def populate_user(self, request, sociallogin, data):
        user = super().populate_user(request, sociallogin, data)
        # allauth may leave username blank when ACCOUNT_USERNAME_REQUIRED=False;
        # use email as a stable, unique username so AbstractUser's unique
        # constraint is satisfied.
        if not user.username and user.email:
            user.username = user.email[:150]
        return user
