# TL Studio

This npm package is a lightweight launcher for **TL Studio**, a fast local development workspace with AI built in.

It does not bundle or replace the TL Studio application. The launcher detects the current OS/architecture, downloads the matching official GitHub Release, verifies its SHA-256 checksum, caches it locally, and runs it in the current project directory.

Each npm package version is pinned to the matching TL Studio GitHub Release. For example, `tl-studio@0.1.0` launches `v0.1.0`, keeping runs reproducible.

## Quick start

Run TL Studio from the current project directory:

```bash
npx --yes tl-studio
```

Run without automatically opening the browser:

```bash
npx --yes tl-studio --no-browser
```

Prerelease builds, when available, use explicit dist-tags such as `alpha` and can be launched with commands such as `npx --yes tl-studio@alpha`.

## What gets stored locally?

The npm package itself uses the normal npm/npx cache. TL Studio release files are cached separately:

- Windows: `%LOCALAPPDATA%\\TL-Studio\\cache`
- Linux/macOS: `${XDG_CACHE_HOME:-~/.cache}/tl-studio`

Nothing is installed as a Windows service, system package, or global CLI unless you explicitly choose to install the npm package globally.

## Source and releases

- Source: https://github.com/pouramin/TL-Studio
- Releases: https://github.com/pouramin/TL-Studio/releases
- Issues: https://github.com/pouramin/TL-Studio/issues

TL Studio is MIT licensed. See the repository for third-party notices and runtime attribution.
