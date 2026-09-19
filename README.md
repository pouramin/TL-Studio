[English](./README.md) | [فارسی](./README.fa_IR.md)

<p align="center">
  <img src="./media/tl-studio-logo.svg" width="300" alt="TL Studio">
</p>

<h1 align="center">TL Studio</h1>

<p align="center">
  A fast local development workspace with AI built in.
</p>

<p align="center">
  <a href="https://github.com/pouramin/TL-Studio/releases"><img src="https://img.shields.io/github/v/release/pouramin/TL-Studio?sort=semver" alt="Release"></a>
  <a href="https://github.com/pouramin/TL-Studio/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/pouramin/TL-Studio/ci.yml?branch=main&label=CI" alt="CI"></a>
  <a href="https://www.npmjs.com/package/tl-studio"><img src="https://img.shields.io/npm/v/tl-studio" alt="npm"></a>
  <a href="https://github.com/pouramin/TL-Studio/releases"><img src="https://img.shields.io/github/downloads/pouramin/TL-Studio/total" alt="Downloads"></a>
  <a href="./LICENSE"><img src="https://img.shields.io/github/license/pouramin/TL-Studio" alt="License"></a>
</p>

**TL Studio** is a local browser-based development workspace where you can edit code yourself and work alongside an AI agent. Open a project, browse and edit files, search across the codebase, run commands, preview the app, choose models/providers, and hand work to the agent — all on your own computer.

No VS Code, JetBrains, Cursor, Docker, hosted TL Studio backend, database, or project-owned cloud service is required.

## Quick Start

### One-command launch

If Node.js/npm is installed, run this inside the project directory you want to work on:

```bash
npx --yes tl-studio
```

The npm package is only a lightweight launcher. It detects the operating system and architecture, downloads the matching official TL Studio GitHub Release, verifies its SHA-256 checksum, caches it locally, and opens TL Studio with the current directory selected.

Run without automatically opening the browser:

```bash
npx --yes tl-studio --no-browser
```

Prerelease channels can still be launched explicitly when available, for example:

```bash
npx --yes tl-studio@alpha
```

### Portable release

Node.js is not required for the portable build. Download your platform archive from **[GitHub Releases](https://github.com/pouramin/TL-Studio/releases)**, extract it, and run:

```text
Windows:  tl-studio.exe
Linux:    ./tl-studio
macOS:    ./tl-studio
```

The release already includes the pinned local agent runtime.

## Features

- **Standalone local development workspace** — edit files, search the project, run commands, preview the app, and work with an AI agent in one browser workspace.
- **Local project picker** — open project folders with the operating-system folder picker.
- **Agent & model selection** — switch agents and available provider models from the composer.
- **Custom providers** — connect OpenAI-compatible, OpenAI Responses, and Anthropic-compatible endpoints with your own credentials.
- **File attachments** — attach images, PDFs, and text/code files; multi-select, drag/drop, and clipboard paste are supported.
- **Live agent activity** — compact Reasoning and Tool cards with live status updates.
- **Permissions & questions** — approve one-time actions, save matching rules when supported, reject actions, and answer interactive questions.
- **Stop & recovery** — interrupt active work and recover from stalled or retryable upstream failures.
- **Session management** — create, resume, rename, delete, and switch sessions across recent projects.
- **Project-scoped usage** — per-turn and project totals for tokens, requests, time, reasoning, and cache usage.
- **Changes panel** — inspect changed files, addition/deletion counts, and patches.
- **Project workspace** — writable local file explorer, multi-tab editor, save/create/rename/delete actions, and external-change reconciliation.
- **Project Search** — fast project-wide text search with include/exclude filters and click-to-open results.
- **Integrated terminal** — project-scoped command execution, output history, stop controls, and process-tree termination.
- **Live Preview** — local static or Node dev-server preview in a movable/resizable browser window.
- **Appearance & editor settings** — System, Dark, and Light themes plus editor theme and separate UI/code/terminal font controls.
- **Local-first security** — loopback-only UI, random per-run backend password, origin checks, and restrictive CSP.
- **No TL Studio telemetry or cloud service** — model traffic goes directly through the provider/runtime configuration selected by the user.

## Architecture

```text
Browser workspace
    │ localhost only
    ▼
TL Studio launcher (Go)
    │
    ├─ TL Studio provider/model registry
    ├─ project files / search / terminal / preview
    │
    └─ authenticated runtime adapter
            ▼
        Local agent runtime
            ├─ agents / sessions / tools
            ├─ permissions / questions / live events
            └─ provider execution / model inference
```

TL Studio owns the workspace, product UI, local launcher, provider/model definitions, project/session experience, recovery behavior, and release packaging. Custom provider definitions are persisted in TL Studio's local state and translated to the active runtime by the launcher. Provider credentials are currently delegated to the runtime's local credential store and are not written into TL Studio's provider registry. The runtime remains a replaceable infrastructure layer behind that product boundary.

The selected project stays on the user's computer, and TL Studio does not proxy model traffic through project-owned infrastructure.

## Runtime boundary

TL Studio's browser and product UI depend on TL Studio-owned contracts, not on an engine-specific browser API. Browser runtime traffic stays under the local `/runtime/*` boundary, while provider/model definitions, project files, search, terminal, preview, and related workspace behavior are owned by TL Studio.

The current stable distribution bundles **Kilo Code 7.6.2** as the tested third-party agent engine. That engine is an implementation detail behind TL Studio's runtime adapter rather than the public product identity. Engine-specific compatibility is isolated in [`docs/KILO_API_CONTRACT.md`](./docs/KILO_API_CONTRACT.md), and required attribution is kept in [`THIRD_PARTY_NOTICES.md`](./THIRD_PARTY_NOTICES.md).

CI validates the pinned engine through TL Studio's public runtime boundary for project routing, agent/provider/session APIs, async prompts, live events, permissions, provider configuration, tool execution, and real file writes.

## Supported builds

| Platform | Architecture |
| --- | --- |
| Windows | x64 |
| Linux | x64, ARM64 |
| macOS | Intel x64, Apple Silicon ARM64 |

## Release package

```text
tl-studio/
├─ tl-studio[.exe]
├─ bin/
│  └─ kilo[.exe]
├─ LICENSE
├─ THIRD_PARTY_NOTICES.md
└─ third_party/
   └─ KILO_LICENSE.txt
```

## Run from source

Development requirements:

- Go 1.23+
- the compatible local runtime binary in `PATH`, beside the launcher, or supplied explicitly

```bash
go run ./cmd/launcher
```

Open a specific project:

```bash
go run ./cmd/launcher --project /path/to/project
```

Use a specific runtime binary:

```bash
go run ./cmd/launcher --runtime-bin /path/to/runtime
```

Use `--no-browser` to suppress automatic browser launch.

## Zero-infrastructure rule

TL Studio is intentionally designed so the maintainer does not need to pay for a VPS, application hosting, database, API gateway, model inference, or telemetry backend. Source, issues, CI, release definitions, downloadable builds, and the lightweight npm launcher are distributed through GitHub/npm infrastructure.

Any paid AI usage is between the user and the provider they configure.

## Security model

The launcher:

1. binds the UI to loopback only (`127.0.0.1`, `localhost`, or `::1`),
2. starts the local runtime on loopback with a random per-run password,
3. keeps that password server-side,
4. routes the selected project directory locally,
5. rejects cross-origin browser requests, and
6. serves the UI with a restrictive Content Security Policy.

The agent runtime can read/write files and execute commands when permissions allow it. Only run TL Studio on projects and machines you trust.

## Status

TL Studio keeps the stable production line on `main` and experimental development on `dev`. Stable releases are promoted only after automated CI plus hands-on validation on a real Windows machine. The core path covered before promotion includes:

```text
TL Studio UI
→ local agent runtime
→ selected model
→ tool call
→ permission
→ local file write
→ final assistant response
```

Experimental builds continue on explicit prerelease channels without changing the stable `latest` npm path.

## License & attribution

TL Studio launcher/UI code is MIT licensed. The bundled Kilo Code runtime is also MIT licensed and remains a separate upstream project. Release archives retain its license notice; see [`THIRD_PARTY_NOTICES.md`](./THIRD_PARTY_NOTICES.md).

TL Studio is an independent project and is not an official product of its runtime upstream.

Built under the **TunnelLab** identity.
