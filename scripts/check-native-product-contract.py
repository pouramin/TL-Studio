#!/usr/bin/env python3
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request


def request(base, path, method="GET", payload=None, expected=200):
    data = None
    headers = {}
    launch_key = os.environ.get("TL_STUDIO_LAUNCH_KEY", "")
    if launch_key:
        headers["Authorization"] = "Bearer " + launch_key
        headers["X-TL-Studio-Request"] = "1"
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(base + path, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=10) as response:
            body = response.read()
            status = response.status
    except urllib.error.HTTPError as error:
        body = error.read()
        status = error.code
    if status != expected:
        raise AssertionError(f"{method} {path}: expected {expected}, got {status}: {body[:1000]!r}")
    if not body:
        return None
    try:
        return json.loads(body.decode("utf-8"))
    except Exception:
        return body.decode("utf-8", errors="replace")


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def main():
    if len(sys.argv) != 3:
        print("usage: check-native-product-contract.py <base-url> <project>", file=sys.stderr)
        return 2
    base = sys.argv[1].rstrip("/")
    project = sys.argv[2]
    query = urllib.parse.urlencode({"directory": project})

    health = request(base, "/local/health")
    require(health == {"healthy": True, "mode": "native"}, f"native health mismatch: {health!r}")

    status = request(base, "/local/status")
    runtime = status.get("runtime") if isinstance(status, dict) else None
    require(isinstance(runtime, dict) and runtime.get("mode") == "native", f"local status mismatch: {status!r}")
    require("compatibilityAvailable" not in json.dumps(status), f"compatibility state leaked into status: {status!r}")

    path = request(base, "/local/path")
    require(path.get("directory") == project, f"local path mismatch: {path!r}")

    agents = request(base, "/local/agents")
    require(isinstance(agents, list) and any(a.get("id") == "code" for a in agents if isinstance(a, dict)),
            f"native agent catalog mismatch: {agents!r}")

    request(base, "/runtime/global/health", expected=404)

    accounts = request(base, f"/local/provider-accounts?{query}")
    require(isinstance(accounts, list), f"provider account list mismatch: {accounts!r}")
    require(not any(isinstance(item, dict) and item.get("id") == "kilo" for item in accounts),
            f"unavailable Kilo account adapter leaked into product: {accounts!r}")

    catalog = request(base, f"/local/providers/catalog?{query}")
    require(isinstance(catalog, dict) and isinstance(catalog.get("all"), list),
            f"provider catalog mismatch: {catalog!r}")
    require("hosted" not in catalog, f"legacy hosted provider metadata leaked into catalog: {catalog!r}")

    permissions = request(base, "/local/permissions")
    require(permissions == [], f"native permissions mismatch: {permissions!r}")
    questions = request(base, "/local/questions")
    require(questions == [], f"native questions mismatch: {questions!r}")

    session = request(base, f"/local/sessions?{query}", method="POST", payload={"title": "Native CI"}, expected=201)
    session_id = session.get("id")
    require(isinstance(session_id, str) and session_id.startswith("tls_"), f"native session id mismatch: {session!r}")

    renamed = request(base, f"/local/sessions/{urllib.parse.quote(session_id)}?{query}",
                      method="PATCH", payload={"title": "Native CI renamed"})
    require(renamed.get("title") == "Native CI renamed", f"native rename mismatch: {renamed!r}")

    sessions = request(base, "/local/sessions?limit=20")
    require(any(s.get("id") == session_id for s in sessions if isinstance(s, dict)),
            f"native session missing from list: {sessions!r}")

    result = request(base, f"/local/sessions/{urllib.parse.quote(session_id)}?{query}", method="DELETE")
    require(result.get("deleted") is True, f"native delete mismatch: {result!r}")

    print("TL Studio native product contract: PASS")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
