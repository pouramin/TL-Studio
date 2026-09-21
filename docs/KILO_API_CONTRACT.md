# Bundled Runtime Contract — Kilo Code 7.6.2

This document records the **implementation-specific compatibility contract** for the engine currently bundled with TL Studio.

It is intentionally not the public browser/product contract.

Current engine:

- upstream: `Kilo-Org/kilocode`
- pinned version: `v7.6.2`
- pinned version file: `/KILO_VERSION`
- browser runtime adapter source: `cmd/launcher/ui/runtime-api.ts`
- bundled Browser output: `cmd/launcher/web/browser.js`
- launcher provider/hosted translation: `cmd/launcher/runtime_providers.go`

TL Studio owns the browser-facing runtime namespace, provider/model registry, workspace APIs, release packaging, and product identity. Kilo-specific routes, environment variables, authentication, and provider implementation details stay behind that boundary.

## Product boundary

The browser talks to TL Studio through:

- `/local/*` for TL Studio-owned workspace/files/search/process/preview/tool-registry/permission-policy capabilities;
- `/runtime/*` for agent-runtime capabilities.

The browser must not construct Kilo-specific route prefixes or depend on Kilo-specific authentication details.

For generic runtime product routes, the launcher reverse proxy strips the `/runtime` prefix, injects runtime authentication, and injects the selected project directory before forwarding to the bundled engine.

For TL Studio-owned semantic provider routes, the launcher handles translation itself.

## Current engine routing

Kilo v7.6.2 expects project-scoped requests to carry the selected directory. TL Studio forwards the selected project as the runtime query directory and injects the current engine's `x-kilo-directory` header at the private proxy boundary.

That header is an engine implementation detail. Browser modules must never set or depend on it.

The launcher also starts the current engine with its required local server environment and a random per-launch credential. Those engine-specific environment variables remain server-side.

## Runtime product routes currently exercised

The browser reaches these through the TL Studio `/runtime/*` namespace:

### Health and project routing

- `GET /runtime/global/health`
- `GET /runtime/path?directory=...`

### Agents

- `GET /runtime/agent?directory=...`

The current engine's product layer exposes the visible `code` agent rather than the lower-level raw `build` agent. CI treats that behavior as part of the pinned-engine compatibility contract.

### Sessions and messages

The current engine implementation still provides these private session routes behind the launcher adapter:

- `GET /session?directory=...`
- `POST /session?directory=...`
- `GET /session/status?directory=...`
- `GET /session/:sessionID?directory=...`
- `PATCH /session/:sessionID?directory=...`
- `DELETE /session/:sessionID?directory=...`
- `GET /session/:sessionID/message?directory=...`
- `POST /session/:sessionID/prompt_async?directory=...`
- `POST /session/:sessionID/abort?directory=...`
- `GET /session/:sessionID/diff?directory=...`

The Browser no longer uses the mutation/execution routes above directly. TL Studio's launcher-owned session command contract exposes create, update, delete, run, and abort through `/local/sessions*`, while `runtime_kilo_sessions.go` owns translation to these Kilo-specific routes.

The pinned engine returns product messages in its current `info + parts` envelope. That envelope is implementation-only for current sessions: the launcher projects it into TL Studio's `/local/sessions*` semantic read contract before the browser consumes it.

### TL Studio session read projection

Normal Browser reads no longer consume the engine's session/message shapes directly. The launcher maps the implementation routes above into:

- `GET /local/sessions`
- `GET /local/sessions/status`
- `GET /local/sessions/{sessionID}`
- `GET /local/sessions/{sessionID}/messages`
- `GET /local/sessions/{sessionID}/changes`

The projection owns cross-project aggregation, flat session timestamps, semantic message roles/text, activity classification, usage, normalized status, and Changes fallback. Runtime-specific `info`, `parts`, `busy`, and `retry` shapes therefore stay on this compatibility side of the boundary.

TL Studio owns the Browser-facing mutation/run/abort semantics and now persists its own semantic session snapshots. Kilo still maintains its execution-side session state, executes models and tools, emits execution events, and runs the agent loop. Launcher reads refresh the TL Studio snapshot; if Kilo later loses a historical session, TL Studio can still present, rename, and delete its persisted semantic history. Resuming execution still requires a live runtime execution binding.

### Events

Implementation compatibility still consumes:

- `GET /runtime/global/event?directory=...` (SSE)

The Browser no longer consumes this engine-specific stream directly. The launcher projects it to:

- `GET /local/events` (SSE)

Current runtime event names and nested `properties` envelopes are therefore implementation details. TL Studio maps relevant runtime events into `stream.ready`, `session.changed`, `message.changed`, `attention.changed`, and `workspace.changed`. Persisted/current semantic session reads remain the reconnect-safe rendering source of truth.

### Permissions

The current engine still exposes and enforces:

- `GET /runtime/permission?directory=...`
- `POST /runtime/permission/:requestID/reply?directory=...`

Those routes are now an implementation compatibility surface. Normal browser permission UI uses TL Studio's launcher-owned `/local/permissions*` contract. The launcher reads current engine requests, applies TL Studio's project-scoped policy, and translates explicit or remembered decisions back to the engine as one-time replies.

### Tool Registry

Tool identity and product-facing metadata are no longer inferred directly in the browser. The launcher exposes `GET /local/tools`, which maps known Kilo 7.6.2 runtime IDs such as `read`, `write`, `edit`, `apply_patch`, `bash`, `webfetch`, and `websearch` to TL Studio semantic descriptors.

This registry does not replace Kilo's execution engine. Tool parts still arrive through runtime session messages and Kilo still executes the underlying tool. Unknown IDs use TL Studio's conservative runtime-controlled fallback. Permission decisions continue through the separate TL Studio permission-policy boundary and final runtime enforcement.

### Questions

The current engine implementation still provides these private compatibility routes:

- `GET /question?directory=...`
- `POST /question/:requestID/reply?directory=...`
- `POST /question/:requestID/reject?directory=...`

Normal Browser code no longer calls those routes through `/runtime/*`. TL Studio exposes semantic question operations through:

- `GET /local/questions?sessionID=...`
- `POST /local/questions/:requestID/reply`
- `POST /local/questions/:requestID/reject`

The Kilo-specific route mapping is isolated in `runtime_kilo_questions.go`.

## Provider and model ownership

Provider/model definitions are no longer owned by browser code or stored as engine-specific configuration in the TL Studio UI.

TL Studio owns a local `providers.json` registry with product concepts such as:

- provider ID and display name
- protocol
- base URL
- model definitions
- tool-calling capability
- reasoning capability
- context/output limits

The browser uses TL Studio semantic routes:

- `GET /runtime/providers/catalog`
- `GET /runtime/providers/config`
- `PUT /runtime/providers/config/{id}`
- `DELETE /runtime/providers/config/{id}`

The launcher translates those definitions to the currently bundled engine internally. Current engine package identifiers such as `@ai-sdk/*`, overlay config shapes, and auth routes are not part of the browser contract.

Credentials remain intentionally excluded from `providers.json` and browser storage. Custom-provider API keys are now owned by TL Studio's credential vault and synchronized into Kilo's auth store only as an execution copy. Kilo's existing pre-migration secrets are not reverse-readable, so a legacy provider keeps working through the runtime store until the user saves a credential through TL Studio. Hosted Kilo OAuth remains an engine-specific account integration behind the hosted-provider adapter rather than part of the custom-provider API-key vault.

## Hosted provider mapping

The browser uses TL Studio semantic hosted-provider routes:

- `GET /runtime/hosted/status`
- `POST /runtime/hosted/authorize`
- `POST /runtime/hosted/callback`
- `DELETE /runtime/hosted`

The launcher currently maps those calls to Kilo's hosted-provider/auth implementation.

The current bundled engine exposes its hosted provider as `kilo` and currently advertises `kilo-auto/free` as the preferred Auto Free model. These IDs are returned to the browser as runtime metadata rather than hard-coded in browser modules.

A future engine replacement must be able to change this mapping without redesigning the product UI.

## Browser adapter policy

Authored browser runtime code lives in:

`cmd/launcher/ui/runtime-api.ts`

The Browser module graph is bundled into:

`cmd/launcher/web/browser.js`

UI modules must use TL Studio-owned semantic contracts and must not construct engine-specific session mutation routes directly.

The adapter owns browser-side runtime concerns such as:

- TL Studio `/runtime/*` route construction
- selected-project routing
- response normalization
- session/message access
- model selection representation
- semantic question access through launcher-owned `/local/questions*` routes

Permission policy is intentionally not a browser-to-engine adapter concern anymore; it is owned by the launcher-side TL Studio permission engine.
- tool presentation resolves through the launcher-owned `/local/tools` semantic registry rather than raw runtime labels
- raw live-event compatibility and launcher-side semantic projection

Engine-specific provider/config/auth translation belongs in the launcher, not in browser modules.

## Automated compatibility gates

The test filenames still use `check-kilo-*` because they validate the exact currently bundled Kilo engine. Their scope is implementation compatibility, not product identity.

### Runtime product contract

`scripts/check-kilo-product-contract.py`

CI starts the pinned real engine through the TL Studio launcher and verifies health, selected-project routing, agent behavior, provider/session/message shapes, permissions/questions, and the event stream through TL Studio's public `/runtime/*` boundary.

### Project routing

`scripts/check-kilo-project-routing.py`

This verifies that changing the selected TL Studio project changes the directory used by the current engine rather than silently falling back to the launcher process CWD.

### Prompt + real write-tool E2E

`scripts/check-kilo-prompt-e2e.py`

CI starts a local fake OpenAI-compatible provider and exercises:

```text
TL Studio launcher
→ bundled Kilo 7.6.2 engine
→ TL Studio /runtime boundary
→ product agent/session flow
→ local test provider
→ real write tool
→ TL Studio /local/permissions policy boundary
→ edit permission
→ hello.txt inside the selected project
→ final assistant response
```

The fixture asserts:

```text
hello.txt == TL_STUDIO_E2E_OK
```

and:

```text
E2E_PRODUCT_OK
```

A release must not be cut if this real-engine product-path E2E fails.

## Experimental Protocol v2

Kilo v7.6.2 exposes an experimental `/api/*` Protocol v2 surface in addition to the product/current HTTP API used by its official client.

TL Studio's current stable coding path does not use that experimental surface as its primary session model. Any future engine/API migration must be version-pinned and validated against the real bundled engine and TL Studio's product E2E before release.

## Engine upgrade checklist

Before changing `KILO_VERSION`:

1. inspect the new upstream release and generated SDK/client routes;
2. inspect the upstream product client to determine which routes it actually uses;
3. compare agent, provider, session, message/part, permission, question, event, and tool behavior;
4. update only the launcher/runtime adapter and this implementation-specific contract where possible;
5. keep browser modules on TL Studio-owned `/runtime/*` and semantic provider/hosted routes;
6. run the real runtime contract, project-routing, provider contract, and Prompt/write-tool E2E tests;
7. perform hands-on Windows validation before promotion.

## Attribution

The current release bundle includes Kilo Code under its MIT license. Required attribution and the upstream license remain in:

- `THIRD_PARTY_NOTICES.md`
- `third_party/KILO_LICENSE.txt`

Reducing product coupling does not remove or weaken required third-party attribution.
