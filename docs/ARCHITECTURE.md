# Architecture

## Components

### Launcher

A single Go binary using only the Go standard library. It owns the lifecycle of the bundled coding runtime, chooses ephemeral ports, serves embedded static UI assets, exposes project-scoped local workspace APIs, and reverse-proxies the runtime API behind TL Studio's local product boundary.

The launcher is also the filesystem trust boundary for browser IDE operations. Browser requests never receive arbitrary host filesystem access: file reads and mutations are resolved relative to the selected project, traversal and symlink escapes are rejected, and workspace mutations protect Git metadata.

### Bundled coding runtime

The current implementation starts the bundled runtime on loopback with a random per-launch password. The runtime remains responsible for active agent execution, actual tool execution, provider execution, model inference, and filesystem operations initiated by the agent. TL Studio owns semantic session persistence/history, product-facing session commands, interactive-question semantics, custom-provider credentials, and the semantic identity/presentation metadata for known tools.

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
- `GET /runtime/hosted/status`
- `POST /runtime/hosted/authorize`
- `POST /runtime/hosted/callback`
- `DELETE /runtime/hosted`

The launcher translates managed definitions to the current engine's provider config internally. Hosted provider IDs and preferred hosted models (including the current Auto Free route) are returned as runtime metadata rather than hard-coded by the browser.

API keys are deliberately excluded from TL Studio's provider registry and browser storage. TL Studio now owns custom-provider credentials in a separate local credential vault. Windows uses user-scoped DPAPI; macOS uses Keychain; Linux uses Secret Service when available; environments without a usable keyring use an AES-GCM encrypted private-file fallback with a separate `0600` local master key. The launcher restores owned credentials into the active runtime's execution store when needed. Existing legacy runtime-only credentials cannot be reverse-read or silently imported because the engine does not expose their plaintext; saving that provider again moves the credential under TL Studio ownership.

Semantic session persistence is now TL Studio-owned. The runtime remains the active execution binding for resumable Agent work, while TL Studio retains its own semantic history independently. Permission request generation and enforcement still happen in the runtime, but permission policy and remembered approval semantics are owned by TL Studio as described below.

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
