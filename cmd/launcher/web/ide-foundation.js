(() => {
  "use strict";
  const K = window.KLU;
  if (!K || K.__ideFoundationInstalled) return;
  K.__ideFoundationInstalled = true;

  const normalizePath = (value) => String(value || "").replace(/\\/g, "/").replace(/^\/+|\/+$/g, "");
  const pathKey = (value) => {
    const normalized = normalizePath(value);
    return K.state.local?.platform === "windows" ? normalized.toLowerCase() : normalized;
  };
  const isDirty = (tab) => !!tab && tab.content !== tab.savedContent;
  const hasDirtyTabs = () => Array.isArray(K.state.editorTabs) && K.state.editorTabs.some(isDirty);

  const localRequest = async (path) => {
    const response = await fetch(path, { cache: "no-store" });
    const type = response.headers.get("content-type") || "";
    const payload = type.includes("application/json") ? await response.json().catch(() => null) : await response.text().catch(() => "");
    if (!response.ok) {
      const error = new Error(payload?.error || payload?.message || String(payload || `${response.status} ${response.statusText}`));
      error.status = response.status;
      throw error;
    }
    return payload;
  };

  const applyPreview = (tab, preview) => {
    tab.path = preview.path || tab.path;
    tab.content = preview.content || "";
    tab.savedContent = preview.content || "";
    tab.sha256 = preview.sha256 || "";
    tab.modified = preview.modified || "";
    tab.size = preview.size || 0;
    tab.mime = preview.mime || "text/plain";
    tab.externalChanged = false;
  };

  const rerenderTabs = () => {
    const buttons = [...document.querySelectorAll(".file-tab-open")];
    if (!buttons.length) return;
    const active = buttons.find((button) => pathKey(button.title) === pathKey(K.state.activeEditorPath));
    (active || buttons[0]).click();
  };

  const refreshOpenTabs = async () => {
    if (!Array.isArray(K.state.editorTabs) || !K.state.editorTabs.length) return;
    const removed = new Set();
    for (const tab of [...K.state.editorTabs]) {
      if (!tab?.path) continue;
      try {
        const preview = await localRequest(`/local/file?${new URLSearchParams({ path: tab.path })}`);
        if (preview?.binary) {
          tab.externalChanged = true;
          continue;
        }
        if (isDirty(tab)) {
          tab.externalChanged = !!preview.sha256 && preview.sha256 !== tab.sha256;
        } else {
          applyPreview(tab, preview);
        }
      } catch (error) {
        if (error.status !== 404) continue;
        if (isDirty(tab)) {
          tab.externalChanged = true;
        } else {
          removed.add(pathKey(tab.path));
        }
      }
    }
    if (removed.size) {
      K.state.editorTabs = K.state.editorTabs.filter((tab) => !removed.has(pathKey(tab.path)));
      if (removed.has(pathKey(K.state.activeEditorPath))) {
        K.state.activeEditorPath = K.state.editorTabs.at(-1)?.path || "";
      }
    }
    rerenderTabs();
  };

  window.addEventListener("beforeunload", (event) => {
    if (!hasDirtyTabs()) return;
    event.preventDefault();
    event.returnValue = "";
  });

  const confirmProjectSwitch = () => !hasDirtyTabs() || window.confirm("Switch projects and discard all unsaved editor changes?");
  const originalPickProject = K.pickProject;
  if (typeof originalPickProject === "function") {
    K.pickProject = (...args) => {
      if (!confirmProjectSwitch()) return undefined;
      return originalPickProject(...args);
    };
  }
  const originalSetManualProject = K.setManualProject;
  if (typeof originalSetManualProject === "function") {
    K.setManualProject = (event, ...args) => {
      event?.preventDefault?.();
      if (!confirmProjectSwitch()) return undefined;
      return originalSetManualProject(event, ...args);
    };
  }

  let refreshTimer = null;
  const scheduleRefresh = () => {
    if (refreshTimer) window.clearTimeout(refreshTimer);
    refreshTimer = window.setTimeout(() => {
      refreshTimer = null;
      refreshOpenTabs().catch((error) => console.warn("[TL Studio] Workspace reconciliation failed", error));
    }, 140);
  };

  const originalHandleRuntimeEvent = K.handleRuntimeEvent;
  K.handleRuntimeEvent = (event) => {
    originalHandleRuntimeEvent(event);
    const type = event?.type || "";
    if (type.startsWith("file.") || type === "session.diff" || type === "session.idle") scheduleRefresh();
  };

  K.workspaceFiles = { refreshOpenTabs, hasDirtyTabs };
})();
