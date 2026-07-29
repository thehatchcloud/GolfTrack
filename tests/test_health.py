import re


def test_health_endpoint_returns_ok(client):
    response = client.get("/api/health")
    assert response.status_code == 200
    body = response.json()
    assert body["status"] == "ok"
    assert re.fullmatch(r"\d{4}\.\d{2}\.\d{2}\+\S+", body["version"])


def test_home_page_renders(client):
    response = client.get("/")
    assert response.status_code == 200
    assert b"GolfTrack" in response.content
