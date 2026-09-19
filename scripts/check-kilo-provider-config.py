#!/usr/bin/env python3
"""Exercise TL Studio's custom-provider routes against the pinned Kilo runtime."""

from __future__ import annotations

import json
import sys
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
        return json.loads(raw)


def query(project: str, **extra):
    return urllib.parse.urlencode({"directory": project, **extra})


def model_ids(provider):
    models = provider.get("models", {}) if isinstance(provider, dict) else {}
    if isinstance(models, dict):
        return set(models.keys())
    if isinstance(models, list):
        return {
            str(item.get("id") or item.get("modelID"))
            for item in models
            if isinstance(item, dict) and (item.get("id") or item.get("modelID"))
        }
    return set()


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: check-kilo-provider-config.py <launcher-base-url>", file=sys.stderr)
        return 2

    base = sys.argv[1].rstrip("/")
    local = request(base, "/local/status")
    require(isinstance(local, dict) and isinstance(local.get("project"), str), "local/status.project missing")
    project = local["project"]
    provider_id = "tl-studio-contract-provider"
    model_id = "contract-model"
    overlay_path = f"/runtime/config/overlay?{query(project, scope='global')}"

    overlay = unwrap(request(base, overlay_path))
    require(isinstance(overlay, dict), "config overlay must be an object")
    effective = overlay.get("effective") if isinstance(overlay.get("effective"), dict) else {}
    original = effective.get("provider") if isinstance(effective.get("provider"), dict) else {}

    providers = dict(original)
    providers[provider_id] = {
        "name": "TL Studio Contract Provider",
        "npm": "@ai-sdk/openai-compatible",
        "options": {"baseURL": "http://127.0.0.1:9/v1"},
        "models": {
            model_id: {
                "name": "Contract Model",
                "tool_call": True,
                "reasoning": False,
                "limit": {"context": 32768, "output": 4096},
            }
        },
    }

    try:
        updated = unwrap(request(base, f"/runtime/config/overlay?{query(project)}", method="PATCH", payload={
            "scope": "global",
            "set": {"provider": providers},
        }))
        require(isinstance(updated, dict), "config.overlay update must return an object")

        auth = unwrap(request(base, f"/runtime/auth/{urllib.parse.quote(provider_id, safe='')}", method="PUT", payload={
            "type": "api",
            "key": "tl-studio-contract-key",
        }))
        require(auth is True, f"auth.set mismatch: {auth!r}")

        disposed = unwrap(request(base, "/runtime/global/dispose", method="POST"))
        require(disposed is True, f"global.dispose mismatch: {disposed!r}")

        state = unwrap(request(base, f"/runtime/provider?{query(project)}"))
        require(isinstance(state, dict), "provider state must be an object")
        all_providers = state.get("all") if isinstance(state.get("all"), list) else []
        hit = next((item for item in all_providers if isinstance(item, dict) and item.get("id") == provider_id), None)
        require(hit is not None, f"custom provider did not load: {[p.get('id') for p in all_providers if isinstance(p, dict)]!r}")
        require(model_id in model_ids(hit), f"custom model did not load: {hit!r}")
        connected = state.get("connected") if isinstance(state.get("connected"), list) else []
        require(provider_id in connected, f"stored API auth did not connect provider: {connected!r}")
    finally:
        try:
            latest = unwrap(request(base, overlay_path))
            latest_effective = latest.get("effective") if isinstance(latest, dict) and isinstance(latest.get("effective"), dict) else {}
            cleanup = dict(latest_effective.get("provider") if isinstance(latest_effective.get("provider"), dict) else {})
            cleanup[provider_id] = None
            request(base, f"/runtime/config/overlay?{query(project)}", method="PATCH", payload={
                "scope": "global",
                "set": {"provider": cleanup},
            })
        except Exception as error:
            print(f"warning: provider cleanup failed: {error}", file=sys.stderr)
        try:
            request(base, f"/runtime/auth/{urllib.parse.quote(provider_id, safe='')}", method="DELETE")
            request(base, "/runtime/global/dispose", method="POST")
        except Exception as error:
            print(f"warning: auth cleanup failed: {error}", file=sys.stderr)

    print(json.dumps({
        "ok": True,
        "provider": provider_id,
        "model": model_id,
        "config_overlay": True,
        "auth_store": True,
        "catalog_reload": True,
    }, indent=2))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except ContractError as error:
        print(f"CUSTOM PROVIDER CONTRACT FAILURE: {error}", file=sys.stderr)
        raise SystemExit(1)
