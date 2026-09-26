// TL Studio Browser entry point.
//
// Keep imports in product initialization order while the Browser source is
// migrated incrementally away from the historical shared global kernel.
// esbuild follows this module graph and emits the single Browser bundle loaded
// by index.html.

import "./kernel";
import "./core";
import "./runtime-api";
import "./tools";
import "./chat";
import "./presentation";
import "./attention";
import "./workspace";
import "./files";
import "./monaco";
import "./status-ui";
import "./product-ui";

import "./ide-foundation";
import "./editor-enhancements";
import "./terminal";
import "./preview";
import "./preview-floating";
import "./search";
import "./settings-enhancements";
import "./plugins";
import "./attachments";
import "./diagnostics-ui";
import "./provider-recovery-ui";
import "./providers-ui";
import "./provider-discovery-ui";
import "./jev-ui";
import "./providers-settings-bridge";
import "./legacy-sessions";

import "./app";
