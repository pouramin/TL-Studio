# TL Studio Work Continuity

This file is a durable operating instruction for future TL Studio development sessions.

## Source of truth

- Repository: `https://github.com/pouramin/TL-Studio`
- The repository state is the final source of truth.
- Before continuing substantial work, inspect the current `dev` branch and reconcile this file, `README.md`, and `docs/ARCHITECTURE.md` with the actual code.
- Do not restart or redesign the project from scratch.

## Branch and release discipline

- `main` is stable only.
- `dev` is the next private alpha line.
- `feature/*` branches start from `dev`.
- Experimental work must not be merged into `main`.
- Private alpha builds are GitHub Actions Preview Build artifacts.
- Stable releases are promoted only after automated gates and hands-on validation.

## Development behavior

- Continue autonomously through the current milestone.
- Prefer implementing and validating the work over describing a plan and asking for permission.
- Preserve existing working behavior unless the milestone explicitly changes it.
- Keep engine-specific implementation details behind TL Studio-owned contracts and adapters.
- If the session is approaching a usage limit, stop beginning large tasks and leave a durable repository checkpoint with current state, completed work, remaining work, branch/commit/PR/build references, and exact next steps.

## Communication and bidirectional-text rule

The project owner primarily communicates in Persian. This rule must be included explicitly in every new-chat continuation prompt prepared for this project.

- Write Persian explanations in an RTL-friendly form.
- Do not start a Persian sentence with an English word, identifier, version, branch name, filename, or technical term.
- Avoid mixing Persian and English fragments in the same sentence when that can break bidirectional rendering.
- Put English technical terms, identifiers, commands, paths, branch names, filenames, versions, and code on their own separate line whenever practical.
- Prefer this visual rhythm:

  Persian explanation line.

  `English technical term or identifier`

  Persian continuation line.

- Code blocks remain left-to-right.
- Keep explanations simple and direct when describing architecture.

## Current direction

TL Studio is an independent local browser-based development workspace. The current bundled engine is Kilo Code 7.6.2, but it is an implementation detail behind TL Studio-owned boundaries.

The long-term direction is:

1. Product semantics owned by TL Studio.
2. Replaceable runtime/engine adapters.
3. Session read/commands/persistence, interactive questions, custom-provider credentials, provider/model definitions, permission policy, and tool semantics are now TL Studio-owned.
4. The two remaining heavy Phase 2 milestones are tool-execution ownership and the Agent execution loop.
5. Eventually third-party runtimes become optional adapters rather than product dependencies.
