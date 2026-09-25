#!/usr/bin/env python3
"""Regression checks for bundled plugin release staging without network access."""

from __future__ import annotations

import hashlib
import importlib.util
import json
import tarfile
import tempfile
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
MODULE_PATH = ROOT / "scripts" / "stage-bundled-plugins.py"
spec = importlib.util.spec_from_file_location("tl_stage_bundled_plugins", MODULE_PATH)
if spec is None or spec.loader is None:
    raise SystemExit("could not load bundled plugin staging module")
stage = importlib.util.module_from_spec(spec)
spec.loader.exec_module(stage)


def require(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)


with tempfile.TemporaryDirectory(prefix="tl-bundled-stage-test-") as temp_name:
    temp = Path(temp_name)
    repo = temp / "repo"
    repo.mkdir()
    license_path = repo / "fixture-LICENSE"
    license_path.write_text("fixture license\n", encoding="utf-8")

    platforms = {}
    for target in sorted(stage.SUPPORTED_TARGETS):
        platforms[target] = {
            "url": "https://example.invalid/fixture",
            "sha256": "0" * 64,
            "archive": "raw",
        }

    manifest = {
        "version": 1,
        "plugins": [
            {
                "id": "fixture",
                "name": "Fixture",
                "description": "Fixture plugin",
                "version": "1.0.0",
                "executable": "fixture-mcp",
                "transport": "stdio",
                "license": "MIT",
                "upstream": "https://example.invalid/fixture",
                "licenseFile": "fixture-LICENSE",
                "platforms": platforms,
            }
        ],
    }
    stage.validate_manifest(manifest, repo)

    bad = json.loads(json.dumps(manifest))
    del bad["plugins"][0]["platforms"]["macos-arm64"]
    try:
        stage.validate_manifest(bad, repo)
    except SystemExit:
        pass
    else:
        raise AssertionError("manifest validation must reject missing TL Studio targets")

    payload = b"TL_STUDIO_BUNDLED_PLUGIN_FIXTURE"
    raw = temp / "fixture.raw"
    raw.write_bytes(payload)
    require(stage.sha256(raw) == hashlib.sha256(payload).hexdigest(), "sha256 regression")

    raw_out = temp / "raw-out"
    stage.extract_member(raw, "raw", "", raw_out)
    require(raw_out.read_bytes() == payload, "raw staging regression")

    zipped = temp / "fixture.zip"
    with zipfile.ZipFile(zipped, "w") as archive:
        archive.writestr("nested/fixture-mcp", payload)
    zip_out = temp / "zip-out"
    stage.extract_member(zipped, "zip", "nested/fixture-mcp", zip_out)
    require(zip_out.read_bytes() == payload, "zip member staging regression")

    tarred = temp / "fixture.tar.gz"
    member_source = temp / "member-source"
    member_source.write_bytes(payload)
    with tarfile.open(tarred, "w:gz") as archive:
        archive.add(member_source, arcname="nested/fixture-mcp")
    tar_out = temp / "tar-out"
    stage.extract_member(tarred, "tar.gz", "nested/fixture-mcp", tar_out)
    require(tar_out.read_bytes() == payload, "tar.gz member staging regression")

print("bundled plugin staging regressions: ok")
