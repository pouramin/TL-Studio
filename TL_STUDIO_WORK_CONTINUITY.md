# TL Studio work continuity

Last updated: 2026-10-04

## Source of truth

The actual repository state is the source of truth. Before substantial work, inspect current dev and main and reconcile this file with README.md, README.fa_IR.md, docs/ARCHITECTURE.md, VERSION, open PRs, releases, and CI.

Stable main contains only validated stable releases. Ongoing development continues on dev.

## Current milestone

Development version:

0.7.0-alpha.1

Stable base:

v0.6.0 on main

Development status:

TL Studio v0.6.0 is published as the stable release from main commit 0bef7774ea1e087cfac1c0b1194b74eca1ff67bc. Stable main must remain on the 0.6 line except for intentional stable bugfix releases.

The 0.7 development generation begins on dev at 0.7.0-alpha.1. Reserve 0.6.x versions for stable bugfix releases rather than new alpha development.

The v0.6.0 release includes the Laya Router, JEV Direct through TypeSafe while preserving JEV via OpenRouter, global Graphify installation with project-local graphs, the responsive Plugins redesign, Claude Web Bridge improvements, native routed-Agent fallback/cooldown reliability, the single-scroll Settings redesign, and the verified GitHub Releases updater.

Release validation:

- PR #167 merged the validated feature work into dev.
- PR #168 promoted the stable release through the protected dev → main path.
- v0.6.0 tag points to the exact stable main commit.
- Release workflow #38 succeeded.
- CI #1021 and Custom Provider Contract #881 succeeded on the exact stable main commit.

Stable promotion rule:

Keep main stable. Develop 0.7 work only on dev or branches based on dev. Future stable promotion must again use the protected PR path and pass the required checks.


The 0.7 Context Management foundation is integrated with configurable Agent run limits on dev, including multi-tool latest-round protection, fail-closed oversized requests, and bounded final checkpoints. Tests must pass on the integrated commit before promotion.

## Architecture state

TL Studio core execution is fully native.

Owned by TL Studio:

- Workspace and Monaco editor
- Files and Search
- Terminal/process foundation
- Preview
- Plugin/MCP system
- Provider registry and discovery
- Credential vault
- Custom Providers
- Direct native model clients
- Native Agent loop
- Native Context Manager and model-turn budgeting
- Native Tool Registry and Tool Executor
- Native Permissions
- Native semantic Session persistence and execution
- Native interactive Questions
- Native local Event Bus
- generic Provider Account domain
- JEV/OpenRouter integration

Normal startup initializes TL Studio services and the loopback local server directly.

Optional provider/plugin integrations may launch their own explicit helper processes only when that integration is used. ChatGPT account access uses the official OpenAI Codex CLI/App Server and Claude subscription access uses the official Anthropic Claude Code CLI. Neither helper replaces TL Studio Session, Permission, Tool, persistence, project mutation, or Native Agent ownership.

Native execution is the normal core execution mode.

## Session architecture

Browser
→ /local/sessions*
→ TL Studio Session domain
→ Native Agent
→ selected Provider

Session create, rename, delete, run, abort, status, transcript, activity, changes, and persistence are TL Studio-owned.

## Question architecture

Native Agent
→ interaction.question
→ TL Studio Question Manager
→ /local/questions
→ Browser
→ reply/reject
→ Question Manager
→ Native Agent resumes

The Question Manager supports multiple choice, custom text, rejection, session scoping, cancellation, and semantic attention events.

## Permissions and events

Permissions are fully TL Studio-owned. Pending requests, one-time approvals, remembered project rules, rejection, and enforcement use /local/permissions*.

The local Event Bus is authoritative:

Native Agent / Tool / Session / Question / Permission / Workspace
→ local event bus
→ /local/events
→ Browser

There is no external runtime event stream.

## Providers

Provider configuration and model catalogs are authoritative in TL Studio.

Direct native model protocols currently include OpenAI-compatible Chat Completions, OpenAI Responses, Anthropic Messages, and Google Gemini generateContent. ChatGPT-plan access uses `codex-chatgpt`; Claude.ai subscription access uses `claude-code-account`. Unsupported capabilities return explicit errors.

The generic ProviderAccountAdapter architecture now has a typed account lifecycle: begin login, OAuth callback completion or polling, cancellation, refresh, status, model discovery, and logout. Browser state is semantic only and does not carry provider access tokens, refresh tokens, authorization codes, PKCE verifiers, API keys, cookies, or client secrets.

Manual API credentials and TL Studio-managed account credentials use separate credential-vault slots. An account credential takes precedence for native model execution while connected, but signing out exposes the preserved manual API credential instead of deleting it.

Provider-owned helper authentication is isolated. OpenAI's official Codex client manages ChatGPT OAuth/refresh material inside a TL Studio-specific `CODEX_HOME`. Anthropic's official Claude Code client manages Claude.ai subscription credentials inside a TL Studio-specific `CLAUDE_CONFIG_DIR`. TL Studio never reads or serializes either raw OAuth credential.

OpenRouter is the first concrete 0.6 account adapter. It uses the documented OpenRouter OAuth + PKCE flow, performs code exchange server-side, stores the resulting account key only in the TL Studio credential vault, discovers models through the normal provider discovery layer, and exposes those models through the existing Native Agent/model selector path.

Hugging Face is merged on dev through PR #123. It uses TL Studio's public CIMD identity at https://pouramin.dev/.well-known/oauth-cimd, Authorization Code + PKCE, loopback callbacks, account-vault access/refresh token persistence, automatic refresh in the Provider Account adapter, and the official OpenAI-compatible Inference Providers router. The provider-specific credential lifecycle remains behind ProviderAccountAdapter; the Native Agent receives only the resolved runtime credential.

Google/Gemini is merged on dev through PR #124. The implementation uses the official installed-app OAuth flow with PKCE, refresh and revocation, a Google Cloud quota project, native Gemini generateContent transport, model discovery, and preservation of Gemini thought signatures across tool-call continuation. No client secret is embedded. PR #127 adds a generic non-secret Provider Account setup surface so the 0.6 alpha can persist the Desktop OAuth Client ID and Google Cloud Project ID from Provider Settings; TL_STUDIO_GOOGLE_CLIENT_ID and TL_STUDIO_GOOGLE_PROJECT_ID remain development overrides. OAuth access/refresh tokens still live only in the credential vault. PR #128 is merged on dev. Each Google OAuth login now uses a dedicated temporary 127.0.0.1 listener on a random port so redirect_uri follows Google's documented Desktop loopback form instead of using a path on TL Studio's control server.

Custom Provider setup is simplified so a new provider normally requires only an API address, OpenAI-compatible or Anthropic-compatible protocol choice, and credential. Provider IDs are generated internally and model discovery runs automatically on save; manual model entry remains an explicit fallback. PR #126 fixed edit-mode behavior so hidden legacy Model ID fields cannot silently bypass a fresh discovery run when connection settings change.

PR #148 introduced connection-aware compact Provider cards; PR #157 kept branded API providers on those cards instead of duplicating them under Custom Providers. Claude is now a hybrid card: Claude.ai account sign-in and Anthropic API-key configuration are independent, with account-backed runtime models stored under `claude-account` so they never intercept the manual `claude` API provider.

ChatGPT/Codex account support is merged on dev through PR #143. It uses OpenAI's documented Codex surface. Login uses `codex app-server` with the official `account/login/start` ChatGPT browser flow, `account/read`, `account/logout`, and `model/list`. TL Studio uses an isolated `CODEX_HOME`, never imports browser cookies/session tokens, never copies a private OAuth client, and never calls undocumented ChatGPT backend endpoints.

For ChatGPT-plan inference, the account-managed Provider uses protocol `codex-chatgpt`. TL Studio keeps one official Codex app-server warm and reuses it across turns instead of starting a new Codex process for every prompt. Each TL Studio model turn creates an ephemeral structured Codex thread with read-only sandboxing, approval policy `never`, web search disabled, and Codex built-in shell/web/plugin/multi-agent/tool paths disabled. The bridge receives the TL Studio conversation and TL Studio Tool schemas and must return structured assistant text or TL Studio Tool calls. TL Studio remains authoritative for the outer model → tool → model loop, Tool execution, permissions, project mutations, Session persistence, and semantic events.

The ChatGPT adapter auto-detects an installed `codex` executable. When unavailable it can use `npx @openai/codex`; Provider Settings also exposes a non-secret Codex executable override. Windows npm shims are resolved through Node so the official Codex app-server starts reliably. Tests include a fake Codex executable plus a real Windows Codex app-server/CLI contract smoke with no real credentials.

PR #125 originally exposed ChatGPT/Codex, Claude, and GitHub Copilot as unavailable boundaries. ChatGPT was resolved through official Codex surfaces in PR #143. Claude account support is active on current dev through PR #162 using Anthropic's documented Claude Code browser-auth and non-interactive CLI surfaces, without implementing a private OAuth client. GitHub Copilot is now the only deferred account boundary because its documented model-access path remains coupled to the Copilot SDK/runtime.

## Browser contract

Browser code uses TL Studio-owned /local/* APIs.

Provider-specific helper processes are not Browser-facing control surfaces; the Browser sees only semantic TL Studio Provider Account state.

## Packaging

Preview and Release workflows build TL Studio plus supported bundled plugins and notices.

The ChatGPT and Claude account integrations do not bundle their provider CLIs into the TL Studio package in this alpha. They use installed official executables or explicit npx fallbacks at runtime.

Release and review packages remain subject to package-content validation.

## Regression proof

Required evidence before review:

- strict Browser TypeScript and Browser build
- go test ./...
- go vet ./...
- supported platform cross-compiles
- standalone normal startup
- native Product Contract
- native Custom Provider contract
- Native Agent model/tool/model filesystem E2E
- Native Agent interactive Question E2E
- Native Permission tests
- real Browser smoke
- ChatGPT official-Codex account lifecycle with fake Codex fixture
- ChatGPT persistent app-server reuse regression test
- ChatGPT structured model/tool bridge regression test
- real official Codex app-server/CLI contract smoke on Windows
- Claude account lifecycle/model bridge fake-CLI regression coverage
- real official Claude Code CLI/auth-status contract smoke on Windows
- SHA256 for the Windows review package

## Manual Windows validation

The final review package must run as:

.\tl-studio.exe

Manual review should verify:

- normal startup
- project loading
- Explorer and Monaco
- Search
- Sessions
- Provider setup
- Native Agent response
- native file tool and workspace reconciliation
- interactive Question flow
- Permission approval/reject/remember behavior
- Terminal and Stop
- Preview
- Plugins/MCP
- JEV/OpenRouter where configured
- ChatGPT sign-in through official Codex
- ChatGPT account model discovery
- ChatGPT model response and TL Studio Tool call round-trip
- Claude.ai browser sign-in through official Claude Code
- Claude account model response and TL Studio Tool call round-trip
- Claude restart/sign-out persistence with manual API preservation

## Current manual validation gate

Automated validation is green through PR #155, including fake-Codex account lifecycle, persistent app-server reuse, model/tool bridge coverage, and real official Codex startup/CLI contract checks on Windows.

Real ChatGPT/Codex validation passed in full on Windows on 2026-09-29. Verified behavior includes browser login, account connection, model discovery/selection, model identity, project read/search/write, TL Studio Permission handling, model → Tool → model continuation, Terminal execution, roughly 3–5 second warm-turn latency, restart persistence, and explicit sign-out persistence.

Claude account automation is green through PR #162, including fake-CLI lifecycle/model-bridge coverage, the no-`--bare` subscription-auth regression, and a real Windows official-CLI smoke without credentials. Real Claude.ai subscription login/model/Tool/restart/logout validation remains pending.

Use docs/ACCOUNT_PROVIDER_VALIDATION.md.

A Windows x64 review package is produced by CI and should be used for the remaining real provider checks. The ChatGPT/Codex Windows checklist is complete; Claude.ai real-account validation and the intended API-key provider setup checks remain before stable promotion. Do not promote dev to main until the intended provider integrations have passed their remaining checklist items or their limitations have been explicitly accepted and documented.

## Resume protocol

1. Inspect current dev and main; repository state is the source of truth.
2. Reconcile this checkpoint with README.md, README.fa_IR.md, docs/ARCHITECTURE.md, VERSION, open PRs, and CI.
3. Keep product development on dev.
4. Promote dev to main only for a validated stable release.
5. After a stable promotion, align dev with the new main baseline before beginning the next milestone.
