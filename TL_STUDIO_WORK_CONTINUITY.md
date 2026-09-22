# TL Studio Work Continuity

This file is a durable operating instruction for future TL Studio development sessions.

## Source of truth

- Repository: `https://github.com/pouramin/TL-Studio`
- The repository state is the final source of truth.
- Before continuing substantial work, inspect current `dev` and reconcile this file, `README.md`, `README.fa_IR.md`, `docs/ARCHITECTURE.md`, `docs/KILO_API_CONTRACT.md`, and `VERSION` with the actual code.
- Do not restart or redesign the project from scratch.

## Branch and release discipline

- `main` is stable production only and is promoted to the stable `v0.3.0` line after Phase 2 validation.
- `dev` is the next private alpha line and currently starts from `0.4.0-alpha.2`.
- Feature/fix branches start from `dev`.
- Experimental work must not be merged into `main`.
- Private alpha builds use the GitHub Actions Preview Build artifact flow.
- Stable releases are promoted only after automated gates and hands-on validation.

## Current development state

Current stable baseline after Phase 2 promotion:

`0.3.0`

Current private development line:

`0.4.0-alpha.1`

The next private milestone is the generic Plugin/MCP architecture. It is being implemented on:

`feature/plugin-mcp`

Merged PR:

`#93 — Add generic Plugin and MCP architecture`

Squash merge commit:

`f69b3c49288558ee42b6adb5cd468ffcefe3e9c0`

Graphify is the first real integration used to validate the architecture, but the Plugin core is intentionally configuration-driven and supports arbitrary user-added stdio MCP servers without a new Agent integration.

Phase 2 native execution milestone was squash-merged through PR:

`#90 — Phase 2: own Agent loop and tool execution`

Phase 2 merge commit:

`1c3fcd05280e3e80d03a1b949f81ae627820b6fd`

Phase 2 was promoted only after the final Preview Build passed and the Windows preview was hands-on validated. Re-read current `main`, `dev`, Release, and CI state from GitHub when resuming.

## Phase 2 ownership — complete

TL Studio now owns the product-facing semantics and supported native execution path for:

- Browser IDE/workspace
- Monaco editor
- file management
- project search
- local terminal/process manager
- preview
- provider/model definitions
- custom provider registry
- custom provider credential vault
- Tool Registry semantic metadata
- executable native core coding tools
- permission policy and native permission enforcement
- session read model
- session create/update/delete/run/abort semantics
- semantic session persistence
- explicit session execution ownership (`native` vs `compatibility`)
- interactive question semantics
- semantic live-event projection
- direct model execution for supported custom providers
- the TL Studio-native model/tool/model Agent loop
- cancellation and loop guards
- project/session presentation and recovery UX

The Browser remains on TL Studio semantic contracts and does not depend directly on raw Kilo Agent/tool/question mutation routes.

## Native Agent runtime

Supported TL Studio-managed custom providers run through:

```text
Browser
→ TL Studio semantic session command
→ TL Studio native Agent loop
→ TL Studio direct model client
→ normalized model/tool call
→ TL Studio permission policy
→ TL Studio Tool Executor
→ TL Studio-owned local subsystem
→ semantic tool result
→ next model turn
→ final assistant response
→ TL Studio persistence + live events
```

Current direct provider protocols:

- OpenAI-compatible Chat Completions
- OpenAI Responses
- Anthropic Messages compatible

Current native executable coding tools:

- `files.read`
- `files.list`
- `files.write`
- `files.edit`
- `search.content`
- `terminal.command`

These reuse existing TL Studio project-boundary, symlink, search, process, cancellation, and permission infrastructure.

Unknown native tools are rejected.

## Kilo compatibility path

Pinned Kilo version remains:

`7.6.2`

Kilo remains bundled as the currently tested third-party compatibility engine.

It is still used for:

- hosted Kilo authentication/models
- legacy/runtime-configured providers that are not resolvable from the TL Studio provider registry + credential vault
- runtime-only capabilities not yet represented by TL Studio native handlers
- compatibility/session behavior covered by existing real-Kilo regression tests

Kilo is no longer the mandatory owner of the Agent loop or core coding tool execution for supported TL Studio-managed custom providers.

Private Kilo details such as `x-kilo-directory`, `prompt_async`, raw Kilo session envelopes, auth routes, question routes, and Kilo-specific tool IDs must remain isolated in compatibility/adapter code.

## Native execution safety

The native path includes:

- strict TL Studio tool IDs and schemas
- structured argument validation/results/errors
- project path confinement
- path traversal and symlink protections
- separate read/write/execute permission classes
- project-scoped remembered non-sensitive permission rules
- no remembered approval for sensitive terminal execution
- timeout/context cancellation
- process-tree stop behavior through the existing process manager
- maximum Agent iterations
- maximum tool rounds
- maximum tools per round
- repeated-identical-tool-call guard
- explicit execution ownership in persisted sessions
- no API secrets in session history

## Automated proof of independence

The repository contains deterministic native tests proving a coding task can complete without Kilo performing the Agent loop or tool execution.

The strongest proof starts a fake OpenAI-compatible HTTP model server and exercises:

```text
user request
→ native model request
→ model emits files.write
→ TL Studio Tool Executor writes a real fixture file
→ structured tool result returns to the model
→ second model request
→ final assistant response
→ semantic session persistence
```

Additional tests cover provider translation/normalization, path traversal rejection, unknown tools, native permission behavior, terminal cancellation, and prevention of Kilo-private protocol leakage into native runtime files.

## CI and validation status at Phase 2 promotion

Before PR #90 was squash-merged, the final `0.3.0-alpha.23` head passed:

- Browser strict TypeScript check
- Browser build
- generated-JavaScript cleanliness
- local Monaco bundle verification
- Go tests
- Go vet
- Browser JavaScript syntax
- npm quick-launcher verification
- TL Studio runtime-boundary enforcement
- Python test syntax
- supported platform cross-compiles
- real bundled-runtime product contract
- real bundled-runtime prompt/write-tool E2E
- custom-provider compatibility contract
- npm package contract

The old Kilo prompt/write E2E remains green through capability-based compatibility fallback.

## Important architectural decisions

1. Native execution is selected only when the requested custom provider/model can be resolved from TL Studio-owned provider definitions and credentials.
2. Hosted Kilo and unresolved legacy runtime providers fall back to the Kilo compatibility adapter.
3. Session persistence explicitly records the execution owner so native semantic history is not accidentally overwritten by compatibility runtime reads.
4. The existing semantic live-event channel is shared by native and compatibility execution; no second Browser event architecture was introduced.
5. The existing local file/search/process systems are reused; native tools do not duplicate those subsystems.
6. Kilo remains bundled for compatibility. Do not claim it is fully removable yet.

## Plugin / MCP milestone — implemented on feature branch

The `0.4.0-alpha.2` milestone adds a TL Studio-owned generic Plugin system with first-class MCP support.

Current architecture:

```text
Settings → Plugins
        ↓
TL Studio Plugin Manager
        ↓
MCP Client Manager
        ↓
discovered MCP tools
        ↓
TL Studio Tool Registry
        ↓
Permission Engine
        ↓
Native Tool Executor
        ↓
Native Agent
```

Implemented behavior:

- persisted project/global Plugin definitions owned by TL Studio
- initial Plugin type `mcp`
- initial MCP transport `stdio`
- transport client interface designed so Streamable HTTP can be added without changing the Agent architecture
- user-configurable command, argument vector, working directory, scope, and environment-variable names
- secret environment values stored through the existing TL Studio credential vault and omitted from normal Plugin API responses
- explicit user confirmation before enabling a local Plugin command
- MCP initialize handshake, dynamic tool discovery, optional resource discovery, structured tool calls/results, process error reporting, reconnect, stop, timeout, and cancellation signaling
- namespaced tool IDs in the form `mcp.<plugin-id>.<tool-name>`
- discovered MCP schemas normalized into native model tool definitions
- MCP tools merged into the existing TL Studio Tool Registry
- all MCP Agent calls routed through the existing native Permission Engine
- conservative permission classification for unknown MCP capabilities
- enabled Plugin tools exposed to the existing native Agent loop; the Agent remains the tool-selection decision maker
- Plugin disable/remove/project switch/application shutdown stops relevant MCP child processes
- Settings → Plugins list/add/configure/remove/enable/disable/Test Connection UI
- Graphify convenience detection for `graphify` and `graphify-mcp`
- Graphify Build/Rebuild using the fixed local `graphify extract . --code-only` command after explicit confirmation
- Graphify Open Graph reuses TL Studio Preview for `graphify-out/graph.html`
- Graphify MCP tools are never hardcoded; they are discovered through MCP like any other plugin

Deterministic automated coverage uses a fake local stdio MCP server and does not require Graphify in CI. Coverage proves Plugin persistence/scoping, disabled vs enabled process behavior, MCP initialization, tool/resource discovery, namespacing/schema normalization, native Agent tool availability, tool-result round-trip, permission authorization, cancellation, process failure status, secret redaction, disable/stop behavior, and preservation of the built-in native tool set.

The implementation has also passed the existing real bundled-runtime product contract, real prompt/write E2E, strict Browser TypeScript/build checks, Go test/vet, and custom-provider compatibility gates on the feature PR during development.

Final PR gates were green before merge:

- strict Browser TypeScript check and Browser build
- Go tests and vet, including deterministic fake stdio MCP coverage
- supported launcher cross-compiles
- npm package contract
- real bundled-runtime product contract
- real bundled-runtime prompt/write-tool E2E
- real bundled-runtime custom-provider contract

The milestone was squash-merged into `dev` only. Stable `main` remains on `v0.3.0`.

The post-merge `dev` Preview Build status must be re-read from GitHub when resuming, because this continuity checkpoint itself creates a later `dev` documentation commit and therefore triggers a newer private Preview Build.

Exact next product action after a green Preview Build: hands-on validate `0.4.0-alpha.2` on Windows, especially Settings → Plugins, arbitrary stdio MCP add/test/enable/disable, and Graphify build/query/open behavior. Do not start the separate runtime-independence phase until this milestone is validated.

## Next product phase

Phase 2 is complete according to its ownership criteria.

After the Plugin/MCP milestone is validated on `dev`, the previously identified runtime-independence work remains a separate future architectural phase: native session ownership cleanup, runtime-optional startup, and eventually lazy compatibility-engine startup. Do not mix that work into the Plugin/MCP milestone.

## Development behavior

- Continue autonomously through the current milestone.
- Prefer implementation and validation over repeatedly asking whether to continue.
- Preserve existing working behavior unless the milestone explicitly changes it.
- Keep engine-specific implementation details behind TL Studio-owned contracts/adapters.
- Before a session limit, update this file with current branch, version, commit/PR, completed work, tests, remaining work, exact next action, and Preview Build status.

## Communication and bidirectional-text rule

The project owner primarily communicates in Persian.

- Write Persian explanations in an RTL-friendly form.
- Do not start a Persian sentence with an English word, identifier, version, branch name, filename, or technical term.
- Avoid mixing Persian and English fragments in the same sentence when that can break bidirectional rendering.
- Put English technical terms, identifiers, commands, paths, branch names, filenames, versions, and code on their own separate lines whenever practical.
- Code blocks remain left-to-right.
- Keep architecture explanations simple and direct.
