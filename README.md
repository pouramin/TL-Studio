[English](./README.md) | [فارسی](./README.fa_IR.md)

<p align="center">
  <img src="./media/tl-studio-logo.svg" width="360" alt="TL Studio">
</p>

<p align="center"><strong>Development branch: 0.4.0-alpha.2</strong> · Stable release: v0.3.0.</p>

<p align="center">
  A fast local development workspace with AI built in.
</p>

<p align="center">
  <a href="https://www.npmjs.com/package/tl-studio"><img src="https://img.shields.io/npm/v/tl-studio" alt="npm"></a>
  <a href="https://github.com/pouramin/TL-Studio/releases"><img src="https://img.shields.io/github/v/release/pouramin/TL-Studio?sort=semver" alt="Release"></a>
  <a href="https://github.com/pouramin/TL-Studio/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/pouramin/TL-Studio/ci.yml?branch=main&label=CI" alt="CI"></a>
  <a href="https://github.com/pouramin/TL-Studio/releases"><img src="https://img.shields.io/github/downloads/pouramin/TL-Studio/total" alt="Downloads"></a>
  <a href="./LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License"></a>
</p>

**TL Studio** is a local browser-based development workspace where you can edit code yourself and work alongside an AI agent. Open a project, browse and edit files, search across the codebase, run commands, preview the app, choose models/providers, and hand work to the agent — all on your own computer.

No VS Code, JetBrains, Cursor, Docker, hosted TL Studio backend, database, or project-owned cloud service is required.

## Quick Start

### One-command launch

If Node.js/npm is installed, run this inside the project directory you want to work on:

```bash
npx --yes tl-studio
```

The npm package is a lightweight launcher pinned to the matching stable TL Studio GitHub Release. It detects the operating system and architecture, downloads the official archive, verifies its SHA-256 checksum, caches it locally, and opens TL Studio with the current directory selected.

Run without automatically opening the browser:

```bash
npx --yes tl-studio --no-browser
```

### Portable release

Download your platform archive from **[GitHub Releases](https://github.com/pouramin/TL-Studio/releases)**, extract it, and run:

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
- **Plugins & MCP** — add arbitrary stdio MCP servers from Settings without editing config files; TL Studio discovers their tools dynamically, namespaces them, routes them through the native Tool Registry and permission engine, and exposes enabled tools to the native Agent.
- **File attachments** — attach images, PDFs, and text/code files; multi-select, drag/drop, and clipboard paste are supported.
- **TL Studio-native Agent execution** — supported custom providers now run through a TL Studio-owned model/tool/model loop with cancellation, loop guards, semantic persistence, and live events; hosted Kilo remains available through the compatibility adapter.
- **TL Studio Tool Executor** — core coding tools for project file read/list/write/edit, project search, and terminal commands execute through TL Studio-owned handlers with project confinement, validation, cancellation, and permission enforcement.
- **TL Studio Tool Registry** — product tools use TL Studio-owned names, categories, capability metadata, permission classes, schemas, and presentation hints.
- **Live agent activity** — compact Reasoning and Tool cards with live status updates using TL Studio tool semantics.
- **TL Studio permission policies & questions** — approve one-time actions, remember project-scoped non-sensitive rules in TL Studio, forget saved rules from Settings, reject actions, and answer interactive questions through launcher-owned `/local/questions*` semantics rather than raw runtime question routes.
- **Stop & recovery** — interrupt active work and recover from stalled or retryable upstream failures.
- **TL Studio session read model** — current sessions, messages, activity, status, usage metadata, and session changes are projected through launcher-owned semantic `/local/sessions*` contracts instead of exposing the runtime's raw message envelope to the product UI.
- **TL Studio session command contract** — session create, rename, delete, run/prompt, and abort now use launcher-owned semantic `/local/sessions*` routes; the active engine adapter translates those commands to its private implementation API.
- **TL Studio session persistence** — semantic session metadata, transcripts, usage/activity history, and changes are mirrored into TL Studio-owned local storage. History remains readable after runtime history loss, and persisted-only sessions can still be renamed or deleted locally.
- **TL Studio credential vault** — custom-provider API keys are owned by TL Studio, never written to `providers.json` or browser storage, restored into the active runtime when needed, and protected with Windows DPAPI, macOS Keychain, Linux Secret Service when available, or an encrypted private-file fallback.
- **Session management** — create, resume, rename, delete, and switch sessions across recent projects.
- **Project-scoped usage** — per-turn and project totals for tokens, requests, time, reasoning, and cache usage.
- **Changes panel** — inspect changed files, addition/deletion counts, and patches.
- **Project workspace** — writable local file explorer plus a locally bundled, lazy-loaded Monaco editor with multi-tab editing, find/replace, multi-cursor editing, save/create/rename/delete actions, external-change reconciliation, and **Show in Folder** for revealing the active file in the native system file manager.
- **Project Search** — fast project-wide text search with include/exclude filters and click-to-open results.
- **Integrated terminal** — project-scoped command execution, output history, stop controls, and process-tree termination.
- **Live Preview** — capability-driven local preview in a movable/resizable browser window. TL Studio follows the active previewable file across HTML, SVG/raster images, PDF, video, audio, rendered Markdown, and rendered plain text; PDF is served directly with inline MIME/disposition and HTTP range support for the browser's native PDF viewer. Previewable binary media opens as a real read-only Workspace tab, while the floating Preview can be resized from all four edges and all four corners.
- **Appearance & editor settings** — System, Dark, and Light themes plus editor theme and separate UI/code/terminal font controls.
- **Strict TypeScript + real Browser modules** — all Browser UI source under `cmd/launcher/ui` is type-checked with `strict: true`; `kernel.ts` exports the shared typed Browser kernel, feature modules import it directly, `browser.ts` defines the ES-module graph, and esbuild produces one primary `browser.js` bundle. No `window.KLU` dependency or legacy per-module JavaScript build is required.
- **Local-first security** — loopback-only UI, random per-run backend password, origin checks, and restrictive CSP.
- **No TL Studio telemetry or cloud service** — model traffic goes directly through the provider/runtime configuration selected by the user.

## Plugins & MCP

Open:

```text
Settings
→ Plugins
→ + Add Plugin
```

The initial plugin transport is **MCP over stdio**. Enter the MCP server command, one argument per line, optional environment variables, working directory, and scope. Use **Test Connection** before saving, then explicitly enable the plugin. Secret environment values are stored through TL Studio's credential vault and are not returned to the browser after saving.

Enabled MCP servers are initialized by TL Studio, their tools are discovered dynamically, and tool IDs are namespaced as:

```text
mcp.<plugin-id>.<tool-name>
```

This is a generic plugin path, not a Graphify-specific integration. Another stdio MCP server can be added through the same screen without adding a custom Agent adapter.

### Graphify example

If Graphify and its MCP executable are already installed, a project-scoped plugin can use:

```text
Name: Graphify
Command: graphify-mcp
Arguments:
graphify-out/graph.json
```

Graphify's MCP tools are discovered at runtime; TL Studio does not hardcode its tool list. The Graphify card additionally offers **Build/Rebuild Graph** and **Open Graph** conveniences. Graph building runs the fixed local command `graphify extract . --code-only` only after explicit confirmation, while **Open Graph** reuses TL Studio's existing Preview for `graphify-out/graph.html`.

## Architecture

```text
Browser workspace
    │ localhost only
    ▼
TL Studio launcher (Go)
    │
    ├─ provider/model registry + credential vault
    ├─ Plugin Manager
    │    └─ MCP Client Manager
    │         ├─ stdio MCP servers
    │         └─ future transports behind the MCP client interface
    │
    ├─ Tool Registry ← discovered MCP tools
    ├─ Permission Engine
    ├─ Native Tool Executor
    ├─ Native Agent loop
    ├─ semantic sessions / persistence / live events
    ├─ project files / search / terminal / preview
    │
    ├─ supported custom providers → direct model APIs
    │
    └─ compatibility adapter → bundled Kilo engine
                              → hosted Kilo / compatibility capabilities
```

TL Studio owns the workspace, product UI, local launcher, provider/model definitions, custom-provider credentials, Plugin Manager, MCP normalization, tool semantics/metadata, semantic session persistence/read/command models, semantic question handling, semantic live-event projection, project-scoped permission policy, project/session experience, recovery behavior, and release packaging. MCP tools enter the same native Agent/tool/permission path as built-in tools; they do not create a parallel Agent architecture. Custom provider definitions, plugin definitions, and semantic session history are persisted in TL Studio-owned state. Plugin secret environment values stay in the credential vault instead of plugin JSON.

For supported custom-provider coding, TL Studio owns the Agent loop and core tool execution directly. Kilo remains bundled as the currently tested compatibility engine for hosted Kilo authentication/models and capabilities not yet provided by the native path.

The selected project stays on the user's computer, and TL Studio does not proxy model traffic through project-owned infrastructure.

## Runtime boundary

TL Studio's browser and product UI depend on TL Studio-owned contracts, not on an engine-specific browser API. Current session reads, persistence, and session commands use launcher-owned `/local/sessions*` semantics; interactive questions use `/local/questions*`; permissions use `/local/permissions*`; and live Browser updates use `/local/events` semantic SSE. Custom-provider credentials are stored by TL Studio and synchronized into the active runtime only for execution. Remaining generic runtime capabilities stay behind the local `/runtime/*` adapter.

The current stable distribution bundles **Kilo Code 7.6.2** as the tested third-party compatibility engine. The launcher now selects it through a TL Studio-owned `runtimeEngine` boundary: binary discovery, process startup, credentials, project request scoping, and engine-specific request decoration live in the Kilo adapter instead of generic launcher/session/provider/permission/event code. That engine remains an implementation detail behind TL Studio's runtime adapter rather than the public product identity. Engine-specific compatibility is isolated in [`docs/KILO_API_CONTRACT.md`](./docs/KILO_API_CONTRACT.md), and required attribution is kept in [`THIRD_PARTY_NOTICES.md`](./THIRD_PARTY_NOTICES.md).

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
   ├─ KILO_LICENSE.txt
   ├─ MONACO_LICENSE.txt
   └─ MONACO_THIRD_PARTY_NOTICES.txt
```

## Run from source

Development requirements:

- Go 1.23+
- Node.js 18+ and npm for the local Browser build
- the compatible local runtime binary in `PATH`, beside the launcher, or supplied explicitly

Build the Browser assets once after cloning or after Browser-source changes:

```bash
npm install --ignore-scripts --no-audit --no-fund
npm run build:web
```

Then run the launcher:

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

Experimental builds continue on private Preview Build artifacts from `dev` without changing the stable `latest` npm path or GitHub stable release.

## License & attribution

TL Studio launcher/UI code is MIT licensed. The bundled Kilo Code runtime is also MIT licensed and remains a separate upstream project. Release archives retain its license notice; see [`THIRD_PARTY_NOTICES.md`](./THIRD_PARTY_NOTICES.md).

TL Studio is an independent project and is not an official product of its runtime upstream.

Built under the **TunnelLab** identity.
