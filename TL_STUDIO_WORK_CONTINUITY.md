# TL Studio Work Continuity

This file is a durable operating instruction for future TL Studio development sessions.

## Source of truth

- Repository: `https://github.com/pouramin/TL-Studio`
- The repository state is the final source of truth.
- Before continuing substantial work, inspect current `dev` and reconcile this file, `README.md`, `README.fa_IR.md`, `docs/ARCHITECTURE.md`, `docs/KILO_API_CONTRACT.md`, and `VERSION` with the actual code.
- Do not restart or redesign the project from scratch.

## Branch and release discipline

- `main` is stable production only and is promoted to the stable `v0.3.0` line after Phase 2 validation.
- `dev` is the next private alpha line.
- Feature/fix branches start from `dev`.
- Experimental work must not be merged into `main`.
- Private alpha builds use the GitHub Actions Preview Build artifact flow.
- Stable releases are promoted only after automated gates and hands-on validation.

## Current development state

Current stable baseline after Phase 2 promotion:

`0.3.0`

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

## Next product phase

Phase 2 is complete according to its ownership criteria.

The next major architectural work is Phase 3: reduce the remaining runtime dependency further by deciding which compatibility-only capabilities should become TL Studio-native next, and whether/when launcher startup can make the third-party engine optional instead of always bundled/started.

Do not begin Phase 3 by rewriting the product. Continue from the current boundaries.

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
