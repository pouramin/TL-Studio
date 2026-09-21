# Architecture

## Components

### Launcher

A single Go binary using only the Go standard library. It owns the lifecycle of the bundled coding runtime, chooses ephemeral ports, serves embedded static UI assets, exposes project-scoped local workspace APIs, and reverse-proxies the runtime API behind TL Studio's local product boundary.

The launcher is also the filesystem trust boundary for browser IDE operations. Browser requests never receive arbitrary host filesystem access: file reads and mutations are resolved relative to the selected project, traversal and symlink escapes are rejected, and workspace mutations protect Git metadata.

### Bundled coding runtime

The current implementation starts the bundled runtime on loopback with a random per-launch password. The runtime remains responsible for sessions, agents, actual tool execution, provider integrations, model execution, and filesystem operations initiated by the agent. TL Studio owns the semantic identity and presentation metadata for known tools through its Tool Registry.

The runtime is behind TL Studio's product boundary. The browser never connects to it directly and never receives its server password.

Implementation-specific API compatibility details are documented in [`KILO_API_CONTRACT.md`](./KILO_API_CONTRACT.md).

### Browser UI

Static HTML/CSS and compiled JavaScript are embedded in the launcher at build time. Browser application source is maintained in TypeScript under `cmd/launcher/ui`; `cmd/launcher/web/*.js` is build output. The normal TL Studio control surface talks only to the launcher origin.

The browser IDE keeps one in-memory buffer per open editor tab. User-initiated reads, saves, creates, renames, and deletes go through launcher-owned `/local/*` routes. File previews include a SHA-256 revision token; normal saves send that token back so an external edit by the agent, Git, or another process cannot be silently overwritten. A deliberate force-save is a separate explicit action after a conflict.

Runtime file/session events trigger workspace reconciliation. Clean open buffers follow disk changes automatically, while dirty buffers are preserved and marked when the disk version changes or disappears.

The enhanced editor uses a fully local Monaco bundle with same-origin workers and no CDN dependency. TL Studio still owns the file-buffer/save/conflict contract, and the original textarea editor remains a lightweight fallback if Monaco cannot load. User font/theme preferences are stored in browser-local settings.

## Project Search

Project Search is a TL Studio workspace capability owned by the launcher rather than the bundled coding runtime.

The browser sends a bounded query to `/local/search`. The launcher recursively scans only the currently selected project, groups matches by file, and returns deterministic path/line-ordered results with line, column, byte offsets, matched text, and a bounded snippet. The result shape intentionally preserves match location metadata so a future Replace in Files milestone can build on the same search foundation without moving filesystem authority into the browser.

Search follows the same local filesystem trust boundary as the rest of the workspace. It does not construct shell commands or require an external search binary. Symlink entries are not followed, obvious heavy/generated directories such as `.git`, `node_modules`, `dist`, `build`, `out`, `coverage`, and `.next` are skipped by default, binary/non-UTF-8 files are ignored, and individual searchable files plus total results are bounded.

The browser Search UI supports case-sensitive search and lightweight include/exclude patterns. Rapid query changes abort the previous HTTP request and stale generations are discarded. Clicking a match opens or activates the file in the existing editor and selects the matching range. The standard project-search shortcut is `Ctrl+Shift+F` or `Cmd+Shift+F`.

## Local process manager and Terminal

TL Studio owns a project-scoped local process manager behind `/local/process`.

- Commands run with the currently selected project as their working directory.
- Output is kept in a bounded in-memory buffer and surfaced to the browser locally.
- Long-running processes can be stopped by TL Studio.
- On Windows, stop terminates the process tree so child dev servers are not left behind.
- Completed process records are retained only temporarily.

The current Terminal UI is a command runner built on this process layer. It is deliberately not a full PTY/terminal-emulation implementation yet; interactive TUI applications remain out of scope for the current foundation.

This process layer is also reused by higher-level features such as Live Preview rather than giving each feature its own process lifecycle implementation.

## Live Web Preview

Live Preview has two supported paths:

1. static HTML is served by a TL Studio-owned loopback-only preview server. A root `index.html` remains the default. Without one, TL Studio can use the active HTML file from the Editor, automatically select the only discovered `.html`/`.htm` file, or expose a Browser-side selector when multiple HTML entries exist;
2. supported Node projects with a `package.json` `dev` script run that dev server through the process manager and TL Studio discovers its reported loopback URL.

Static entry discovery stays project-scoped, does not follow symlink entries, and skips `.git` and `node_modules`. The static server still serves the project root so relative assets keep working, while the iframe URL points at the selected HTML entry.

Preview content intentionally runs on a **separate loopback origin** from the TL Studio control origin. Project JavaScript must not share an origin with TL Studio's `/local/*` control APIs.

Only loopback preview URLs are accepted. Static preview file serving remains project-boundary checked and rejects traversal/symlink escapes. The browser Preview window is only a view/controller for that isolated local preview origin.

## Request flow

```text
Browser TL Studio UI
  │
  ├── /local/*  ───────────────► launcher project/files/search/process/preview/tool-registry/permission boundary
  │
  └── /runtime/*
          │
          ▼
      launcher reverse proxy
          │  strips /runtime
          │  injects runtime authentication
          │  injects selected project directory
          ▼
      bundled coding runtime on 127.0.0.1:<backend-port>

Live Preview iframe
  │
  └─────────────────────────────► separate 127.0.0.1:<preview/dev-server-port>
```

## Session and agent surface

TL Studio now owns the semantic **read model** for current sessions. The launcher exposes:

- `GET /local/sessions`
- `GET /local/sessions/status`
- `GET /local/sessions/{sessionID}`
- `GET /local/sessions/{sessionID}/messages`
- `GET /local/sessions/{sessionID}/changes`

The launcher translates the bundled runtime's session records, `info + parts` message envelopes, activity/tool state, usage metadata, status values, and diff metadata into TL Studio concepts. Cross-project session aggregation also lives in the launcher now; the browser no longer needs to know that the current runtime scopes root-session listing by directory.

The current semantic message contract exposes stable product fields such as `role`, `text`, `activities`, `usage`, `changes`, `createdAt`, and `completedAt`. Tool activities carry both the TL Studio semantic Tool Registry ID and the runtime ID for diagnostics. Unknown runtime tools still degrade through the Tool Registry's conservative fallback.

This milestone deliberately owns **reading and presentation semantics**, not session persistence or agent execution. Session creation, rename/delete mutations, prompts, aborts, the agent loop, persisted runtime session storage, and live SSE events still go through the runtime adapter. The runtime remains authoritative for execution and persisted transcript data.

A narrow read-only compatibility bridge remains for sessions created during an older TL Studio alpha protocol window. Legacy parsing is isolated to that compatibility path rather than defining the current session contract.

## Provider and model ownership

Provider/model configuration is the first Phase 2 capability moved behind a TL Studio-owned domain contract.

Custom provider definitions are persisted in TL Studio's own local state as `providers.json` under the same state root used by the launcher. The persisted schema contains TL Studio concepts only: provider ID/name, protocol, base URL, model definitions, tool/reasoning capability flags, and optional context/output limits. Runtime package names and runtime-specific config shapes are not part of this file.

On first use, when no TL Studio provider registry exists yet, the launcher imports compatible custom providers from the current runtime configuration so existing alpha users do not lose supported provider definitions. After that, TL Studio is the source of truth for those managed definitions.

The browser uses only TL Studio routes for this surface:

- `GET /runtime/providers/catalog`
- `GET /runtime/providers/config`
- `PUT /runtime/providers/config/{id}`
- `DELETE /runtime/providers/config/{id}`
- `GET /runtime/hosted/status`
- `POST /runtime/hosted/authorize`
- `POST /runtime/hosted/callback`
- `DELETE /runtime/hosted`

The launcher translates managed definitions to the current engine's provider config internally. Hosted provider IDs and preferred hosted models (including the current Auto Free route) are returned as runtime metadata rather than hard-coded by the browser.

API keys are deliberately excluded from TL Studio's provider registry and browser storage. In this phase, credentials are still delegated to the bundled runtime's local credential store. Moving credential ownership to a TL Studio-controlled secure store is a separate future security milestone.

Session persistence and execution remain in the runtime for now, while TL Studio owns the current session read/presentation contract and tool semantics. Permission request generation and enforcement also still happen in the runtime, but permission policy and remembered approval semantics are owned by TL Studio as described below.

## Tool Registry ownership

TL Studio owns a versioned semantic Tool Registry exposed by the launcher at `GET /local/tools`. Known runtime tool IDs are mapped to TL Studio descriptors containing a stable semantic ID, product-facing name and description, category, capability flags, permission class, and a presentation hint. Browser activity cards resolve tool labels through this registry instead of treating the runtime's raw tool ID as product metadata.

The registry is deliberately descriptive. It does not execute tools, grant permissions, or weaken the runtime's enforcement. Unknown runtime tools fall back to a conservative `runtime.unknown` descriptor with no invented capabilities, and the original runtime ID remains observable for diagnostics. If registry metadata and runtime behavior ever disagree, runtime enforcement is authoritative.

This boundary is intentionally extensible for future runtimes: runtime-specific IDs can change behind the mapping while TL Studio's semantic tool concepts remain stable.

## Permission policy ownership

Permission policy is the second Phase 2 capability moved behind a TL Studio-owned domain contract.

The bundled runtime still generates permission requests at the exact point where a tool requires approval and remains responsible for enforcing the final allow/reject result. TL Studio now owns the policy layer that decides how a human choice is remembered and when a matching non-sensitive request can be approved automatically.

The browser no longer lists or replies to permissions through raw runtime routes. It uses launcher-owned routes:

- `GET /local/permissions?sessionID=...`
- `POST /local/permissions/{requestID}/reply`
- `GET /local/permissions/rules`
- `DELETE /local/permissions/rules/{ruleID}`

When the user chooses **Always allow in this project**, TL Studio stores normalized project-scoped allow rules in its own `permissions.json` file under the local state directory. The launcher translates that explicit choice into a one-time approval for the current runtime request; future matching requests are evaluated by TL Studio before they reach the browser.

Remembered rules are conservative by design:

- they are scoped to the selected project;
- they match the runtime-provided canonical `always` matcher tokens exactly rather than inventing broader wildcard semantics;
- every matcher on a future request must already be covered before automatic approval occurs;
- sensitive `skillShell` and `sandboxEscalation` requests are never remembered or auto-approved;
- requests marked `disableAlways` remain interactive;
- Settings exposes remembered rules and lets the user forget them.

Automatic policy approvals are sent to the runtime as non-interactive one-time approvals. Explicit clicks remain interactive approvals. This preserves the runtime's sensitive-permission enforcement while moving persistence and decision policy into TL Studio.

## Editor asset strategy

TL Studio does not load editor code, fonts, workers, or other runtime assets from a CDN.

TL Studio uses Monaco Editor as the enhanced workspace editor. Monaco is bundled locally at build time together with same-origin workers; no CDN or remote editor assets are used. The editor engine is lazy-loaded on the first file open so the initial application/Agent surface remains lightweight. The original textarea editor remains a functional fallback when the enhanced bundle is unavailable. TL Studio still owns the file-buffer/save/conflict contract; Monaco is a replaceable presentation/editor engine rather than a filesystem authority.

## Local-only security boundary

The public TL Studio UI binds to loopback only. Requests are rejected when the Host is not loopback, and browser requests with an Origin must match the same local control origin. The bundled coding runtime also binds to `127.0.0.1` and is protected with a random per-launch password known only to the launcher.

Project file mutation routes reject paths outside the selected project, project-root mutation, symlink-parent escapes, and protected Git metadata. Direct symlink writes are not treated as editable regular files. Project Search remains rooted at the canonical selected-project path and does not follow symlink entries outside that tree.

Preview execution does not relax this boundary: project web code runs on a separate loopback origin and external preview URLs are not accepted.

## No cloud control plane

There is intentionally no application server belonging to this project. External requests are only those required by services the user explicitly configures, plus normal distribution/update traffic such as GitHub Releases when applicable.


## Runtime abstraction boundary

The browser must not call implementation-specific runtime routes directly. `/runtime/*` is the public local runtime boundary owned by TL Studio. Implementation-specific route names, authentication details, binary discovery, and hosted-provider quirks stay behind the launcher/runtime adapter.

The current engine remains replaceable. New browser features must depend on TL Studio concepts such as sessions, messages, providers, permissions, questions, tools, and events rather than on the bundled engine's product name.

Phase 1 established the runtime independence boundary. Provider/model definitions, tool semantics, permission policy, and the current session read/presentation model are now TL Studio-owned. Session persistence, live-event projection, questions, and the Agent execution loop remain follow-up work behind the same boundary; future work should continue to move semantics inward without exposing engine-specific contracts to the browser.
