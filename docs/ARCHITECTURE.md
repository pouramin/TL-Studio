# Architecture

## Components

### Launcher

A single Go binary using only the Go standard library. It owns the lifecycle of the bundled coding runtime, chooses ephemeral ports, serves embedded static UI assets, exposes project-scoped local workspace APIs, and reverse-proxies the runtime API behind TL Studio's local product boundary.

The launcher is also the filesystem trust boundary for browser IDE operations. Browser requests never receive arbitrary host filesystem access: file reads and mutations are resolved relative to the selected project, traversal and symlink escapes are rejected, and workspace mutations protect Git metadata.

### Bundled coding runtime

The current implementation still starts the bundled runtime on loopback with a random per-launch password, but it is a compatibility engine rather than the owner of every Agent run. Supported TL Studio-managed custom providers use the TL Studio-native Agent loop and native Tool Executor. The bundled runtime remains responsible for hosted Kilo execution and compatibility-only capabilities that have not yet moved behind native handlers.

The runtime is behind TL Studio's product boundary. The browser never connects to it directly and never receives its server password.

Implementation-specific API compatibility details are documented in [`KILO_API_CONTRACT.md`](./KILO_API_CONTRACT.md).

### Browser UI

Static HTML/CSS and generated Browser assets are embedded in the launcher at build time. Browser application source is maintained in TypeScript under `cmd/launcher/ui`; `browser.ts` is the ordered ES-module entry point and esbuild emits the single `cmd/launcher/web/browser.js` product bundle loaded by `index.html`. Generated Browser JavaScript is intentionally not tracked in Git. `kernel.ts` owns and exports the shared typed Browser kernel, and feature modules import that kernel directly instead of reading mutable state from `window.KLU`. The legacy per-module JavaScript emit has been removed; Browser regressions validate the TypeScript source/module graph directly. The complete Browser TypeScript surface is checked with `strict: true`; `noCheck` is not used. Shared declarations in `global.d.ts` define the `TLStudioKernel`, application state, required DOM elements, runtime/session/tool/live-event contracts, and extension-owned state. The normal TL Studio control surface talks only to the launcher origin.

The browser IDE keeps one in-memory buffer per open editor tab. User-initiated reads, saves, creates, renames, and deletes go through launcher-owned `/local/*` routes. **Show in Folder** uses the launcher-owned `POST /local/reveal` route; the launcher resolves the requested path through the same project boundary before invoking the native file manager (`explorer.exe /select` on Windows, `open -R` on macOS, parent-folder `xdg-open` on Linux). File previews include a SHA-256 revision token; normal saves send that token back so an external edit by the agent, Git, or another process cannot be silently overwritten.

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

Live Preview is now capability-driven rather than HTML-specific. The launcher owns a versioned Preview Capability Registry exposed at:

- `GET /local/preview/capabilities`

The registry maps supported file extensions to TL Studio preview concepts. Current capabilities are:

- HTML: `.html`, `.htm`
- SVG
- raster/browser-native images: `.png`, `.jpg`, `.jpeg`, `.webp`, `.gif`, `.avif`, `.ico`, `.bmp`, `.apng`
- PDF
- video: `.mp4`, `.webm`, `.ogv`, `.m4v`
- audio: `.mp3`, `.wav`, `.ogg`, `.oga`, `.m4a`, `.aac`, `.flac`
- Markdown: `.md`, `.markdown`, `.mdown`
- plain text: `.txt`, `.text`, `.log`

Browser-native image/video/audio previews are served by the same TL Studio-owned loopback-only file preview server. Markdown and plain text use TL Studio-owned safe HTML renderers on that preview origin; raw text/HTML is escaped before presentation. PDF is served directly from a dedicated TL Studio preview route with `Content-Type: application/pdf`, `Content-Disposition: inline`, and byte-range support so the browser's native PDF viewer receives the document as the top-level content of the Preview iframe.

The Browser does not hard-code preview extensions. It loads the Preview Capability Registry and uses it to decide whether the active Workspace tab can replace the current Preview entry. Text-editable preview types such as HTML, SVG, Markdown, and plain text stay normal editor tabs. Binary media capabilities such as raster images, PDF, video, and audio open as read-only Workspace tabs: they carry path/type metadata, publish the same active-tab event, and never create a Monaco text model. While Preview is open, activating any previewable tab automatically switches to that file. Activating a non-previewable code file leaves the current Preview unchanged.

Node projects keep project-aware behavior: when the active previewable file is HTML and a `package.json` `dev` script exists, TL Studio runs the project's dev server instead of serving the raw HTML file. Activating a standalone previewable asset such as an image, PDF, audio/video file, or Markdown document temporarily switches to file Preview; returning to HTML restores the dev-server path.

File discovery stays project-scoped, does not follow symlink entries, skips `.git` and `node_modules`, and is bounded. Large preview-only binary files are not loaded into the Workspace text-buffer path; only metadata is returned while the isolated preview server streams/serves the actual file. Switching between file previews reuses the same loopback server whenever possible. The floating Preview window persists its geometry and exposes custom resize handles on all four edges and all four corners for side-by-side editing.

Preview content intentionally runs on a **separate loopback origin** from the TL Studio control origin. Project content must not share an origin with TL Studio's `/local/*` control APIs.

Only loopback preview URLs are accepted. File serving remains project-boundary checked and rejects traversal/symlink escapes. The Browser Preview window is only a view/controller for that isolated local preview origin.

## Request flow

```text
Browser TL Studio UI
  │
  ├── /local/*  ───────────────► launcher project/files/search/process/preview/plugins/tool-registry/permission boundary
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

TL Studio now owns both the Browser-facing **read model** and the Browser-facing **session command semantics**. The launcher exposes semantic commands for create, rename/update, delete, run/prompt, and abort:

- `POST /local/sessions`
- `PATCH /local/sessions/{sessionID}`
- `DELETE /local/sessions/{sessionID}`
- `POST /local/sessions/{sessionID}/runs`
- `POST /local/sessions/{sessionID}/abort`

The generic command contract does not contain engine-specific route names. The active runtime adapter translates these operations to its private implementation API. TL Studio now mirrors semantic session metadata, transcripts, activity/usage history, and changes into its own local persistence. Persisted history remains readable if the runtime loses its copy, and persisted-only sessions remain locally renameable/deletable. Model execution, tool execution, and the Agent loop still remain in the runtime. Live runtime events are consumed by the launcher and projected through TL Studio's semantic SSE boundary described below.

A narrow read-only compatibility bridge remains for sessions created during an older TL Studio alpha protocol window. Legacy parsing is isolated to that compatibility path rather than defining the current session contract.

## Live event projection ownership

TL Studio owns the Browser-facing live-event contract at:

- `GET /local/events` (SSE)

The launcher maintains the authenticated connection to the bundled runtime's implementation event stream and projects engine-specific events into a small, versioned semantic vocabulary:

- `stream.ready`
- `session.changed`
- `message.changed`
- `attention.changed`
- `workspace.changed`

Projected events expose only stable product metadata such as `sessionID`, `messageID`, `attentionKind`, `path`, and a normalized action. Raw runtime envelopes such as `properties`, `message.part.updated`, `session.status`, or `permission.asked` do not cross into Browser code.

The event stream is intentionally a responsiveness signal rather than transcript authority. After a semantic event, Browser modules refresh the launcher-owned Session/Permission contracts; persisted semantic session reads remain the reconnect-safe source of truth. Unknown runtime event types are ignored instead of being surfaced as accidental product API.

The runtime remains the source of execution events. TL Studio owns their translation and Browser-facing meaning.


## Interactive question ownership

Interactive Agent questions now use a TL Studio-owned semantic contract:

- `GET /local/questions?sessionID=...`
- `POST /local/questions/{requestID}/reply`
- `POST /local/questions/{requestID}/reject`

The launcher validates session/request association, normalizes question headers, prompts, options, multiple-choice/custom-answer behavior, and answer payloads. The active engine adapter translates those semantic operations to the engine's private question API. Raw question routes and engine envelopes do not cross into Browser code.

The runtime still decides *when* an executing Agent needs a question and blocks execution while waiting. TL Studio owns the product-facing question shape and reply/reject semantics.

## Provider and model ownership

Provider/model configuration is the first Phase 2 capability moved behind a TL Studio-owned domain contract.

Custom provider definitions are persisted in TL Studio's own local state as `providers.json` under the same state root used by the launcher. The persisted schema contains TL Studio concepts only: provider ID/name, protocol, base URL, model definitions, tool/reasoning capability flags, and optional context/output limits. Runtime package names and runtime-specific config shapes are not part of this file.

On first use, when no TL Studio provider registry exists yet, the launcher imports compatible custom providers from the current runtime configuration so existing alpha users do not lose supported provider definitions. After that, TL Studio is the source of truth for those managed definitions.

The browser uses only TL Studio routes for this surface:

- `GET /runtime/providers/catalog`
- `GET /runtime/providers/config`
- `PUT /runtime/providers/config/{id}`
- `DELETE /runtime/providers/config/{id}`
- `POST /runtime/providers/discover`
- `GET /runtime/hosted/status`
- `POST /runtime/hosted/authorize`
- `POST /runtime/hosted/callback`
- `DELETE /runtime/hosted`

The launcher translates managed definitions to the current engine's provider config internally. Hosted provider IDs and preferred hosted models (including the current Auto Free route) are returned as runtime metadata rather than hard-coded by the browser.

Model discovery is a TL Studio-owned edge service, not a Kilo capability. A draft provider can call `POST /runtime/providers/discover` before it has been saved. OpenAI-compatible and OpenAI Responses protocols first use the generic `<baseURL>/models` contract; Anthropic Messages uses a provider-specific model-list adapter with pagination and Anthropic authentication headers. Both paths normalize results into one discovered-model shape. Unknown tool/reasoning/vision capabilities stay unknown during discovery rather than being invented from model names.

Discovered catalogs and configured models are deliberately separate concepts. The Browser can search a large discovered catalog and select only the models that should enter `providers.json`. Manual model IDs remain available for providers with no listing endpoint or private/unlisted models. For saved providers, TL Studio keeps a private last-good model-catalog cache with no credentials. Transient discovery failures may fall back to that cache with a stale warning; authentication failures remain explicit. Configured models that disappear from a later provider response are shown as unavailable instead of being silently deleted.

API keys are deliberately excluded from TL Studio's provider registry and browser storage. TL Studio now owns custom-provider credentials in a separate local credential vault. Windows uses user-scoped DPAPI; macOS uses Keychain; Linux uses Secret Service when available; environments without a usable keyring use an AES-GCM encrypted private-file fallback with a separate `0600` local master key. The launcher restores owned credentials into the active runtime's execution store when needed. Existing legacy runtime-only credentials cannot be reverse-read or silently imported because the engine does not expose their plaintext; saving that provider again moves the credential under TL Studio ownership.

### Jev Router and Decision Engine

TypeSafe Jev is integrated at two different architectural boundaries.

**Jev Router** is a normal generative model from TL Studio's point of view. The model ID is `typesafe/jev-router` and the provider connection is the normal OpenRouter-compatible endpoint at `https://openrouter.ai/api/v1`. TL Studio does not introduce an OpenRouter-specific Agent implementation. An existing OpenRouter provider definition and credential are reused when present; otherwise the standard provider form is used. The discovered model is tagged with `kind: "router"`, but its underlying routed models are never hard-coded.

The native OpenAI-compatible client preserves its existing streaming/messages/tools contract. It additionally records the provider-returned top-level model identifier when available. Router models can project that value as semantic `kind: "model"` activity. This is observability only; no routed model is inferred when the provider does not return one.

Direct Jev System One models are not chat models and therefore do not enter `nativeModelClient`. TL Studio owns a small provider-independent `decisionEngine` interface with a Jev/OpenRouter implementation behind:

- `GET /local/decision-engine`
- `PUT /local/decision-engine`
- `POST /local/decision-engine/evaluate`

The persisted Decision Engine setting defaults to `off`. Enabling Jev is explicit and reuses the API key from an existing official OpenRouter provider. The Jev implementation targets the OpenRouter Decisions API and uses `~typesafe/jev-latest` by default. This direct path is paid and is never invoked merely because the user selected Jev Router. The current product has no automatic Decision Engine hooks in model routing, tool routing, permission decisions, loop continuation, or output verification.

Decision answers normalize Choice, Score, and Noul results plus probabilities/confidence/usage. They are probabilistic signals, not security facts. The existing deterministic Permission Engine remains the authoritative boundary and is intentionally not replaced or bypassed by Jev.

Semantic session persistence is now TL Studio-owned. The runtime remains the active execution binding for resumable Agent work, while TL Studio retains its own semantic history independently. Permission request generation and enforcement still happen in the runtime, but permission policy and remembered approval semantics are owned by TL Studio as described below.

## Plugin and MCP ownership

TL Studio owns a generic Plugin domain at `/local/plugins*`. The first plugin type is `mcp`, and the first transport is `stdio`. Plugin definitions are product data rather than Agent/runtime-specific configuration: ID, display metadata, enabled state, scope, transport, command, argument vector, project-relative working directory, environment-variable names, and optional metadata.

Project-scoped definitions are matched only to their configured project. Global scope is represented in the schema so a user can intentionally expose the same plugin across projects. Saving a new plugin does **not** execute it. Starting a configured local MCP command requires a separate explicit enable action, and Graphify build actions require a separate explicit confirmation.

Secret environment values are not written to `plugins.json`, browser storage, session history, or normal API responses. Only environment-variable names and configured-state metadata are persisted with the plugin. Values use the existing TL Studio credential infrastructure: DPAPI on Windows, Keychain on macOS, Secret Service on Linux when available, or the encrypted private-file fallback.

The MCP Client Manager is transport-isolated behind an internal client interface. The initial stdio implementation:

1. starts the configured executable directly with an argument vector rather than shell-concatenating user input;
2. performs MCP `initialize` and `notifications/initialized`;
3. discovers `tools/list` and optional `resources/list`;
4. converts discovered tool schemas to native model-tool definitions;
5. namespaces tool IDs as `mcp.<plugin-id>.<tool-name>`;
6. invokes `tools/call` and returns structured results to the native Agent loop;
7. propagates timeout/cancellation and sends the MCP cancellation notification best-effort;
8. observes process exit, reports useful errors, and reconnects once after a transport/process failure;
9. terminates the plugin process on disable, removal, project switch where relevant, and TL Studio shutdown.

The Plugin Manager depends on that client interface rather than on stdio details. A future Streamable HTTP MCP transport can therefore implement the same client contract without changing the native Agent loop, Tool Registry, or Browser plugin model.

Discovered MCP tools are merged into the existing TL Studio Tool Registry. MCP annotations are used when available to classify tools as read-like, write-like, or open-world/network-capable; conservative name-based classification is used only as a fallback. Unclassified tools use a safer `unknown` permission class with execution-sensitive behavior. Every MCP tool invocation still passes through the existing native permission authorizer before the MCP server receives the call.

The native Agent remains the decision maker. Enabling an MCP plugin only adds its discovered tool definitions to the same model request that already contains TL Studio's built-in tools. No prompt router forces code questions through a plugin, which keeps plugin OFF/ON a clean benchmarking boundary.

### Graphify validation integration

Graphify validates the generic path rather than defining it. TL Studio does not hardcode Graphify's MCP tool inventory; `graphify-mcp` is initialized like any other MCP server and its actual tools are discovered at runtime.

Graphify-specific convenience behavior is kept at the product edge:

- detect whether `graphify` and the configured MCP executable are available;
- detect `graphify-out/graph.json`, `graphify-out/graph.html`, and `graphify-out/GRAPH_REPORT.md`;
- run the fixed local graph-build command `graphify extract . --code-only` through the existing project process manager after explicit confirmation;
- reopen/reconnect its MCP client after a graph rebuild;
- open the generated interactive HTML using the existing TL Studio Preview surface.

These conveniences are not part of the generic Plugin core and do not change how the Agent executes MCP tools.

### Bundled plugins

The generic Plugin domain now has two origins:

- `bundled`: version-pinned MCP sidecar executables shipped inside the TL Studio release package;
- `user`: externally installed/configured MCP commands supplied by the user.

Origin only affects configuration ownership and executable resolution. Once a process starts, both origins use the same MCP Client Manager, dynamic `tools/list`, Tool Registry, Permission Engine, Native Tool Executor, and Native Agent path. Bundled tools do not receive an Agent bypass or a privileged tool-execution path.

Bundled plugin metadata is loaded from the versioned embedded `cmd/launcher/bundled_plugins.json` manifest. User plugin metadata cannot claim reserved bundled origin/version/license fields or shadow a bundled plugin ID. Bundled enable/disable state is stored separately from user `plugins.json`; bundled plugins cannot be removed through Settings.

Release archives resolve bundled executables from:

```text
plugins/<plugin-id>/bin/<executable>
```

The release staging script validates that each third-party bundled plugin declares every TL Studio-supported platform, HTTPS artifact URLs, exact SHA-256 checksums, and a repository-retained license before packaging. The same staging path is used by stable release and Windows Preview Build workflows. The current alpha manifest is intentionally empty until a real candidate passes those packaging/security gates.

Bundled plugin child processes inherit a reduced ordinary OS/runtime environment rather than the launcher's entire environment, limiting accidental exposure of unrelated user secrets. This is defense in depth only: a bundled executable is still part of TL Studio's trusted computing base and is not OS-sandboxed by the Permission Engine.

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

The current engine remains replaceable. The launcher now expresses the active backend through a TL Studio-owned `runtimeEngine` interface plus a shared `runtimeBackend`. Generic launcher, provider, session, permission, and live-event code delegates binary discovery, subprocess construction, authentication, project request scoping, and request decoration to the selected engine adapter. Kilo-specific lifecycle and `x-kilo-directory` behavior are isolated in `runtime_kilo.go`; the current default adapter remains Kilo Code 7.6.2. New browser features must depend on TL Studio concepts such as sessions, messages, providers, permissions, questions, tools, and events rather than on the bundled engine's product name.

Phase 2 ownership is complete for the supported native coding path. TL Studio owns provider/model definitions, custom-provider credentials, tool semantics and executable handlers, permission policy/enforcement, session read/commands/persistence, interactive-question semantics, Browser-facing live events, direct model clients, and the model/tool/model Agent loop. The native tool executor reuses TL Studio's existing file, search, and process subsystems and rejects unknown tools, path escapes, unsafe remembered permissions, and unbounded execution.\n\nSupported custom providers use TL Studio native execution by default. The bundled Kilo adapter remains a compatibility path for hosted Kilo authentication/models and capabilities that are not yet implemented by the native executor. Generic product code must continue to use capability/product boundaries rather than Kilo-specific routes.


## Native Agent runtime

The native coding path is owned by TL Studio and follows this execution flow:

```text
semantic session
→ resolve TL Studio provider/model + credential
→ direct provider request
→ normalized streamed model response
→ TL Studio tool call
→ TL Studio permission policy / user approval
→ TL Studio Tool Executor
→ semantic tool result
→ next provider request
→ final assistant response
→ TL Studio session persistence + live events
```

The initial native executable set covers project file read/list/write/edit, project content search, and project-scoped terminal commands. File operations reuse the existing project-boundary and symlink protections. Terminal execution reuses the local process manager and propagates timeout/cancellation to process termination.

Explicit guards cap Agent iterations, consecutive tool rounds, tools per round, and repeated identical calls. Abort cancels model requests and tool/process execution, and native run state is projected through the existing semantic session status/event contracts.

Direct provider execution is isolated behind the native model interface. Current supported protocols are OpenAI-compatible Chat Completions, OpenAI Responses, and Anthropic Messages. Hosted Kilo authentication remains engine-specific and therefore stays behind the Kilo compatibility adapter.
