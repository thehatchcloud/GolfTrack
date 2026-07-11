import pytest
from django.contrib.auth import get_user_model
from django.test import Client, override_settings

User = get_user_model()


@pytest.fixture
def user(db):
    u = User.objects.create_user(username="alice", email="alice@example.com", password="s3cret-pw")
    return u


def test_login_page_shows_oauth_only_by_default(client, db):
    res = client.get("/accounts/login/")
    assert res.status_code == 200
    assert b"Continue with Google" in res.content
    assert b"Continue with Microsoft" in res.content
    assert b'name="password"' not in res.content


@override_settings(
    ALLOW_PASSWORD_LOGIN=True,
    SOCIALACCOUNT_ONLY=False,
    ACCOUNT_SIGNUP_FIELDS=["email*", "password1*"],
)
def test_login_page_shows_password_form_when_enabled(db):
    res = Client().get("/accounts/login/")
    assert res.status_code == 200
    assert b'name="login"' in res.content
    assert b'name="password"' in res.content
    assert b"Continue with Google" not in res.content


@override_settings(
    ALLOW_PASSWORD_LOGIN=True,
    SOCIALACCOUNT_ONLY=False,
    ACCOUNT_SIGNUP_FIELDS=["email*", "password1*"],
)
def test_password_login_succeeds_for_existing_user(user):
    res = Client().post(
        "/accounts/login/",
        {"login": "alice@example.com", "password": "s3cret-pw"},
    )
    assert res.status_code == 302

    res = Client().post(
        "/accounts/login/",
        {"login": "alice@example.com", "password": "wrong-password"},
    )
    assert res.status_code == 200
