# Bundled Runtime Contract — Kilo Code 7.6.2

This document records the **implementation-specific compatibility contract** for the engine currently bundled with TL Studio.

It is intentionally not the public browser/product contract.

Current engine:

- upstream: `Kilo-Org/kilocode`
- pinned version: `v7.6.2`
- pinned version file: `/KILO_VERSION`
- browser runtime adapter source: `cmd/launcher/ui/runtime-api.ts`
- generated browser adapter: `cmd/launcher/web/runtime-api.js`
- launcher provider/hosted translation: `cmd/launcher/runtime_providers.go`

TL Studio owns the browser-facing runtime namespace, provider/model registry, workspace APIs, release packaging, and product identity. Kilo-specific routes, environment variables, authentication, and provider implementation details stay behind that boundary.

## Product boundary

The browser talks to TL Studio through:

- `/local/*` for TL Studio-owned workspace/files/search/process/preview capabilities;
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

- `GET /runtime/session?directory=...`
- `POST /runtime/session?directory=...`
- `GET /runtime/session/status?directory=...`
- `GET /runtime/session/:sessionID?directory=...`
- `GET /runtime/session/:sessionID/message?directory=...`
- `POST /runtime/session/:sessionID/prompt_async?directory=...`
- `POST /runtime/session/:sessionID/abort?directory=...`
- `GET /runtime/session/:sessionID/diff?directory=...`

The pinned engine returns product messages in its current `info + parts` envelope. The browser adapter normalizes and presents that data through TL Studio UI concepts.

### Events

- `GET /runtime/global/event?directory=...` (SSE)

TL Studio uses events for responsive updates and projected runtime state, while persisted/current session messages remain the reconnect-safe rendering source of truth.

### Permissions

- `GET /runtime/permission?directory=...`
- `POST /runtime/permission/:requestID/reply?directory=...`

### Questions

- `GET /runtime/question?directory=...`
- `POST /runtime/question/:requestID/reply?directory=...`
- `POST /runtime/question/:requestID/reject?directory=...`

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

Credentials are intentionally excluded from `providers.json` and browser storage. Credential ownership is still delegated to the current engine's local credential store and can move later as a separate security milestone.

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

Generated browser JavaScript lives in:

`cmd/launcher/web/runtime-api.js`

UI modules must use this TL Studio adapter and must not construct engine-specific routes directly.

The adapter owns browser-side runtime concerns such as:

- TL Studio `/runtime/*` route construction
- selected-project routing
- response normalization
- session/message access
- model selection representation
- permission/question access
- live-event handling

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
