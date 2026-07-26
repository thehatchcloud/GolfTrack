#!/usr/bin/env python3
"""Import ``pocketbase/pb_schema.json`` into a running PocketBase instance.

``pb_schema.json`` is the checked-in source of truth for the GolfTrack
collections. This script pushes it through PocketBase's collection import
endpoint, which upserts by collection id — so re-running it is safe and will
reconcile a dev instance back to the committed schema.

Normally this is unnecessary: the golftrack-pb binary embeds pb_schema.json
and performs the same import at startup. Use this script to reconcile a
running instance mid-session (after editing pb_schema.json) without a
restart, or when serving with GOLFTRACK_SCHEMA_SYNC=0.

Usage:
    python3 pocketbase/scripts/apply_schema.py \
        --url http://127.0.0.1:8090 \
        --email dev@golftrack.local --password devdevdevdev

``--delete-missing`` additionally removes non-system collections that are not
in the file. It destroys data, so it is off by default.
"""

from __future__ import annotations

import argparse
import json
import pathlib
import sys
import urllib.error
import urllib.request

SCHEMA_PATH = pathlib.Path(__file__).resolve().parent.parent / "pb_schema.json"


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
    parser.add_argument(
        "--delete-missing",
        action="store_true",
        help="delete collections absent from pb_schema.json (destroys data)",
    )
    args = parser.parse_args()

    collections = json.loads(SCHEMA_PATH.read_text())

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
        print(
            "superuser login failed — create one with:\n"
            f"  ./pocketbase superuser upsert {args.email} {args.password}",
            file=sys.stderr,
        )
        return 2

    try:
        request(
            args.url,
            "PUT",
            "/api/collections/import",
            {"collections": collections, "deleteMissing": args.delete_missing},
            token=auth["token"],
        )
    except urllib.error.HTTPError as exc:
        print(f"import failed ({exc.code}):", file=sys.stderr)
        print(exc.read().decode(), file=sys.stderr)
        return 1

    names = ", ".join(c["name"] for c in collections)
    print(f"Imported {len(collections)} collections: {names}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
