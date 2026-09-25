#!/usr/bin/env python3
"""Validate and stage version-pinned TL Studio bundled MCP plugins."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import stat
import tarfile
import tempfile
import urllib.request
import zipfile
from pathlib import Path, PurePosixPath

SUPPORTED_TARGETS = {
    "windows-x64",
    "linux-x64",
    "linux-arm64",
    "macos-x64",
    "macos-arm64",
}
SUPPORTED_ARCHIVES = {"raw", "zip", "tar.gz"}


def fail(message: str) -> None:
    raise SystemExit(f"bundled plugin staging failed: {message}")


def read_manifest(path: Path) -> dict:
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except Exception as exc:
        fail(f"cannot read {path}: {exc}")
    if data.get("version") != 1:
        fail("manifest version must be 1")
    plugins = data.get("plugins")
    if not isinstance(plugins, list):
        fail("manifest plugins must be an array")
    return data


def normalized_relpath(value: str, label: str) -> PurePosixPath:
    path = PurePosixPath(value)
    if not value or path.is_absolute() or ".." in path.parts:
        fail(f"{label} must be a safe repository-relative path")
    return path


def validate_manifest(data: dict, repo_root: Path) -> None:
    seen: set[str] = set()
    for index, plugin in enumerate(data["plugins"]):
        if not isinstance(plugin, dict):
            fail(f"plugin #{index + 1} must be an object")
        plugin_id = str(plugin.get("id") or "").strip()
        if not plugin_id or any(ch not in "abcdefghijklmnopqrstuvwxyz0123456789-_" for ch in plugin_id):
            fail(f"plugin #{index + 1} has invalid id")
        if plugin_id[0] not in "abcdefghijklmnopqrstuvwxyz0123456789":
            fail(f"plugin {plugin_id!r} has invalid id")
        if plugin_id in seen:
            fail(f"duplicate plugin id {plugin_id!r}")
        seen.add(plugin_id)

        for key in ("name", "version", "executable", "transport", "license", "upstream"):
            if not isinstance(plugin.get(key), str) or not plugin[key].strip():
                fail(f"plugin {plugin_id!r} requires non-empty {key}")
        executable = plugin["executable"].strip()
        if "/" in executable or "\\" in executable:
            fail(f"plugin {plugin_id!r} executable must be a file name")
        if plugin["transport"] != "stdio":
            fail(f"plugin {plugin_id!r} currently must use stdio")

        license_file = normalized_relpath(str(plugin.get("licenseFile") or ""), f"{plugin_id} licenseFile")
        if not (repo_root / license_file).is_file():
            fail(f"plugin {plugin_id!r} licenseFile does not exist: {license_file}")

        notice = plugin.get("noticeFile")
        if notice:
            notice_file = normalized_relpath(str(notice), f"{plugin_id} noticeFile")
            if not (repo_root / notice_file).is_file():
                fail(f"plugin {plugin_id!r} noticeFile does not exist: {notice_file}")

        platforms = plugin.get("platforms")
        if not isinstance(platforms, dict):
            fail(f"plugin {plugin_id!r} platforms must be an object")
        missing = SUPPORTED_TARGETS.difference(platforms)
        extra = set(platforms).difference(SUPPORTED_TARGETS)
        if missing:
            fail(f"plugin {plugin_id!r} is missing supported targets: {', '.join(sorted(missing))}")
        if extra:
            fail(f"plugin {plugin_id!r} has unsupported targets: {', '.join(sorted(extra))}")

        for target, asset in platforms.items():
            if not isinstance(asset, dict):
                fail(f"plugin {plugin_id!r} target {target} must be an object")
            url = str(asset.get("url") or "").strip()
            checksum = str(asset.get("sha256") or "").strip().lower()
            archive = str(asset.get("archive") or "").strip()
            member = str(asset.get("member") or "").strip()
            if not url.startswith("https://"):
                fail(f"plugin {plugin_id!r} target {target} requires an https URL")
            if len(checksum) != 64 or any(ch not in "0123456789abcdef" for ch in checksum):
                fail(f"plugin {plugin_id!r} target {target} has invalid sha256")
            if archive not in SUPPORTED_ARCHIVES:
                fail(f"plugin {plugin_id!r} target {target} has unsupported archive type {archive!r}")
            if archive != "raw":
                normalized_relpath(member, f"{plugin_id} {target} member")


def download(url: str, destination: Path) -> None:
    request = urllib.request.Request(url, headers={"User-Agent": "TL-Studio-release"})
    with urllib.request.urlopen(request, timeout=60) as response, destination.open("wb") as out:
        shutil.copyfileobj(response, out)


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def extract_member(downloaded: Path, archive: str, member: str, destination: Path) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    if archive == "raw":
        shutil.copyfile(downloaded, destination)
        return
    if archive == "zip":
        with zipfile.ZipFile(downloaded) as zipped:
            try:
                info = zipped.getinfo(member)
            except KeyError:
                fail(f"archive does not contain {member!r}")
            if info.is_dir():
                fail(f"archive member {member!r} is not a file")
            with zipped.open(info) as source, destination.open("wb") as out:
                shutil.copyfileobj(source, out)
        return
    if archive == "tar.gz":
        with tarfile.open(downloaded, "r:gz") as tar:
            try:
                info = tar.getmember(member)
            except KeyError:
                fail(f"archive does not contain {member!r}")
            if not info.isfile():
                fail(f"archive member {member!r} is not a regular file")
            source = tar.extractfile(info)
            if source is None:
                fail(f"could not read archive member {member!r}")
            with source, destination.open("wb") as out:
                shutil.copyfileobj(source, out)
        return
    fail(f"unsupported archive type {archive!r}")


def executable_name(plugin: dict, target: str) -> str:
    name = plugin["executable"].strip()
    if target.startswith("windows-") and not name.lower().endswith(".exe"):
        return f"{name}.exe"
    return name


def stage_plugin(plugin: dict, target: str, package_dir: Path, repo_root: Path) -> None:
    asset = plugin["platforms"][target]
    plugin_dir = package_dir / "plugins" / plugin["id"]
    binary = plugin_dir / "bin" / executable_name(plugin, target)
    plugin_dir.mkdir(parents=True, exist_ok=True)

    with tempfile.TemporaryDirectory(prefix="tl-bundled-plugin-") as temp_name:
        downloaded = Path(temp_name) / "asset"
        download(asset["url"], downloaded)
        actual = sha256(downloaded)
        expected = asset["sha256"].lower()
        if actual != expected:
            fail(
                f"plugin {plugin['id']!r} {target} SHA-256 mismatch: "
                f"expected {expected}, got {actual}"
            )
        extract_member(downloaded, asset["archive"], str(asset.get("member") or ""), binary)

    if not target.startswith("windows-"):
        mode = binary.stat().st_mode
        binary.chmod(mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

    shutil.copy2(repo_root / plugin["licenseFile"], plugin_dir / "LICENSE")
    if plugin.get("noticeFile"):
        shutil.copy2(repo_root / plugin["noticeFile"], plugin_dir / "NOTICE")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--manifest", default="cmd/launcher/bundled_plugins.json")
    parser.add_argument("--repo-root", default=".")
    parser.add_argument("--target", choices=sorted(SUPPORTED_TARGETS))
    parser.add_argument("--package-dir")
    parser.add_argument("--validate-only", action="store_true")
    args = parser.parse_args()

    repo_root = Path(args.repo_root).resolve()
    manifest_path = (repo_root / args.manifest).resolve()
    data = read_manifest(manifest_path)
    validate_manifest(data, repo_root)

    if args.validate_only:
        print(f"Bundled plugin manifest valid: {len(data['plugins'])} plugin(s)")
        return

    if not args.target or not args.package_dir:
        fail("--target and --package-dir are required unless --validate-only is used")
    package_dir = Path(args.package_dir).resolve()
    package_dir.mkdir(parents=True, exist_ok=True)

    for plugin in data["plugins"]:
        stage_plugin(plugin, args.target, package_dir, repo_root)

    if data["plugins"]:
        manifest_output = package_dir / "plugins" / "manifest.json"
        manifest_output.parent.mkdir(parents=True, exist_ok=True)
        manifest_output.write_text(
            json.dumps(
                {
                    "version": 1,
                    "plugins": [
                        {
                            "id": plugin["id"],
                            "name": plugin["name"],
                            "version": plugin["version"],
                            "license": plugin["license"],
                            "upstream": plugin["upstream"],
                        }
                        for plugin in data["plugins"]
                    ],
                },
                indent=2,
            )
            + "\n",
            encoding="utf-8",
        )
    print(f"Bundled plugins staged for {args.target}: {len(data['plugins'])}")


if __name__ == "__main__":
    main()
