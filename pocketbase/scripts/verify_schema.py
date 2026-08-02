#!/usr/bin/env python3
"""Verify a running PocketBase instance against the Phase 1 validation gate.

Checks that the six GolfTrack collections exist with the fields, indexes and
constraints that ``pb_schema.json`` describes, then exercises the constraints
that only show up at write time (partial unique index on in-progress rounds,
composite unique indexes, relation cascades, field range validation).

Usage:
    python3 pocketbase/scripts/verify_schema.py \
        --url http://127.0.0.1:8090 \
        --email dev@golftrack.local --password devdevdevdev

Exits non-zero on the first failed check. Records created during the run are
deleted before exit, so it is safe to re-run against a dev instance.
"""

from __future__ import annotations

import argparse
import json
import sys
import urllib.error
import urllib.request

EXPECTED_COLLECTIONS = {
    "users": {"role", "display_name"},
    "courses": {"name", "hole_count", "time_zone", "is_archived", "created_at", "updated_at"},
    "course_holes": {"course", "hole_number", "par"},
    "rounds": {
        "user",
        "course",
        "status",
        "play_mode",
        "started_at",
        "finished_at",
        "note",
        "current_hole",
        "total_strokes",
        "total_par",
        "relative_to_par",
        "created_at",
        "updated_at",
    },
    "round_holes": {"round", "hole_number", "par", "strokes", "created_at", "updated_at"},
    "shots": {"round_hole", "shot_number", "club", "created_at"},
}

EXPECTED_INDEXES = {
    "course_holes": ["idx_course_holes_course_hole_number"],
    "rounds": ["idx_rounds_one_in_progress_per_user"],
    "round_holes": ["idx_round_holes_round_hole_number"],
    "shots": ["idx_shots_round_hole_shot_number"],
}


class CheckError(Exception):
    pass


class Client:
    def __init__(self, base_url: str) -> None:
        self.base = base_url.rstrip("/")
        self.token = ""

    def request(self, method: str, path: str, payload=None):
        body = json.dumps(payload).encode() if payload is not None else None
        headers = {"Content-Type": "application/json"}
        if self.token:
            headers["Authorization"] = self.token
        req = urllib.request.Request(
            self.base + path, data=body, method=method, headers=headers
        )
        with urllib.request.urlopen(req) as resp:
            raw = resp.read()
            return json.loads(raw) if raw else {}

    def login(self, email: str, password: str) -> None:
        data = self.request(
            "POST",
            "/api/collections/_superusers/auth-with-password",
            {"identity": email, "password": password},
        )
        self.token = data["token"]


passed = 0


def ok(label: str) -> None:
    global passed
    passed += 1
    print(f"  ok   {label}")


def expect_rejected(client: Client, label: str, method: str, path: str, payload=None):
    """Assert that a write is rejected by PocketBase validation/constraints."""
    try:
        client.request(method, path, payload)
    except urllib.error.HTTPError as exc:
        if exc.code in (400, 403):
            ok(f"{label} (rejected with {exc.code})")
            return
        raise CheckError(f"{label}: expected 400/403, got {exc.code}") from None
    raise CheckError(f"{label}: write was accepted but should have been rejected")


def check_collections(client: Client) -> dict:
    print("collections")
    data = client.request("GET", "/api/collections?perPage=200")
    by_name = {c["name"]: c for c in data["items"]}

    for name, expected_fields in EXPECTED_COLLECTIONS.items():
        collection = by_name.get(name)
        if collection is None:
            raise CheckError(f"collection '{name}' is missing")
        actual = {f["name"] for f in collection["fields"]}
        missing = expected_fields - actual
        if missing:
            raise CheckError(f"collection '{name}' is missing fields: {sorted(missing)}")
        ok(f"{name}: {len(expected_fields)} expected fields present")

    for name, index_names in EXPECTED_INDEXES.items():
        joined = " ".join(by_name[name]["indexes"])
        for index_name in index_names:
            if index_name not in joined:
                raise CheckError(f"collection '{name}' is missing index '{index_name}'")
            ok(f"{name}: index {index_name}")

    return by_name


def check_constraints(client: Client) -> None:
    print("constraints")
    created: list[tuple[str, str]] = []

    def create(collection: str, payload: dict) -> dict:
        record = client.request(
            "POST", f"/api/collections/{collection}/records", payload
        )
        created.append((collection, record["id"]))
        return record

    try:
        user = create(
            "users",
            {
                "email": "schema-check@golftrack.local",
                "password": "verify-schema-pw",
                "passwordConfirm": "verify-schema-pw",
                "role": "USER",
                "display_name": "Schema Check",
            },
        )
        course = create("courses", {"name": "Verify GC", "hole_count": 9, "is_archived": False})
        hole = create("course_holes", {"course": course["id"], "hole_number": 1, "par": 4})
        ok("created user, course and course_hole")

        expect_rejected(
            client,
            "course_holes unique (course, hole_number)",
            "POST",
            "/api/collections/course_holes/records",
            {"course": course["id"], "hole_number": 1, "par": 5},
        )
        expect_rejected(
            client,
            "course_holes par out of range (par=7)",
            "POST",
            "/api/collections/course_holes/records",
            {"course": course["id"], "hole_number": 2, "par": 7},
        )
        expect_rejected(
            client,
            "courses hole_count out of range (hole_count=27)",
            "POST",
            "/api/collections/courses/records",
            {"name": "Too Big GC", "hole_count": 27},
        )

        round_ = create(
            "rounds",
            {
                "user": user["id"],
                "course": course["id"],
                "status": "in_progress",
                "play_mode": "full",
                "started_at": "2026-01-01 10:00:00.000Z",
                "current_hole": 1,
            },
        )
        expect_rejected(
            client,
            "rounds one in-progress round per user",
            "POST",
            "/api/collections/rounds/records",
            {
                "user": user["id"],
                "course": course["id"],
                "status": "in_progress",
                "play_mode": "full",
                "started_at": "2026-01-02 10:00:00.000Z",
                "current_hole": 1,
            },
        )
        expect_rejected(
            client,
            "rounds status rejects values outside the enum",
            "POST",
            "/api/collections/rounds/records",
            {
                "user": user["id"],
                "course": course["id"],
                "status": "abandoned",
                "play_mode": "full",
                "started_at": "2026-01-02 10:00:00.000Z",
                "current_hole": 1,
            },
        )

        # A completed round for the same user must be allowed alongside the
        # in-progress one — that is what makes the unique index *partial*.
        completed_round = create(
            "rounds",
            {
                "user": user["id"],
                "course": course["id"],
                "status": "completed",
                "play_mode": "full",
                "started_at": "2025-12-01 10:00:00.000Z",
                "finished_at": "2025-12-01 13:00:00.000Z",
                "current_hole": 9,
                "total_strokes": 40,
                "total_par": 36,
                "relative_to_par": 4,
            },
        )
        ok("completed round coexists with an in-progress round (partial index)")

        round_hole = create(
            "round_holes",
            {"round": round_["id"], "hole_number": 1, "par": 4, "strokes": 0},
        )
        ok("round_hole accepts strokes=0")

        expect_rejected(
            client,
            "round_holes unique (round, hole_number)",
            "POST",
            "/api/collections/round_holes/records",
            {"round": round_["id"], "hole_number": 1, "par": 5, "strokes": 0},
        )

        create("shots", {"round_hole": round_hole["id"], "shot_number": 1, "club": "7 Iron"})
        expect_rejected(
            client,
            "shots unique (round_hole, shot_number)",
            "POST",
            "/api/collections/shots/records",
            {"round_hole": round_hole["id"], "shot_number": 1, "club": "Putter"},
        )
        expect_rejected(
            client,
            "shots club max length 32",
            "POST",
            "/api/collections/shots/records",
            {"round_hole": round_hole["id"], "shot_number": 2, "club": "x" * 33},
        )

        # Cascade: deleting the round removes its holes and their shots.
        client.request("DELETE", f"/api/collections/rounds/records/{round_['id']}")
        remaining_holes = client.request(
            "GET",
            f"/api/collections/round_holes/records?filter=(round='{round_['id']}')",
        )
        if remaining_holes["totalItems"] != 0:
            raise CheckError("deleting a round did not cascade to round_holes")
        remaining_shots = client.request(
            "GET",
            f"/api/collections/shots/records?filter=(round_hole='{round_hole['id']}')",
        )
        if remaining_shots["totalItems"] != 0:
            raise CheckError("deleting a round did not cascade to shots")
        created[:] = [
            (c, i) for c, i in created
            if i not in {round_["id"], round_hole["id"]}
        ]
        ok("deleting a round cascades to round_holes and shots")

        # rounds.course uses cascadeDelete=false, which makes PocketBase refuse
        # to delete a course that still has rounds — the equivalent of Django's
        # on_delete=PROTECT.
        expect_rejected(
            client,
            "courses cannot be deleted while rounds reference them",
            "DELETE",
            f"/api/collections/courses/records/{course['id']}",
        )
        client.request(
            "DELETE", f"/api/collections/rounds/records/{completed_round['id']}"
        )
        created[:] = [(c, i) for c, i in created if i != completed_round["id"]]

        # course_holes cascade from courses, verified via the hole created above.
        client.request("DELETE", f"/api/collections/courses/records/{course['id']}")
        remaining_course_holes = client.request(
            "GET",
            f"/api/collections/course_holes/records?filter=(course='{course['id']}')",
        )
        if remaining_course_holes["totalItems"] != 0:
            raise CheckError("deleting a course did not cascade to course_holes")
        created[:] = [
            (c, i) for c, i in created if i not in {course["id"], hole["id"]}
        ]
        ok("deleting a course cascades to course_holes")
    finally:
        for collection, record_id in reversed(created):
            try:
                client.request(
                    "DELETE", f"/api/collections/{collection}/records/{record_id}"
                )
            except urllib.error.HTTPError:
                pass


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", default="http://127.0.0.1:8090")
    parser.add_argument("--email", default="dev@golftrack.local")
    parser.add_argument("--password", default="devdevdevdev")
    args = parser.parse_args()

    client = Client(args.url)
    try:
        health = client.request("GET", "/api/health")
    except (urllib.error.URLError, urllib.error.HTTPError) as exc:
        print(f"cannot reach PocketBase at {args.url}: {exc}", file=sys.stderr)
        return 2
    print(f"PocketBase reachable at {args.url} ({health.get('message', '')})")

    try:
        client.login(args.email, args.password)
    except urllib.error.HTTPError:
        print(
            "superuser login failed — create one with:\n"
            f"  ./pocketbase superuser upsert {args.email} {args.password}",
            file=sys.stderr,
        )
        return 2

    try:
        check_collections(client)
        check_constraints(client)
    except CheckError as exc:
        print(f"\nFAILED: {exc}", file=sys.stderr)
        return 1

    print(f"\nAll {passed} checks passed.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
