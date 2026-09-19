#!/usr/bin/env python3
"""Verify that TL-Studio routes current and legacy read APIs to the selected project."""

from __future__ import annotations

import json
import os
import sys
import urllib.parse
import urllib.request


def get_json(base: str, path: str):
    req = urllib.request.Request(base.rstrip("/") + path, headers={"Accept": "application/json"})
    with urllib.request.urlopen(req, timeout=20) as response:
        return json.loads(response.read())


def canonical(path: str) -> str:
    return os.path.normcase(os.path.realpath(path))


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: check-kilo-project-routing.py <launcher-base-url>", file=sys.stderr)
        return 2

    base = sys.argv[1]
    local = get_json(base, "/local/status")
    selected = local.get("project")
    if not isinstance(selected, str):
        print(f"invalid local project response: {selected!r}", file=sys.stderr)
        return 1

    quoted = urllib.parse.quote(selected, safe="")
    routed = get_json(base, f"/runtime/path?directory={quoted}")
    if isinstance(routed, dict) and isinstance(routed.get("data"), dict):
        routed = routed["data"]
    directory = routed.get("directory") if isinstance(routed, dict) else None
    if not isinstance(directory, str):
        print(f"invalid /path response: {routed!r}", file=sys.stderr)
        return 1
    if canonical(selected) != canonical(directory):
        print(f"project routing mismatch: selected={selected!r} routed={directory!r}", file=sys.stderr)
        return 1

    # TL Studio briefly used Protocol v2 during alpha development. The current
    # product path never writes through that API, but recovery of those alpha
    # sessions requires its read endpoints to remain available in the pinned
    # local runtime and scoped to the selected project by the proxy boundary.
    legacy_location = get_json(base, "/runtime/api/location")
    legacy_directory = legacy_location.get("directory") if isinstance(legacy_location, dict) else None
    if not isinstance(legacy_directory, str):
        print(f"invalid legacy /api/location response: {legacy_location!r}", file=sys.stderr)
        return 1
    if canonical(selected) != canonical(legacy_directory):
        print(
            f"legacy project routing mismatch: selected={selected!r} routed={legacy_directory!r}",
            file=sys.stderr,
        )
        return 1

    legacy_sessions = get_json(base, "/runtime/api/session?order=desc&limit=1")
    legacy_data = legacy_sessions.get("data") if isinstance(legacy_sessions, dict) else None
    if not isinstance(legacy_data, list):
        print(f"invalid legacy /api/session response: {legacy_sessions!r}", file=sys.stderr)
        return 1

    print(json.dumps({
        "ok": True,
        "selected": selected,
        "routed": directory,
        "legacy_routed": legacy_directory,
        "legacy_read": True,
    }, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
