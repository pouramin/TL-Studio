#!/usr/bin/env python3
"""Exercise TL Studio-owned custom-provider and credential routes against the pinned runtime."""

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
    provider = {
        "id": provider_id,
        "name": "TL Studio Contract Provider",
        "protocol": "openai-compatible",
        "baseURL": "http://127.0.0.1:9/v1",
        "models": [{
            "id": model_id,
            "name": "Contract Model",
            "toolCall": True,
            "reasoning": False,
            "contextLimit": 32768,
            "outputLimit": 4096,
        }],
    }

    # Bootstrap TL Studio's provider registry before mutation.
    initial = request(base, "/runtime/providers/config")
    require(isinstance(initial, dict) and isinstance(initial.get("providers"), list),
            f"provider config shape mismatch: {initial!r}")

    try:
        saved = request(
            base,
            f"/runtime/providers/config/{urllib.parse.quote(provider_id, safe='')}",
            method="PUT",
            payload={"provider": provider, "apiKey": "tl-studio-contract-key"},
        )
        require(isinstance(saved, dict) and saved.get("id") == provider_id,
                f"semantic provider save mismatch: {saved!r}")
        require("apiKey" not in saved and "key" not in saved,
                f"provider response leaked credential material: {saved!r}")

        config = request(base, "/runtime/providers/config")
        require(isinstance(config, dict) and isinstance(config.get("providers"), list),
                f"provider config missing after save: {config!r}")
        managed = next(
            (item for item in config["providers"] if isinstance(item, dict) and item.get("id") == provider_id),
            None,
        )
        require(managed is not None, f"TL Studio registry did not persist provider: {config!r}")
        require("apiKey" not in json.dumps(config) and "tl-studio-contract-key" not in json.dumps(config),
                "TL Studio provider registry exposed credential material")

        catalog = request(base, f"/runtime/providers/catalog?{query(project)}")
        require(isinstance(catalog, dict), f"catalog must be an object: {catalog!r}")
        all_providers = catalog.get("all") if isinstance(catalog.get("all"), list) else []
        hit = next((item for item in all_providers if isinstance(item, dict) and item.get("id") == provider_id), None)
        require(hit is not None, f"custom provider did not load: {[p.get('id') for p in all_providers if isinstance(p, dict)]!r}")
        require(hit.get("source") == "custom", f"provider is not TL Studio-owned in catalog: {hit!r}")
        require(model_id in model_ids(hit), f"custom model did not load: {hit!r}")
        connected = catalog.get("connected") if isinstance(catalog.get("connected"), list) else []
        require(provider_id in connected, f"owned API credential did not connect provider: {connected!r}")
    finally:
        try:
            request(
                base,
                f"/runtime/providers/config/{urllib.parse.quote(provider_id, safe='')}",
                method="DELETE",
            )
        except Exception as error:
            print(f"warning: semantic provider cleanup failed: {error}", file=sys.stderr)

    after = request(base, "/runtime/providers/config")
    remaining = after.get("providers") if isinstance(after, dict) and isinstance(after.get("providers"), list) else []
    require(not any(isinstance(item, dict) and item.get("id") == provider_id for item in remaining),
            f"provider remained in TL Studio registry after delete: {remaining!r}")

    print(json.dumps({
        "ok": True,
        "provider": provider_id,
        "model": model_id,
        "semantic_provider_contract": True,
        "credential_owner": "tl-studio",
        "runtime_sync": True,
        "catalog_reload": True,
    }, indent=2))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except ContractError as error:
        print(f"CUSTOM PROVIDER CONTRACT FAILURE: {error}", file=sys.stderr)
        raise SystemExit(1)
