"""Django Ninja API instance.

Routers for courses, rounds, and shots are added in later phases (#88–#89).
The API is mounted at /api in config/urls.py so the REST paths match the
existing Next.js contract.
"""
from ninja import NinjaAPI

api = NinjaAPI(title="GolfTrack API", version="0.1.0")


@api.get("/health")
def health(request):
    return {"status": "ok"}
