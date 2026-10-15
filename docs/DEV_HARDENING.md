# Development hardening status

This work is based on `dev` at
`7ae0f9486b19e3373b4c2d84868adf08540d6df4`. Development pull requests target
`dev`; `main` remains the stable branch. This is a mitigation pass, not a release
acceptance certificate.

| Area | Implemented behavior | Remaining qualification or work |
| --- | --- | --- |
| Local API | Explicit per-launch bearer capability on protected requests and fetch-based SSE; no ambient auth cookies; refresh uses origin-scoped sessionStorage | Shared-host bootstrap secrecy, restart/multiple-tab and extension acceptance across supported browsers; no defense against a compromised same-user browser |
| Session ownership | One launcher per state directory via an OS lock; live PID owners excluded from interruption recovery; late results cannot recreate deleted sessions | Legacy JSON snapshot/index is still a two-file crash boundary. SQLite remains opt-in pending migration, crash-matrix, soak and manual acceptance |
| Provider streams | Responses item IDs and call IDs kept distinct; final tool output reconciled once; terminal events, argument objects, response-byte and output-token budgets enforced | Additional provider compatibility fixtures and paid real-model cancellation/long-session acceptance |
| Workspace | Mutations reject symlink/reparse parents and resolved protected paths; Browser identities preserve case | Portable protection against hostile path replacement between check and use, hardlinks and uncooperative external writers needs a separate design |
| MCP | Server-supplied annotations and tool names do not grant read-only authority; unknown tools require approval | Reviewed local capability manifests or process sandboxing could restore narrower automatic permissions safely |
| Attachments | Validated UTF-8 text/code payloads reach native model history; bounded count/size; unsupported binary attachments fail explicitly | Provider-specific multimodal support is not implemented |
| History/events | UI tail reads avoid loading unrelated archives; unused per-token invalidations removed | Actual incremental answer rendering and bounded model-context selection remain separate work |
| Shutdown | Owned native runs, commands, previews, plugins, helper bridges and stores have coordinated cleanup | Cross-platform soak tests and process-tree cancellation under unusual child behavior |
| Updates | Unsafe automatic in-place replacement disabled; release checks and manual downloads remain | Staged, verified, rollback-capable installation and power-loss tests before re-enabling |
| Maintenance | npm lockfile, pinned helper fallbacks, patched sanitizer bundle, release-auth guard, race/fuzz/vulnerability CI | Repository branch-protection settings and broad package extraction were not changed |

## Verification

The development patch has local coverage for unauthorized clients and port
isolation, state-lock exclusion, SQLite live-run recovery, duplicate Responses
calls, truncated streams, attachment delivery/rejection, deletion, protected
paths, bounded tail history and shutdown. A real Windows Chromium test verifies
header auth, cookie-free SSE, refresh and editor conflict/save flows. Official
Codex and Claude CLI smoke tests initialize/inspect the helpers without paid
model calls. CI runs native platform tests plus the Linux race detector and
bounded parser fuzzing.

Run the commands in [CONTRIBUTING.md](../CONTRIBUTING.md) before review. A green
suite does not replace the outstanding acceptance work above, especially storage
crash consistency and portable filesystem race guarantees.
