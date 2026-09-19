# Internal engineering docs

- [`ARCHITECTURE.md`](./ARCHITECTURE.md) describes the current TL Studio launcher, browser workspace, runtime abstraction, process/Terminal foundation, Live Preview isolation, and local security boundaries.
- [`KILO_API_CONTRACT.md`](./KILO_API_CONTRACT.md) documents the implementation-specific compatibility contract for the **currently bundled** Kilo engine. It is not the browser-facing TL Studio product contract.

Product-facing code should use TL Studio-owned concepts and the `/runtime/*` boundary. Engine-specific routes, credentials, package names, and compatibility quirks belong behind the launcher/runtime adapter.

Milestone-specific implementation notes should be folded back into these durable documents instead of accumulating as separate one-off files.
