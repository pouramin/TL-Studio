#!/usr/bin/env python3
"""Exercise Kilo v7.6.2's production coding path against a local fake LLM."""

from __future__ import annotations

import json
import os
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

EXPECTED = "E2E_PRODUCT_OK"
FILE_CONTENT = "TL_STUDIO_E2E_OK"


class E2EError(RuntimeError):
    pass


def require(condition: bool, message: str):
    if not condition:
        raise E2EError(message)


def unwrap(value):
    if isinstance(value, dict) and "data" in value:
        return value["data"]
    return value


def request(base: str, path: str, method: str = "GET", payload=None, timeout=30):
    data = None
    headers = {"Accept": "application/json"}
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(base.rstrip("/") + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as res:
            raw = res.read()
            if not raw:
                return None
            ctype = res.headers.get("Content-Type", "")
            if "json" not in ctype:
                raise E2EError(f"{method} {path}: expected JSON, got {ctype!r}")
            return json.loads(raw)
    except urllib.error.HTTPError as error:
        detail = error.read().decode("utf-8", errors="replace")
        raise E2EError(f"{method} {path}: HTTP {error.code}: {detail}") from error


def routed(path: str, project: str, **params) -> str:
    query = {"directory": project, **params}
    return f"{path}?{urllib.parse.urlencode(query)}"


def sse_events(base: str, project: str, sink: list[dict], ready: threading.Event, stop: threading.Event):
    try:
        req = urllib.request.Request(
            base.rstrip("/") + routed("/runtime/global/event", project),
            headers={"Accept": "text/event-stream", "Cache-Control": "no-cache"},
        )
        with urllib.request.urlopen(req, timeout=60) as res:
            data_lines: list[str] = []
            while not stop.is_set():
                raw = res.readline()
                if not raw:
                    break
                line = raw.decode("utf-8", errors="replace").rstrip("\r\n")
                if not line:
                    if not data_lines:
                        continue
                    try:
                        envelope = json.loads("\n".join(data_lines))
                        if isinstance(envelope, dict):
                            sink.append(envelope)
                            payload = envelope.get("payload", envelope)
                            if isinstance(payload, dict) and payload.get("type") == "server.connected":
                                ready.set()
                    finally:
                        data_lines = []
                    continue
                if line.startswith("data:"):
                    data_lines.append(line[5:].lstrip())
    except Exception as error:
        sink.append({"type": "test.sse.error", "message": str(error)})
        ready.set()


def assistant_text(envelope: dict) -> str:
    if not isinstance(envelope, dict):
        return ""
    info = envelope.get("info")
    if not isinstance(info, dict) or info.get("role") != "assistant":
        return ""
    parts = envelope.get("parts")
    if not isinstance(parts, list):
        return ""
    return "\n".join(
        part.get("text", "")
        for part in parts
        if isinstance(part, dict) and part.get("type") == "text" and isinstance(part.get("text"), str)
    )


def has_completed_write(envelope: dict) -> bool:
    parts = envelope.get("parts") if isinstance(envelope, dict) else None
    if not isinstance(parts, list):
        return False
    return any(
        isinstance(part, dict)
        and part.get("type") == "tool"
        and part.get("tool") == "write"
        and isinstance(part.get("state"), dict)
        and part["state"].get("status") == "completed"
        for part in parts
    )


def projected_changes(messages: list[dict]) -> list[dict]:
    """Mirror TL Studio's fallback when the bundled runtime's aggregate session diff is empty."""
    summary_diffs: list[dict] = []
    for message in messages:
        info = message.get("info") if isinstance(message, dict) else None
        summary = info.get("summary") if isinstance(info, dict) else None
        diffs = summary.get("diffs") if isinstance(summary, dict) else None
        if isinstance(diffs, list):
            summary_diffs.extend(item for item in diffs if isinstance(item, dict))
    if summary_diffs:
        return summary_diffs

    changes: list[dict] = []
    for message in messages:
        parts = message.get("parts") if isinstance(message, dict) else None
        if not isinstance(parts, list):
            continue
        for part in parts:
            if not isinstance(part, dict) or part.get("type") != "tool":
                continue
            state = part.get("state") if isinstance(part.get("state"), dict) else {}
            metadata = state.get("metadata") if isinstance(state.get("metadata"), dict) else {}
            input_data = state.get("input") if isinstance(state.get("input"), dict) else {}
            output = state.get("output") if state.get("output") is not None else state.get("result")
            if output is None:
                output = part.get("output") if part.get("output") is not None else part.get("result")
            fallback_path = (
                input_data.get("filePath")
                or input_data.get("path")
                or input_data.get("file")
                or metadata.get("filepath")
                or metadata.get("path")
                or ""
            )
            candidates = [
                metadata.get("filediff"),
                metadata.get("fileDiff"),
                output.get("filediff") if isinstance(output, dict) else None,
                output.get("fileDiff") if isinstance(output, dict) else None,
                output if isinstance(output, dict) and any(key in output for key in ("patch", "additions", "deletions")) else None,
            ]
            candidate = next((value for value in candidates if isinstance(value, dict)), None)
            if not candidate:
                continue
            file_name = candidate.get("file") or candidate.get("filePath") or candidate.get("path") or fallback_path
            if not file_name:
                continue
            changes.append({
                "file": str(file_name),
                "additions": int(candidate.get("additions") or 0),
                "deletions": int(candidate.get("deletions") or 0),
                "patch": candidate.get("patch") if isinstance(candidate.get("patch"), str) else "",
            })
    return changes


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: check-kilo-prompt-e2e.py <launcher-base-url>", file=sys.stderr)
        return 2
    base = sys.argv[1].rstrip("/")

    local = request(base, "/local/status")
    project = local.get("project") if isinstance(local, dict) else None
    require(isinstance(project, str) and project, f"local project missing: {local!r}")

    agents = unwrap(request(base, routed("/runtime/agent", project)))
    require(isinstance(agents, list), f"agent response mismatch: {agents!r}")
    visible = [a for a in agents if isinstance(a, dict) and not a.get("hidden") and a.get("mode") != "subagent"]
    names = [str(a.get("name") or a.get("id") or "") for a in visible]
    require("code" in names, f"product code agent missing: {names!r}")
    require("build" not in names, f"raw build agent leaked through product API: {names!r}")

    provider_state = unwrap(request(base, routed("/runtime/provider", project)))
    require(isinstance(provider_state, dict), f"provider response mismatch: {provider_state!r}")
    providers = provider_state.get("all")
    require(isinstance(providers, list), "provider.all missing")
    test_provider = next((p for p in providers if isinstance(p, dict) and p.get("id") == "test"), None)
    require(test_provider is not None, f"test provider missing: {[p.get('id') for p in providers if isinstance(p, dict)]!r}")
    models = test_provider.get("models")
    if isinstance(models, dict):
        require("test-model" in models, f"test-model missing from provider: {models!r}")
    elif isinstance(models, list):
        require(
            any(isinstance(m, dict) and (m.get("id") == "test-model" or m.get("modelID") == "test-model") for m in models),
            f"test-model missing from provider: {models!r}",
        )
    else:
        raise E2EError(f"test provider models have invalid shape: {models!r}")

    created = unwrap(request(base, routed("/runtime/session", project), method="POST", payload={}))
    require(isinstance(created, dict) and isinstance(created.get("id"), str), f"session creation failed: {created!r}")
    session_id = created["id"]
    sid = urllib.parse.quote(session_id, safe="")

    events: list[dict] = []
    ready = threading.Event()
    stop = threading.Event()
    thread = threading.Thread(target=sse_events, args=(base, project, events, ready, stop), daemon=True)
    thread.start()
    require(ready.wait(5), f"global SSE did not connect: {events!r}")
    require(not any(event.get("type") == "test.sse.error" for event in events), f"SSE failed: {events!r}")

    request(
        base,
        routed(f"/runtime/session/{sid}/prompt_async", project),
        method="POST",
        payload={
            "agent": "code",
            "model": {"providerID": "test", "modelID": "test-model"},
            "parts": [{"type": "text", "text": "Create the requested fixture file, then confirm completion."}],
        },
    )

    deadline = time.time() + 45
    messages: list[dict] = []
    saw_running = False
    approved_permissions: set[str] = set()
    saw_edit_permission = False

    while time.time() < deadline:
        statuses = unwrap(request(base, routed("/runtime/session/status", project)))
        statuses = statuses if isinstance(statuses, dict) else {}
        status = statuses.get(session_id)
        if isinstance(status, dict) and status.get("type") != "idle":
            saw_running = True

        pending = unwrap(request(base, routed("/runtime/permission", project)))
        pending = pending if isinstance(pending, list) else []
        for permission in pending:
            if not isinstance(permission, dict) or permission.get("sessionID") != session_id:
                continue
            permission_id = permission.get("id")
            if not isinstance(permission_id, str) or permission_id in approved_permissions:
                continue
            require(permission.get("permission") == "edit", f"unexpected permission request: {permission!r}")
            patterns = permission.get("patterns")
            require(isinstance(patterns, list) and any("hello.txt" in str(item) for item in patterns),
                    f"edit permission did not target hello.txt: {permission!r}")
            request(
                base,
                routed(f"/runtime/permission/{urllib.parse.quote(permission_id, safe='')}/reply", project),
                method="POST",
                payload={"reply": "once"},
            )
            approved_permissions.add(permission_id)
            saw_edit_permission = True

        messages = unwrap(request(base, routed(f"/runtime/session/{sid}/message", project, limit=200)))
        messages = messages if isinstance(messages, list) else []
        if any(EXPECTED in assistant_text(message) for message in messages):
            break

        for message in messages:
            info = message.get("info") if isinstance(message, dict) else None
            if isinstance(info, dict) and info.get("role") == "assistant" and info.get("error"):
                raise E2EError(f"assistant failed before fixture reply: {info.get('error')!r}; messages={messages!r}")

        sse_error = next((event for event in events if event.get("type") == "test.sse.error"), None)
        if sse_error:
            raise E2EError(f"SSE failed while waiting for completion: {sse_error!r}")
        time.sleep(0.1)
    else:
        raise E2EError(
            f"timed out waiting for assistant reply; saw_running={saw_running!r} "
            f"saw_edit_permission={saw_edit_permission!r} messages={messages!r} events={events!r}"
        )

    stop.set()

    users = [m for m in messages if isinstance(m, dict) and isinstance(m.get("info"), dict) and m["info"].get("role") == "user"]
    assistants = [m for m in messages if isinstance(m, dict) and isinstance(m.get("info"), dict) and m["info"].get("role") == "assistant"]
    require(users, f"no projected user message: {messages!r}")
    require(assistants, f"no projected assistant message: {messages!r}")
    require(saw_edit_permission, "write tool never requested edit permission")
    require(any(EXPECTED in assistant_text(m) for m in assistants), f"fixture reply missing: {assistants!r}")
    require(any(has_completed_write(m) for m in assistants), f"completed write tool part missing: {assistants!r}")

    target = os.path.join(project, "hello.txt")
    require(os.path.isfile(target), f"bundled runtime did not create {target}")
    with open(target, "r", encoding="utf-8") as handle:
        actual = handle.read()
    require(actual == FILE_CONTENT, f"file content mismatch: {actual!r}")

    aggregate_diffs = unwrap(request(base, routed(f"/runtime/session/{sid}/diff", project)))
    require(isinstance(aggregate_diffs, list), f"session diff must be an array: {aggregate_diffs!r}")
    visible_changes = aggregate_diffs if aggregate_diffs else projected_changes(messages)
    change_source = "session.diff" if aggregate_diffs else "tool-metadata"
    hello_diff = next(
        (item for item in visible_changes if isinstance(item, dict) and str(item.get("file") or "").replace("\\", "/").endswith("/hello.txt")),
        None,
    )
    require(hello_diff is not None, f"hello.txt missing from TL Studio change projection: {visible_changes!r}")
    require(int(hello_diff.get("additions") or 0) >= 1, f"hello.txt change additions missing: {hello_diff!r}")

    interesting = []
    for envelope in events:
        payload = envelope.get("payload", envelope) if isinstance(envelope, dict) else {}
        if isinstance(payload, dict):
            props = payload.get("properties") if isinstance(payload.get("properties"), dict) else {}
            info = props.get("info") if isinstance(props.get("info"), dict) else {}
            part = props.get("part") if isinstance(props.get("part"), dict) else {}
            sid_from_event = props.get("sessionID") or info.get("sessionID") or part.get("sessionID")
            if sid_from_event == session_id:
                interesting.append(payload.get("type"))
    require(any(t in {"message.updated", "message.part.updated", "session.status", "session.idle"} for t in interesting),
            f"no production session/message event observed for session: {interesting!r}")
    require("permission.asked" in interesting, f"permission.asked event missing: {interesting!r}")

    print(json.dumps({
        "ok": True,
        "session": session_id,
        "agent": "code",
        "model": "test/test-model",
        "messages": len(messages),
        "events": interesting,
        "saw_running": saw_running,
        "permission": "edit/once",
        "file": target,
        "changes_source": change_source,
        "change_files": [item.get("file") for item in visible_changes if isinstance(item, dict)],
        "reply": EXPECTED,
    }, indent=2))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except E2EError as error:
        print(f"PRODUCT E2E FAILURE: {error}", file=sys.stderr)
        raise SystemExit(1)
