#!/usr/bin/env python3
"""Export the GolfTrack collections from a running instance to pb_schema.json.

The counterpart to ``apply_schema.py``: edit collections in the Admin UI, then
run this to write the change back into the repo. Output is sorted and indented
so the diff is reviewable; PocketBase's system collections (``_superusers``,
``_mfas``, …) are excluded.

Usage:
    python3 pocketbase/scripts/export_schema.py \
        --url http://127.0.0.1:8090 \
        --email dev@golftrack.local --password devdevdevdev
"""

from __future__ import annotations

import argparse
import json
import pathlib
import sys
import urllib.error
import urllib.request

SCHEMA_PATH = pathlib.Path(__file__).resolve().parent.parent / "pb_schema.json"

# Written in dependency order so the file reads top-down.
ORDER = ["users", "courses", "course_holes", "rounds", "round_holes", "shots"]


def request(base: str, method: str, path: str, payload=None, token: str = ""):
    body = json.dumps(payload).encode() if payload is not None else None
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = token
    req = urllib.request.Request(
        base.rstrip("/") + path, data=body, method=method, headers=headers
    )
    with urllib.request.urlopen(req) as resp:
        raw = resp.read()
        return json.loads(raw) if raw else {}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", default="http://127.0.0.1:8090")
    parser.add_argument("--email", default="dev@golftrack.local")
    parser.add_argument("--password", default="devdevdevdev")
    args = parser.parse_args()

    try:
        auth = request(
            args.url,
            "POST",
            "/api/collections/_superusers/auth-with-password",
            {"identity": args.email, "password": args.password},
        )
    except urllib.error.URLError as exc:
        print(f"cannot reach PocketBase at {args.url}: {exc}", file=sys.stderr)
        return 2
    except urllib.error.HTTPError:
        print("superuser login failed", file=sys.stderr)
        return 2

    data = request(
        args.url, "GET", "/api/collections?perPage=200", token=auth["token"]
    )
    by_name = {c["name"]: c for c in data["items"]}

    missing = [name for name in ORDER if name not in by_name]
    if missing:
        print(f"instance is missing collections: {missing}", file=sys.stderr)
        return 1

    collections = []
    for name in ORDER:
        collection = by_name[name]
        # Collection-level created/updated are instance-local bookkeeping and
        # would churn the diff on every export. The import endpoint doesn't
        # need them. (Field-level "created"/"updated" autodate entries live
        # under "fields" and are left alone.)
        collection.pop("created", None)
        collection.pop("updated", None)
        collections.append(collection)

    SCHEMA_PATH.write_text(
        json.dumps(collections, indent=2, sort_keys=True) + "\n"
    )
    print(f"Wrote {len(collections)} collections to {SCHEMA_PATH}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
