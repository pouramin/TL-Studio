# Security

TL Studio is intentionally local-first. Its Browser control UI binds to loopback and the launcher refuses a non-loopback UI address.

The Native Agent can read or modify project files and execute commands only through TL Studio tools and permission policy. Treat prompts, connected providers, MCP servers, repositories, tool output, and plugin processes as potentially security-sensitive inputs.

The launcher protects its local control surface by:

- binding the control server to loopback;
- rejecting non-loopback Host values;
- requiring Browser Origin to match the local control origin;
- keeping provider credentials out of Browser code and provider registry files;
- keeping ChatGPT OAuth material inside the official Codex client's isolated TL Studio-specific `CODEX_HOME`;
- enforcing project boundaries on filesystem operations;
- rejecting traversal and symlink escapes;
- isolating Preview content from the TL Studio control origin;
- permission-gating sensitive tool actions.

TL Studio may launch an explicit provider-owned helper process only when that provider's documented integration requires it. The ChatGPT account integration uses OpenAI's official Codex CLI/App Server for login and ChatGPT-plan model transport. TL Studio does not read or copy Codex OAuth tokens, browser cookies, private session tokens, or undocumented ChatGPT credentials.

The ChatGPT model bridge runs in an empty temporary working directory with user/project Codex configuration and rules ignored, a read-only sandbox, approval policy set to never, and web search disabled. TL Studio remains responsible for project Tool execution and Permission decisions.

External model providers, the official Codex helper, and MCP servers are separate trust boundaries configured or selected by the user.

Do not expose the local control port through a public proxy. Review project code, provider endpoints, plugin commands, and requested permissions before approving sensitive actions.

Please report security issues privately to the repository owner rather than opening a public exploit report.

## Development hardening

The control API requires an explicit per-launch bearer capability on every
protected request, including the fetch-based event stream. It does not accept
ambient cookies. The Browser keeps its capability in origin-scoped tab storage
to survive refresh; it never sends it to Preview ports or other origins.

A state directory has one application owner, enforced by an OS file lock.
Use a separate TL_STUDIO_STATE_DIR for an independent concurrent instance.
SQLite is still opt-in pending its documented acceptance work. PID liveness
checks avoid treating another live owner's pending run as a crashed run.

External MCP annotations do not grant read-only authority. Unknown MCP tools
require explicit execution approval and are excluded from read-only Agent turns.
Approving a plugin executable is a separate trust decision from approving tools.

Native attachments currently support UTF-8 text/code only (256 KiB each,
1 MiB total). Images and PDFs are rejected explicitly rather than dropped.
Automatic in-place installation replacement is disabled: download new releases
into a separate folder while rollback-capable updates are being qualified.
