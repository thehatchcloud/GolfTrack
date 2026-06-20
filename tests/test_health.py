def test_health_endpoint_returns_ok(client):
    response = client.get("/api/health")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_home_page_renders(client):
    response = client.get("/")
    assert response.status_code == 200
    assert b"GolfTrack" in response.content
