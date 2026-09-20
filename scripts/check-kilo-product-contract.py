#!/usr/bin/env python3
"""Runtime contract check for the Kilo v7.6.2 product HttpApi used by TL Studio."""

from __future__ import annotations

import json
import shutil
import sys
import tempfile
import urllib.parse
import urllib.request


class ContractError(RuntimeError):
    pass


def require(condition: bool, message: str):
    if not condition:
        raise ContractError(message)


def unwrap(value):
    if isinstance(value, dict) and "data" in value:
        return value["data"]
    return value


def request(base: str, path: str, method: str = "GET", payload=None, timeout=20):
    data = None
    headers = {"Accept": "application/json"}
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(base.rstrip("/") + path, data=data, headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=timeout) as res:
        raw = res.read()
        if not raw:
            return None
        ctype = res.headers.get("Content-Type", "")
        require("json" in ctype, f"{method} {path}: expected JSON, got {ctype!r}")
        return json.loads(raw)


def directory_query(project: str, extra=None):
    query = {"directory": project}
    if extra:
        query.update(extra)
    return urllib.parse.urlencode(query)


def first_global_event(base: str, project: str):
    path = "/runtime/global/event?" + directory_query(project)
    req = urllib.request.Request(base.rstrip("/") + path, headers={"Accept": "text/event-stream"})
    with urllib.request.urlopen(req, timeout=10) as res:
        require("text/event-stream" in (res.headers.get("Content-Type") or ""), "global/event is not SSE")
        data_lines = []
        for _ in range(150):
            raw = res.readline()
            if not raw:
                break
            line = raw.decode("utf-8", errors="replace").rstrip("\r\n")
            if not line:
                if data_lines:
                    return json.loads("\n".join(data_lines))
                continue
            if line.startswith("data:"):
                data_lines.append(line[5:].lstrip())
    raise ContractError("global/event did not yield an SSE event")


def session_ids(value):
    return {item.get("id") for item in value if isinstance(item, dict) and isinstance(item.get("id"), str)}


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: check-kilo-product-contract.py <launcher-base-url>", file=sys.stderr)
        return 2
    base = sys.argv[1].rstrip("/")

    local = request(base, "/local/status")
    require(isinstance(local, dict) and isinstance(local.get("project"), str), "local/status.project missing")
    project = local["project"]
    query = directory_query(project)

    history = request(base, "/local/projects")
    require(isinstance(history, dict) and isinstance(history.get("projects"), list), "/local/projects shape mismatch")
    require(project in history["projects"], "current project was not remembered")

    health = unwrap(request(base, "/runtime/global/health"))
    require(isinstance(health, dict) and health.get("healthy") is True, f"global health mismatch: {health!r}")

    path = unwrap(request(base, f"/runtime/path?{query}"))
    require(isinstance(path, dict) and isinstance(path.get("directory"), str), f"path mismatch: {path!r}")

    agents = unwrap(request(base, f"/runtime/agent?{query}"))
    require(isinstance(agents, list) and agents, "agent list must be non-empty")
    visible = [a for a in agents if isinstance(a, dict) and not a.get("hidden") and a.get("mode") != "subagent"]
    names = [str(a.get("name") or a.get("id") or "") for a in visible]
    require("code" in names, f"official Kilo code agent missing; visible agents={names!r}")
    require("build" not in names, f"unpatched build agent leaked through product API: {names!r}")

    providers = unwrap(request(base, f"/runtime/provider?{query}"))
    require(isinstance(providers, dict), "/provider must return an object")
    require(isinstance(providers.get("all"), list), "/provider.all must be an array")
    require(isinstance(providers.get("connected"), list), "/provider.connected must be an array")
    require(isinstance(providers.get("default"), dict), "/provider.default must be an object")

    kilo_auth_before = unwrap(request(base, f"/runtime/kilo/auth-status?{query}"))
    require(isinstance(kilo_auth_before, dict), "/runtime/auth-status must return an object")
    require(isinstance(kilo_auth_before.get("authenticated"), bool), "/runtime/auth-status.authenticated must be boolean")

    auth_removed = unwrap(request(base, "/runtime/auth/kilo", method="DELETE"))
    require(auth_removed is True, f"auth.remove mismatch: {auth_removed!r}")

    disposed = unwrap(request(base, "/runtime/global/dispose", method="POST"))
    require(disposed is True, f"global.dispose mismatch: {disposed!r}")

    kilo_auth_after = unwrap(request(base, f"/runtime/kilo/auth-status?{query}"))
    require(isinstance(kilo_auth_after, dict), "post-dispose /kilo/auth-status must return an object")
    require(kilo_auth_after.get("authenticated") is False,
            f"Kilo auth should be signed out after auth.remove + global.dispose: {kilo_auth_after!r}")

    created = unwrap(request(base, f"/runtime/session?{query}", method="POST", payload={}))
    require(isinstance(created, dict) and isinstance(created.get("id"), str), f"session.create mismatch: {created!r}")
    sid = created["id"]
    sidq = urllib.parse.quote(sid, safe="")

    sessions = unwrap(request(base, f"/runtime/session?{directory_query(project, {'limit': 50, 'roots': 'true'})}"))
    require(isinstance(sessions, list), "session.list must be an array")
    require(sid in session_ids(sessions), "created session missing from project list")

    # Kilo serve scopes its useful root-session listing to a directory. TL Studio
    # persists only recent project paths, queries each directory explicitly, and
    # merges the authoritative Kilo session records in the UI.
    alt_project = tempfile.mkdtemp(prefix="tl-studio-contract-project-")
    alt_sid = None
    try:
        switched = request(base, "/local/project", method="POST", payload={"path": alt_project})
        require(isinstance(switched, dict) and switched.get("project") == alt_project,
                f"local project switch mismatch: {switched!r}")

        history = request(base, "/local/projects")
        require(isinstance(history, dict) and isinstance(history.get("projects"), list), "project history missing after switch")
        require(project in history["projects"] and alt_project in history["projects"],
                f"recent project history did not keep both projects: {history!r}")

        alt_query = directory_query(alt_project)
        created_alt = unwrap(request(base, f"/runtime/session?{alt_query}", method="POST", payload={"title": "TL Studio cross-project"}))
        require(isinstance(created_alt, dict) and isinstance(created_alt.get("id"), str),
                f"second project session.create mismatch: {created_alt!r}")
        alt_sid = created_alt["id"]

        # Return to the original project, then prove explicit-directory queries
        # can still retrieve both histories while the launcher's active project is
        # the original one. This mirrors TL Studio's sidebar aggregation.
        request(base, "/local/project", method="POST", payload={"path": project})
        original_sessions = unwrap(request(base, f"/runtime/session?{directory_query(project, {'limit': 50, 'roots': 'true'})}"))
        alt_sessions = unwrap(request(base, f"/runtime/session?{directory_query(alt_project, {'limit': 50, 'roots': 'true'})}"))
        require(isinstance(original_sessions, list) and isinstance(alt_sessions, list), "per-project session list must be arrays")
        require(sid in session_ids(original_sessions), "original project session disappeared")
        require(alt_sid in session_ids(alt_sessions), "alternate project session could not be read by explicit directory")
        merged_ids = session_ids(original_sessions) | session_ids(alt_sessions)
        require(sid in merged_ids and alt_sid in merged_ids, "merged project histories did not contain both sessions")

        alt_record = next((s for s in alt_sessions if isinstance(s, dict) and s.get("id") == alt_sid), None)
        require(isinstance(alt_record, dict) and alt_record.get("directory") == alt_project,
                f"session must expose its own directory: {alt_record!r}")
    finally:
        request(base, "/local/project", method="POST", payload={"path": project})

    renamed = unwrap(request(base, f"/runtime/session/{sidq}?{query}", method="PATCH", payload={"title": "TL Studio contract"}))
    require(isinstance(renamed, dict) and renamed.get("title") == "TL Studio contract", f"session.update mismatch: {renamed!r}")

    messages = unwrap(request(base, f"/runtime/session/{sidq}/message?{directory_query(project, {'limit': 10})}"))
    require(isinstance(messages, list), "session messages must be an array")
    for item in messages:
        require(isinstance(item, dict) and isinstance(item.get("info"), dict) and isinstance(item.get("parts"), list),
                f"production message must be {{info, parts}}: {item!r}")

    diffs = unwrap(request(base, f"/runtime/session/{sidq}/diff?{query}"))
    require(isinstance(diffs, list), "session.diff must be an array")

    statuses = unwrap(request(base, f"/runtime/session/status?{query}"))
    require(isinstance(statuses, dict), "session/status must be an object")

    permissions = unwrap(request(base, f"/runtime/permission?{query}"))
    require(isinstance(permissions, list), "permission list must be an array")
    questions = unwrap(request(base, f"/runtime/question?{query}"))
    require(isinstance(questions, list), "question list must be an array")

    event = first_global_event(base, project)
    require(isinstance(event, dict), "global event must be an object")
    event_payload = event.get("payload", event)
    require(isinstance(event_payload, dict) and isinstance(event_payload.get("type"), str), f"global event payload mismatch: {event!r}")

    removed = unwrap(request(base, f"/runtime/session/{sidq}?{query}", method="DELETE"))
    require(removed is True, f"session.delete mismatch: {removed!r}")
    sessions_after = unwrap(request(base, f"/runtime/session?{directory_query(project, {'limit': 50, 'roots': 'true'})}"))
    require(not any(isinstance(s, dict) and s.get("id") == sid for s in sessions_after), "deleted session still present")

    if alt_sid:
        alt_query = directory_query(alt_project)
        alt_sidq = urllib.parse.quote(alt_sid, safe="")
        removed_alt = unwrap(request(base, f"/runtime/session/{alt_sidq}?{alt_query}", method="DELETE"))
        require(removed_alt is True, f"second project session.delete mismatch: {removed_alt!r}")
    shutil.rmtree(alt_project, ignore_errors=True)

    print(json.dumps({
        "ok": True,
        "project": project,
        "agents": names,
        "providers": len(providers["all"]),
        "kilo_auth_before": kilo_auth_before.get("authenticated"),
        "auth_remove": True,
        "global_dispose": True,
        "kilo_auth_after": kilo_auth_after.get("authenticated"),
        "session_lifecycle": "create/update/diff/delete",
        "recent_project_session_aggregation": True,
        "event": event_payload.get("type"),
    }, indent=2))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except ContractError as error:
        print(f"PRODUCT CONTRACT FAILURE: {error}", file=sys.stderr)
        raise SystemExit(1)
