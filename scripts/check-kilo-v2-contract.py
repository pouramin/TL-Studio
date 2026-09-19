#!/usr/bin/env python3
"""Runtime contract smoke test for the pinned Kilo Protocol v2 surface.

Usage:
    python3 scripts/check-kilo-v2-contract.py http://127.0.0.1:12345

The base URL is the TL-Studio launcher URL, not Kilo's password-protected backend.
"""

from __future__ import annotations

import json
import sys
import urllib.parse
import urllib.request


class ContractError(RuntimeError):
    pass


def request(base: str, path: str, method: str = "GET", payload=None):
    data = None
    headers = {"Accept": "application/json"}
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(base.rstrip("/") + path, data=data, headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=20) as res:
        raw = res.read()
        if res.status < 200 or res.status >= 300:
            raise ContractError(f"{method} {path}: HTTP {res.status}")
        if not raw:
            return None
        content_type = res.headers.get("Content-Type", "")
        if "json" not in content_type:
            raise ContractError(f"{method} {path}: expected JSON, got {content_type!r}")
        return json.loads(raw)


def first_sse_event(base: str, path: str):
    req = urllib.request.Request(
        base.rstrip("/") + path,
        headers={"Accept": "text/event-stream", "Cache-Control": "no-cache"},
        method="GET",
    )
    with urllib.request.urlopen(req, timeout=10) as res:
        content_type = res.headers.get("Content-Type", "")
        require("text/event-stream" in content_type, f"{path}: expected text/event-stream, got {content_type!r}")
        data_lines = []
        for _ in range(100):
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
        raise ContractError(f"{path}: no SSE data event received")


def require(condition: bool, message: str):
    if not condition:
        raise ContractError(message)


def require_location_envelope(value, name: str):
    require(isinstance(value, dict), f"{name}: response must be an object")
    require(isinstance(value.get("data"), list), f"{name}: data must be an array")
    location = value.get("location")
    require(isinstance(location, dict), f"{name}: location must be an object")
    require(isinstance(location.get("directory"), str), f"{name}: location.directory must be a string")
    project = location.get("project")
    require(isinstance(project, dict), f"{name}: location.project must be an object")
    require(isinstance(project.get("id"), str), f"{name}: location.project.id must be a string")
    require(isinstance(project.get("directory"), str), f"{name}: location.project.directory must be a string")
    return value["data"]


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: check-kilo-v2-contract.py <launcher-base-url>", file=sys.stderr)
        return 2
    base = sys.argv[1].rstrip("/")

    local = request(base, "/local/status")
    require(isinstance(local, dict), "local/status must return an object")
    require(isinstance(local.get("project"), str), "local/status.project must be a string")

    health = request(base, "/runtime/api/health")
    require(health == {"healthy": True}, f"/api/health mismatch: {health!r}")

    location = request(base, "/runtime/api/location")
    require(isinstance(location, dict), "/api/location must return an object")
    require(isinstance(location.get("directory"), str), "/api/location.directory must be a string")
    require(isinstance(location.get("project"), dict), "/api/location.project must be an object")

    event = first_sse_event(base, "/runtime/api/event")
    require(isinstance(event, dict), "/api/event: SSE data must decode to an object")
    require(event.get("type") == "server.connected", f"/api/event: expected server.connected first, got {event!r}")
    require(isinstance(event.get("data"), dict), "/api/event: event.data must be an object")

    agents = require_location_envelope(request(base, "/runtime/api/agent"), "/api/agent")
    for agent in agents:
        require(isinstance(agent.get("id"), str), "Agent.Info.id must be a string")
        require(agent.get("mode") in {"subagent", "primary", "all"}, "Agent.Info.mode is invalid")
        require(isinstance(agent.get("hidden"), bool), "Agent.Info.hidden must be boolean")
        require(isinstance(agent.get("permissions"), list), "Agent.Info.permissions must be an array")
        require(isinstance(agent.get("request"), dict), "Agent.Info.request must be an object")

    models = require_location_envelope(request(base, "/runtime/api/model"), "/api/model")
    for model in models:
        require(isinstance(model.get("id"), str), "Model.Info.id must be a string")
        require(isinstance(model.get("providerID"), str), "Model.Info.providerID must be a string")
        require(isinstance(model.get("name"), str), "Model.Info.name must be a string")
        require(isinstance(model.get("enabled"), bool), "Model.Info.enabled must be boolean")
        require(model.get("status") in {"alpha", "beta", "deprecated", "active"}, "Model.Info.status is invalid")
        require(isinstance(model.get("capabilities"), dict), "Model.Info.capabilities must be an object")

    providers = require_location_envelope(request(base, "/runtime/api/provider"), "/api/provider")
    for provider in providers:
        require(isinstance(provider.get("id"), str), "Provider.Info.id must be a string")
        require(isinstance(provider.get("name"), str), "Provider.Info.name must be a string")
        require(isinstance(provider.get("api"), dict), "Provider.Info.api must be an object")
        require(isinstance(provider.get("request"), dict), "Provider.Info.request must be an object")

    # Official provider HttpApi used by Kilo's own clients. TL-Studio uses only
    # connection/default state from this route; model enumeration stays on v2.
    provider_runtime = request(base, "/runtime/provider")
    if isinstance(provider_runtime, dict) and isinstance(provider_runtime.get("data"), dict):
        provider_runtime = provider_runtime["data"]
    require(isinstance(provider_runtime, dict), "/provider must return an object")
    require(isinstance(provider_runtime.get("connected"), list), "/provider.connected must be an array")
    require(isinstance(provider_runtime.get("default"), dict), "/provider.default must be an object")

    created = request(base, "/runtime/api/session", method="POST", payload={})
    require(isinstance(created, dict) and isinstance(created.get("data"), dict), "session.create must return {data}")
    session = created["data"]
    session_id = session.get("id")
    require(isinstance(session_id, str) and session_id, "Session.Info.id must be a non-empty string")
    for key in ("projectID", "title", "location", "time", "tokens"):
        require(key in session, f"Session.Info.{key} missing")

    got = request(base, f"/runtime/api/session/{urllib.parse.quote(session_id, safe='')}")
    require(got.get("data", {}).get("id") == session_id, "session.get returned the wrong session")

    messages = request(base, f"/runtime/api/session/{urllib.parse.quote(session_id, safe='')}/message?order=asc&limit=10")
    require(isinstance(messages, dict), "session.messages must return an object")
    require(isinstance(messages.get("data"), list), "session.messages.data must be an array")
    require(isinstance(messages.get("cursor"), dict), "session.messages.cursor must be an object")

    permissions = request(base, f"/runtime/api/session/{urllib.parse.quote(session_id, safe='')}/permission")
    require(isinstance(permissions, dict) and isinstance(permissions.get("data"), list), "permission list shape mismatch")

    questions = request(base, f"/runtime/api/session/{urllib.parse.quote(session_id, safe='')}/question")
    require(isinstance(questions, dict) and isinstance(questions.get("data"), list), "question list shape mismatch")

    active = request(base, "/runtime/api/session/active")
    require(isinstance(active, dict) and isinstance(active.get("data"), dict), "session.active shape mismatch")

    print(
        json.dumps(
            {
                "ok": True,
                "project": local["project"],
                "agents": len(agents),
                "models": len(models),
                "providers": len(providers),
                "session": session_id,
                "sse": event.get("type"),
            },
            indent=2,
        )
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except ContractError as error:
        print(f"CONTRACT FAILURE: {error}", file=sys.stderr)
        raise SystemExit(1)
