# Security

TL Studio is intentionally local-first. Its browser UI and bundled agent runtime bind to loopback by default, and the launcher refuses a non-loopback UI address.

## Threat model

The active local agent runtime can execute commands and modify files when the selected agent and permission policy allow it. Treat prompts, connected providers, MCP servers, repositories, and tool output as potentially security-sensitive inputs.

The launcher protects its local browser/runtime bridge by:

- starting the bundled runtime on loopback only,
- using a random per-run backend credential,
- keeping runtime credentials out of browser-side code,
- checking loopback Host values,
- rejecting cross-origin requests,
- adding a restrictive Content Security Policy,
- keeping project file operations inside the selected project boundary, and
- not exposing a project-owned remote control plane.

Project file APIs reject traversal and symlink escapes and protect Git metadata from workspace mutations. Live Preview content runs on a separate loopback origin from TL Studio's local control APIs.

The current bundled engine is a third-party implementation detail behind TL Studio's runtime boundary. Engine-specific security and compatibility details must not leak credentials or weaken the launcher-owned local boundary.

## Reporting

Please open a private GitHub security advisory if the repository has that feature enabled. Do not publish working exploits for unresolved vulnerabilities in a public issue.
