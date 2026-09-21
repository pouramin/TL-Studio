(() => {
  "use strict";
  const K = window.KLU;
  if (!K || K.__previewInstalled) return;
  K.__previewInstalled = true;

  const css = document.createElement("link");
  css.rel = "stylesheet";
  css.href = "/preview.css";
  document.head.appendChild(css);

  const toolbar = document.querySelector(".toolbar-controls");
  const main = document.querySelector(".main-pane");
  if (!toolbar || !main) return;

  const button = document.createElement("button");
  button.id = "previewButton";
  button.type = "button";
  button.className = "ghost small";
  button.textContent = "Preview";
  button.title = "Open live web preview";
  toolbar.insertBefore(button, toolbar.firstChild);

  const panel = document.createElement("section");
  panel.id = "previewPanel";
  panel.className = "preview-panel hidden";
  panel.setAttribute("aria-label", "Live web preview");
  panel.innerHTML = `
    <div class="preview-head">
      <div class="preview-title-wrap">
        <strong>Live Preview</strong>
        <span id="previewStatus">Not started</span>
      </div>
      <div class="preview-actions">
        <button id="previewStart" class="primary small" type="button">Start</button>
        <button id="previewStop" class="ghost small" type="button" disabled>Stop</button>
        <button id="previewReload" class="ghost small" type="button" disabled>Reload</button>
        <button id="previewExternal" class="ghost small" type="button" disabled>Open</button>
        <button id="previewClose" class="icon-button" type="button" aria-label="Close preview">×</button>
      </div>
    </div>
    <div class="preview-url-row">
      <span id="previewKind">Web preview</span>
      <code id="previewURL">No preview URL yet</code>
    </div>
    <div id="previewEntryRow" class="preview-entry-row hidden">
      <span>Preview file</span>
      <select id="previewEntry" aria-label="File to preview"></select>
    </div>
    <div id="previewEmpty" class="preview-empty">
      <strong>Preview this project</strong>
      <span id="previewHint">TL Studio can preview supported files or run a package.json dev script.</span>
      <pre id="previewLogs" class="preview-logs hidden"></pre>
    </div>
    <iframe id="previewFrame" class="preview-frame hidden" title="Project live preview" referrerpolicy="no-referrer"></iframe>`;
  main.appendChild(panel);

  const ui: TLStudioDynamicRecord = {
    button,
    panel,
    start: document.getElementById("previewStart"),
    stop: document.getElementById("previewStop"),
    reload: document.getElementById("previewReload"),
    external: document.getElementById("previewExternal"),
    close: document.getElementById("previewClose"),
    status: document.getElementById("previewStatus"),
    kind: document.getElementById("previewKind"),
    url: document.getElementById("previewURL"),
    entryRow: document.getElementById("previewEntryRow"),
    entry: document.getElementById("previewEntry"),
    empty: document.getElementById("previewEmpty"),
    hint: document.getElementById("previewHint"),
    logs: document.getElementById("previewLogs"),
    frame: document.getElementById("previewFrame"),
  };

  K.state.preview = { snapshot: null, poll: null, open: false, lastURL: "", reloadTimer: null, entrySignature: "", followTimer: null, entrySwitchGeneration: 0, registry: null, registryPromise: null };

  const request = async (path: string, options: RequestInit = {}) => {
    const response = await fetch(path, {
      cache: "no-store",
      ...options,
      headers: { ...(options.body ? { "Content-Type": "application/json" } : {}), ...(options.headers || {}) },
    });
    const type = response.headers.get("content-type") || "";
    const payload = response.status === 204
      ? null
      : type.includes("application/json") ? await response.json().catch(() => null) : await response.text().catch(() => "");
    if (!response.ok) {
      const detail = payload && typeof payload === "object" ? payload.error || payload.reason || payload.message : String(payload || `${response.status} ${response.statusText}`);
      const error = new Error(detail || "Preview request failed");
      error.payload = payload;
      throw error;
    }
    return payload;
  };

  const stopPolling = () => {
    if (K.state.preview.poll) window.clearInterval(K.state.preview.poll);
    K.state.preview.poll = null;
  };

  const setOpen = (open: any) => {
    K.state.preview.open = !!open;
    ui.panel.classList.toggle("hidden", !open);
    ui.button.classList.toggle("preview-toggle-active", !!open);
    if (open) loadPreviewRegistry().then(() => refreshStatus()).catch(() => {});
  };

  const loadPreviewRegistry = async () => {
    if (K.state.preview.registry) return K.state.preview.registry;
    if (!K.state.preview.registryPromise) {
      K.state.preview.registryPromise = request("/local/preview/capabilities")
        .then((registry) => {
          K.state.preview.registry = registry && typeof registry === "object" ? registry : { version: 1, capabilities: [] };
          return K.state.preview.registry;
        })
        .finally(() => { K.state.preview.registryPromise = null; });
    }
    return K.state.preview.registryPromise;
  };

  const extensionOf = (value: any) => {
    const name = String(value || "").trim().split(/[\\/]/).pop() || "";
    const index = name.lastIndexOf(".");
    return index > 0 ? name.slice(index).toLowerCase() : "";
  };

  const previewCapabilityForPath = (value: any) => {
    const ext = extensionOf(value);
    if (!ext) return null;
    for (const capability of K.state.preview.registry?.capabilities || []) {
      if ((capability?.extensions || []).some((candidate: any) => String(candidate).toLowerCase() === ext)) return capability;
    }
    return null;
  };

  const isPreviewablePath = (value: any) => !!previewCapabilityForPath(value);
  const activePreviewEntry = () => isPreviewablePath(K.state.activeEditorPath) ? String(K.state.activeEditorPath) : "";
  const kindLabel = (snapshot: any) => snapshot?.kind === "dev-server"
    ? "Dev server"
    : snapshot?.entryMeta?.name || (snapshot?.kind === "file" ? "File preview" : "Web preview");

  const previewPath = (base = "/local/preview", entry = "") => {
    const value = String(entry || "").trim();
    return value ? `${base}?${new URLSearchParams({ entry: value })}` : base;
  };

  const renderEntryChoices = (snapshot: any) => {
    const entries = Array.isArray(snapshot?.entries)
      ? snapshot.entries.filter((entry: any) => entry && typeof entry.path === "string")
      : [];
    const paths = entries.map((entry: any) => entry.path);
    const signature = entries.map((entry: any) => `${entry.path}:${entry.capabilityID || entry.kind || ""}`).join("\n");
    const preferred = String(snapshot?.entry || activePreviewEntry() || ui.entry?.value || "");
    if (ui.entry && K.state.preview.entrySignature !== signature) {
      K.state.preview.entrySignature = signature;
      ui.entry.textContent = "";
      for (const entry of entries) {
        const option = document.createElement("option");
        option.value = entry.path;
        option.textContent = `${entry.path} · ${entry.name || entry.kind || "Preview"}`;
        ui.entry.appendChild(option);
      }
    }
    if (ui.entry && paths.includes(preferred)) ui.entry.value = preferred;
    else if (ui.entry && paths.length && !paths.includes(ui.entry.value)) ui.entry.value = paths[0];
    ui.entryRow?.classList.toggle("hidden", entries.length <= 1 || snapshot?.kind === "dev-server");
    return entries;
  };

  const render = (snapshot: any) => {
    K.state.preview.snapshot = snapshot || null;
    const available = !!snapshot?.available;
    const running = !!snapshot?.running;
    const url = snapshot?.url || "";
    const starting = running && snapshot?.kind === "dev-server" && !url;
    const entries = renderEntryChoices(snapshot);
    const needsChoice = snapshot?.kind === "file" && entries.length > 1 && !snapshot?.entry;

    ui.kind.textContent = kindLabel(snapshot);
    ui.status.textContent = url ? "Live" : starting ? "Starting…" : running ? "Running" : available ? "Ready" : "Unavailable";
    ui.url.textContent = url || "No preview URL yet";
    ui.url.title = url;
    ui.start.disabled = !available || running || (needsChoice && !ui.entry?.value);
    ui.stop.disabled = !running;
    ui.reload.disabled = !url;
    ui.external.disabled = !url;

    const reason = snapshot?.error || snapshot?.reason || "";
    ui.hint.textContent = reason || (starting ? "Waiting for the dev server to report its local URL…" : "Start Preview to see the project here.");

    const output = String(snapshot?.output || "").trim();
    ui.logs.textContent = output;
    ui.logs.classList.toggle("hidden", !output || !!url);

    if (url) {
      ui.empty.classList.add("hidden");
      ui.frame.classList.remove("hidden");
      if (K.state.preview.lastURL !== url) {
        K.state.preview.lastURL = url;
        ui.frame.src = url;
      }
    } else {
      ui.frame.classList.add("hidden");
      ui.empty.classList.remove("hidden");
      if (!running) {
        ui.frame.removeAttribute("src");
        K.state.preview.lastURL = "";
      }
    }

    if (starting) {
      if (!K.state.preview.poll) K.state.preview.poll = window.setInterval(() => refreshStatus().catch(() => {}), 500);
    } else if (!running || url) {
      stopPolling();
    }
  };

  const refreshStatus = async () => {
    const snapshot = await request(previewPath("/local/preview", activePreviewEntry()));
    render(snapshot);
    return snapshot;
  };

  const start = async () => {
    K.showError?.("");
    setOpen(true);
    ui.start.disabled = true;
    try {
      await loadPreviewRegistry().catch(() => null);
      const requestedEntry = String(ui.entry?.value || activePreviewEntry() || "").trim();
      const snapshot = await request(previewPath("/local/preview", requestedEntry), { method: "POST" });
      render(snapshot);
      if (snapshot?.running && !snapshot?.url && !K.state.preview.poll) {
        K.state.preview.poll = window.setInterval(() => refreshStatus().catch(() => {}), 500);
      }
    } catch (error) {
      render(error.payload || { available: false, error: error.message || String(error) });
    }
  };

  const switchPreviewEntry = async (entry: any) => {
    const value = String(entry || "").trim();
    if (!value) return K.state.preview.snapshot;
    const generation = ++K.state.preview.entrySwitchGeneration;
    K.showError?.("");
    try {
      const snapshot = await request(previewPath("/local/preview", value), { method: "POST" });
      if (generation === K.state.preview.entrySwitchGeneration) render(snapshot);
      return snapshot;
    } catch (error) {
      if (generation === K.state.preview.entrySwitchGeneration) {
        render(error.payload || { available: false, error: error.message || String(error) });
      }
      return null;
    }
  };

  const followActivePreviewEntry = async (path: any) => {
    const entry = String(path || "").trim();
    if (!entry || !K.state.preview.open) return;
    await loadPreviewRegistry().catch(() => null);
    if (!isPreviewablePath(entry)) return;
    const snapshot = K.state.preview.snapshot;
    const capability = previewCapabilityForPath(entry);
    if (!snapshot?.running || snapshot?.entry === entry) return;
    if (snapshot?.kind === "dev-server" && capability?.kind === "html") return;
    if (K.state.preview.followTimer) window.clearTimeout(K.state.preview.followTimer);
    K.state.preview.followTimer = window.setTimeout(() => {
      K.state.preview.followTimer = null;
      const activeEntry = activePreviewEntry();
      const current = K.state.preview.snapshot;
      const activeCapability = previewCapabilityForPath(activeEntry);
      if (!K.state.preview.open || !current?.running) return;
      if (!activeEntry || activeEntry !== entry || current?.entry === activeEntry) return;
      if (current?.kind === "dev-server" && activeCapability?.kind === "html") return;
      switchPreviewEntry(activeEntry);
    }, 50);
  };

  const stop = async ({ silent = false } = {}) => {
    stopPolling();
    try {
      await request("/local/preview", { method: "DELETE" });
    } catch (error) {
      if (!silent) K.showError?.(error.message || String(error));
    }
    render(await request("/local/preview").catch(() => ({ available: false })));
  };

  const reload = () => {
    if (!ui.frame.src) return;
    try { ui.frame.contentWindow?.location.reload(); }
    catch { ui.frame.src = ui.frame.src; }
  };

  const scheduleFileReload = () => {
    if (!K.state.preview.open || K.state.preview.snapshot?.kind !== "file" || !K.state.preview.snapshot?.url) return;
    if (K.state.preview.reloadTimer) window.clearTimeout(K.state.preview.reloadTimer);
    K.state.preview.reloadTimer = window.setTimeout(() => {
      K.state.preview.reloadTimer = null;
      reload();
    }, 180);
  };

  ui.button.addEventListener("click", async () => {
    const opening = ui.panel.classList.contains("hidden");
    setOpen(opening);
    if (opening) {
      await loadPreviewRegistry().catch(() => null);
      const snapshot = await refreshStatus().catch(() => null);
      const activeEntry = activePreviewEntry();
      const activeCapability = previewCapabilityForPath(activeEntry);
      const shouldSwitch = snapshot?.running
        && activeEntry
        && activeEntry !== snapshot?.entry
        && !(snapshot?.kind === "dev-server" && activeCapability?.kind === "html");
      if (shouldSwitch) {
        await switchPreviewEntry(activeEntry);
        return;
      }
      const needsChoice = snapshot?.kind === "file" && Array.isArray(snapshot?.entries) && snapshot.entries.length > 1 && !snapshot?.entry;
      if (snapshot?.available && !snapshot?.running && !needsChoice) await start();
    }
  });
  ui.close.addEventListener("click", () => setOpen(false));
  ui.entry?.addEventListener("change", async () => {
    const value = String(ui.entry.value || "").trim();
    if (!value) return;
    if (K.state.preview.snapshot?.running) {
      await switchPreviewEntry(value);
      return;
    }
    ui.start.disabled = false;
    ui.hint.textContent = `Ready to preview ${value}.`;
  });
  ui.start.addEventListener("click", start);
  ui.stop.addEventListener("click", () => stop());
  ui.reload.addEventListener("click", reload);
  ui.external.addEventListener("click", () => {
    const url = K.state.preview.snapshot?.url;
    if (url) window.open(url, "_blank", "noopener,noreferrer");
  });

  // Editor/file mutations use fetch directly. Dispatch a narrow local event only
  // after successful workspace mutations so file previews update automatically.
  const nativeFetch = window.fetch.bind(window);
  window.fetch = async (input, init = {}) => {
    const response = await nativeFetch(input, init);
    try {
      const raw = typeof input === "string" ? input : input?.url || "";
      const parsed = new URL(raw, window.location.href);
      const method = String(init?.method || (typeof input !== "string" ? input?.method : "") || "GET").toUpperCase();
      const workspaceMutation = response.ok
        && parsed.origin === window.location.origin
        && (parsed.pathname === "/local/file" || parsed.pathname === "/local/entry")
        && ["PUT", "POST", "PATCH", "DELETE"].includes(method);
      if (workspaceMutation) window.dispatchEvent(new Event("tl-studio:project-file-changed"));
    } catch {}
    return response;
  };
  window.addEventListener("tl-studio:project-file-changed", scheduleFileReload);
  window.addEventListener("tl-studio:editor-render", (event) => followActivePreviewEntry(event.detail?.path));

  const baseHandleRuntimeEvent = K.handleRuntimeEvent;
  if (typeof baseHandleRuntimeEvent === "function") {
    K.handleRuntimeEvent = (event) => {
      const result = baseHandleRuntimeEvent(event);
      if (String(event?.type || "").startsWith("file.")) scheduleFileReload();
      return result;
    };
  }

  const baseAfterProjectChange = K.afterProjectChange;
  if (typeof baseAfterProjectChange === "function") {
    K.afterProjectChange = async (...args) => {
      await stop({ silent: true }).catch(() => {});
      const result = await baseAfterProjectChange(...args);
      if (K.state.preview.open) await refreshStatus().catch(() => {});
      return result;
    };
  }

  window.addEventListener("beforeunload", () => {
    stopPolling();
    if (K.state.preview.followTimer) window.clearTimeout(K.state.preview.followTimer);
    try { fetch("/local/preview", { method: "DELETE", keepalive: true }); } catch {}
  });

  loadPreviewRegistry().catch(() => {});
  K.preview = Object.freeze({ open: () => setOpen(true), start, stop, reload, refresh: refreshStatus, selectEntry: switchPreviewEntry, capabilities: loadPreviewRegistry });
})();
