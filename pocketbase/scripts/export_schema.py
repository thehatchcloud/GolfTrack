#!/usr/bin/env python3
"""Export the GolfTrack collections from a running instance to pb_schema.json.

The counterpart to ``apply_schema.py``: edit collections in the Admin UI, then
run this to write the change back into the repo. Output is sorted and indented
so the diff is reviewable; PocketBase's system collections (``_superusers``,
``_mfas``, …) are excluded.

The ``users`` authentication settings listed in ``ENV_MANAGED`` are *not*
exported. Phase 4 (#125) applies those from the environment at every startup —
see ``pocketbase/AUTH.md`` — so a running instance carries the deployment's
OAuth client ids and its password-login state, none of which belong in a
committed file. They are written back from the existing pb_schema.json instead,
which also keeps the export byte-for-byte stable.

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

# (collection, top-level key) pairs whose value comes from the environment at
# runtime rather than from this file. Client secrets are redacted by PocketBase
# on the way out anyway, so exporting these would commit a provider list that no
# longer imports.
ENV_MANAGED = [("users", "oauth2"), ("users", "passwordAuth")]


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

    committed = {c["name"]: c for c in json.loads(SCHEMA_PATH.read_text())}

    collections = []
    for name in ORDER:
        collection = by_name[name]
        # Collection-level created/updated are instance-local bookkeeping and
        # would churn the diff on every export. The import endpoint doesn't
        # need them. (Field-level "created"/"updated" autodate entries live
        # under "fields" and are left alone.)
        collection.pop("created", None)
        collection.pop("updated", None)

        # Keep the environment-managed authentication settings as committed.
        for env_name, key in ENV_MANAGED:
            if env_name == name and key in committed.get(name, {}):
                collection[key] = committed[name][key]

        collections.append(collection)

    SCHEMA_PATH.write_text(
        json.dumps(collections, indent=2, sort_keys=True) + "\n"
    )
    print(f"Wrote {len(collections)} collections to {SCHEMA_PATH}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
