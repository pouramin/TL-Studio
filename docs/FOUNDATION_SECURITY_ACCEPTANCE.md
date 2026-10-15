# Foundation security acceptance — v0.7 (Draft)

This is a **release gate**, not an announcement of an implemented defense. Baseline: `dev` at `05efaaaf7965b39a834a1d12861d885cb6234275`. Stabilization work stays in PR #178.

**Development update:** See [DEV_HARDENING.md](DEV_HARDENING.md) for the
subsequent mitigation and remaining acceptance work. The current Browser uses
explicit bearer headers through `fetch`, including SSE; it does not use the
cookie/EventSource design considered below. The original release criteria remain
historical context, not a claim that every release gate has passed.

## Threat boundaries

TL Studio binds the Go server to loopback and validates Host and browser Origin. This blocks many browser-originated cross-site requests, **but is not client authentication**: a native program belonging to another OS user on the same host can reach the listener, omit Origin, call API routes and interact with pending approvals. This was demonstrated by the independent reviewer's isolated local probe. Do not describe loopback or CSP as a complete multi-user security boundary.

The intended protection is against *other OS users* and arbitrary local clients that lack the launch secret, not against malware running with full privileges as the **same** OS user (which can often inspect browser/user data). A malicious webpage is a distinct threat and should be tested separately; current Host/Origin restrictions already provide partial protection there.

## Required API authentication contract (not implemented)

- Generate an unguessable launch-scoped secret with a cryptographically secure RNG; do not persist it with project data or place it in predictable static HTML.
- Require proof of possession on the local API, including sensitive reads, tool execution/permission replies and live events. Make any public health endpoint and separately paired relay routes explicit exceptions, not accidental broad exclusions.
- Provide the launched browser a one-time bootstrap mechanism whose secret is not sent on a normal HTTP navigation request. A fragment-based bootstrap and a session-scoped HttpOnly SameSite cookie is a possible design, not an approved implementation.
- Confirm `EventSource` authentication. It cannot send arbitrary headers; a same-origin authenticated cookie or short-lived event ticket must be validated with a browser test.
- The Claude Web extension already authenticates its relay routes with a separate per-pair token. Preserve or deliberately replace that contract and test it; do not silently block its polling or leak the global launch token to the extension.
- Browser reloads, two tabs, server restart, `--no-browser` usage and CLI/browser harnesses must work without disabling authentication in production.
- Deny unauthenticated and incorrect-secret clients without leaking session state; verify requests that arrive without Origin. Test authorization **before** any potentially mutating handler.
- Do not present a cookie issued to anyone who simply GETs `/` as authentication: it gives unauthorized local processes their own valid credential.

**Acceptance evidence:** adversarial native-client requests return 401/403 with pending approval unchanged; authenticated user actions and SSE work; restart invalidates prior launch credentials; extension compatibility and Windows browser runtime checks pass.

## Session transaction boundary

Snapshot and session-list index are distinct files. Writing each atomically and publishing memory only after both succeed prevents the reproduced failed-save cache divergence, but does **not** make their pair an atomic transaction under process crash or power loss. On index-write failure, the committed snapshot should be restored and restoration failures surfaced explicitly; crash injection at both writes remains an acceptance requirement. A future durable transaction journal or canonical snapshot rebuild strategy may be needed.

## Other outstanding risks

- Uncooperative external file writers can still modify a file between revision validation and the final rename. A mutex and adjacent lock file only coordinate cooperating writers; do not promise portable conflict-free compare-and-swap.
- Syncing staged bytes before rename reduces risk of truncation, but directory entry durability across sudden power loss is platform/filesystem-specific.
- Review UI content sinks and preview iframe isolation with real browser experiments. Existing application CSP is not a substitute for correct source-to-sink escaping.
- Audit remaining state-file writer implementations for fallback in-place writes, especially on Windows.
- Final Foundation approval requires Windows manual tests on a disposable project, real-provider runs, cancellation/restart, and long-session soak tests.

Do not merge PR #178 or publish v0.7 until the required release blockers above have a recorded disposition.
