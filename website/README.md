# TL Studio documentation site

This directory contains the public documentation and product site for **TL Studio**. It is intentionally isolated from the Go application and from internal engineering notes under `../docs/`.

The public site treats **TL Studio as the product identity**. Third-party execution/runtime implementation details stay behind TL Studio-owned contracts and should not dominate user-facing documentation.

## Documentation source of truth

The repository is the final source of truth. Before release-oriented documentation changes, reconcile the public site with current `main`, current `dev`, `VERSION`, the root READMEs, and `docs/ARCHITECTURE.md`.

Current documented lines:

```text
Stable: v0.3.0 on main
Dev:    v0.4.0-alpha.1 on dev
```

Phase 3 implementation has not started; do not invent v0.4 features.

## Stable v0.3 areas covered

- Browser IDE and locally bundled Monaco;
- Project files, Search, Terminal, and capability-driven Preview;
- Provider/Model registry and TL Studio credential vault;
- Tool Registry and native core Tool Executor;
- Project-scoped Permission policy;
- semantic Sessions, persistence, Questions, and live events;
- native Agent execution for supported custom Providers;
- compatibility fallback for hosted/legacy/runtime-only capabilities;
- local network/filesystem/process/Preview security boundaries;
- stable npm launcher and portable release model.

Implementation-specific compatibility-engine naming should remain isolated to the maintainer Runtime Integration reference unless legal attribution genuinely requires it.

## Languages

- English: `/` and `/docs/...`
- فارسی: `/fa/` and `/fa/docs/...` with RTL layout

The language switcher is part of the top navigation beside the appearance and social controls. Persian uses Vazirmatn and keeps established technical terms in English where that reads more naturally.

## Stack

- Next.js 16
- Fumadocs
- Tailwind CSS 4
- static search
- GitHub Pages deployment
- per-page Markdown export plus `llms.txt` / `llms-full.txt`

## Local development

```bash
cd website
npm install
npm run dev
```

## GitHub Pages build

```bash
DEPLOY_TARGET=static NEXT_PUBLIC_BASE_PATH=/TL-Studio npm run build
```

Output is written to `website/out/`.

Nothing under `website/` is imported by the Go launcher or included in TL Studio release archives.
