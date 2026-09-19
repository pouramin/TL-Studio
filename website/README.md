# TL Studio documentation site

This directory contains the public documentation and product site for **TL Studio**. It is intentionally isolated from the Go application and from the internal engineering notes under `../docs/`.

The public site treats **TL Studio as the product identity**. The bundled Agent engine is an implementation dependency behind a TL Studio-owned runtime contract, not the center of user-facing documentation.

## Documentation source of truth

Public docs must be reconciled with the current stable repository before a release-oriented update.

For v0.2, the public site reflects TL Studio-owned capabilities including:

- writable Project Workspace and multi-tab Editor;
- Project Search;
- project-scoped Terminal/process management;
- Live Preview;
- Provider/Model registry;
- Agent sessions, attachments, permissions, recovery, and Changes;
- local filesystem, process, Preview, and runtime security boundaries.

Implementation-specific engine naming should remain isolated to the maintainer Runtime Integration reference unless legal attribution or compatibility debugging genuinely requires it.

## Languages

- English: `/` and `/docs/...`
- فارسی: `/fa/` and `/fa/docs/...` with RTL layout

The language switcher is part of the top navigation beside the appearance controls. Persian copy uses Vazirmatn and keeps established technical terms in English where that reads more naturally.

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
