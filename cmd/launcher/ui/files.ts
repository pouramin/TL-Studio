(() => {
  "use strict";
  const K = window.KLU;
  const $ = (id) => document.getElementById(id);

  const ui = {
    button: $("filesButton"),
    panel: $("filesPanel"),
    close: $("closeFiles"),
    refresh: $("refreshFiles"),
    up: $("filesUp"),
    breadcrumb: $("filesBreadcrumb"),
    list: $("filesList"),
    changesPanel: $("changesPanel"),
    changesButton: $("changesButton"),
  };

  const normalizePath = (value) => String(value || "").replace(/\\/g, "/").replace(/^\/+|\/+$/g, "");
  const basename = (value) => normalizePath(value).split("/").pop() || "";
  const parentPath = (value) => {
    const bits = normalizePath(value).split("/").filter(Boolean);
    bits.pop();
    return bits.join("/");
  };
  const joinPath = (parent, name) => [normalizePath(parent), String(name || "").trim()].filter(Boolean).join("/");
  const pathKey = (value) => normalizePath(value).toLowerCase();

  const makeButton = (id, label, title = label) => {
    const button = document.createElement("button");
    button.id = id;
    button.type = "button";
    button.className = "ghost small";
    button.textContent = label;
    button.title = title;
    return button;
  };

  const installWorkspaceUI = () => {
    if (!ui.panel) return;
    ui.panel.setAttribute("aria-label", "Project workspace");
    if (ui.button) {
      ui.button.textContent = "Workspace";
      ui.button.title = "Open the local project workspace";
    }

    const kicker = ui.panel.querySelector(".files-head .section-label");
    if (kicker) kicker.textContent = "PROJECT WORKSPACE";
    const headActions = ui.panel.querySelector(".files-head-actions");
    if (headActions && !$("newFile")) {
      headActions.insertBefore(makeButton("newFile", "+ File", "Create a file in the current folder"), ui.refresh);
      headActions.insertBefore(makeButton("newFolder", "+ Folder", "Create a folder in the current folder"), ui.refresh);
    }

    const browserHead = ui.panel.querySelector(".files-browser-head");
    if (browserHead) {
      const oldHint = browserHead.querySelector("span");
      if (oldHint) oldHint.remove();
      const actions = document.createElement("div");
      actions.className = "files-selection-actions";
      actions.append(makeButton("renameFile", "Rename", "Rename selected entry"), makeButton("deleteFile", "Delete", "Delete selected entry"));
      browserHead.appendChild(actions);
    }

    const preview = ui.panel.querySelector(".file-preview");
    if (preview) {
      preview.className = "file-editor";
      preview.setAttribute("aria-label", "Code editor");
      preview.innerHTML = `
        <div id="fileTabs" class="file-tabs" role="tablist" aria-label="Open files"></div>
        <div class="file-editor-head">
          <div class="file-editor-title-wrap">
            <strong id="fileEditorTitle">No file open</strong>
            <span id="fileEditorPath">Select a file from the explorer</span>
          </div>
          <div class="file-editor-actions">
            <span id="fileEditorMeta"></span>
            <button id="askFile" class="ghost small" type="button" disabled>Ask Agent</button>
            <button id="reloadFile" class="ghost small" type="button" disabled>Reload</button>
            <button id="saveFile" class="primary small" type="button" disabled>Save</button>
          </div>
        </div>
        <div id="fileEditorBody" class="file-editor-body">
          <div id="fileEditorEmpty" class="file-editor-empty">
            <strong>Open a project file</strong>
            <span>Edit locally, save directly to disk, and hand selected code back to the agent.</span>
          </div>
          <div id="filePreviewOnly" class="file-editor-empty hidden">
            <strong id="filePreviewOnlyTitle">Preview-only file</strong>
            <span id="filePreviewOnlyHint">This file is displayed in Live Preview instead of the text editor.</span>
          </div>
          <div id="fileEditorSurface" class="file-editor-surface hidden">
            <pre id="fileLineNumbers" class="file-line-numbers" aria-hidden="true">1</pre>
            <textarea id="fileEditor" class="file-editor-input" spellcheck="false" autocomplete="off" autocapitalize="off" aria-label="File editor"></textarea>
          </div>
        </div>
        <div class="file-editor-statusbar">
          <span id="editorStatus">Ready</span>
          <span id="editorCursor">Ln 1, Col 1</span>
        </div>`;
    }

    Object.assign(ui, {
      newFile: $("newFile"),
      newFolder: $("newFolder"),
      rename: $("renameFile"),
      remove: $("deleteFile"),
      tabs: $("fileTabs"),
      title: $("fileEditorTitle"),
      path: $("fileEditorPath"),
      meta: $("fileEditorMeta"),
      ask: $("askFile"),
      reload: $("reloadFile"),
      save: $("saveFile"),
      editorBody: $("fileEditorBody"),
      empty: $("fileEditorEmpty"),
      previewOnly: $("filePreviewOnly"),
      previewOnlyTitle: $("filePreviewOnlyTitle"),
      previewOnlyHint: $("filePreviewOnlyHint"),
      surface: $("fileEditorSurface"),
      gutter: $("fileLineNumbers"),
      editor: $("fileEditor"),
      status: $("editorStatus"),
      cursor: $("editorCursor"),
    });
  };

  installWorkspaceUI();

  K.state.filesPath = "";
  K.state.filesEntries = [];
  K.state.filesProject = "";
  K.state.filesLoading = false;
  K.state.selectedFileEntry = null;
  K.state.editorTabs = [];
  K.state.activeEditorPath = "";

  const sizeText = (bytes) => {
    const value = Number(bytes) || 0;
    if (value < 1024) return `${value} B`;
    if (value < 1024 * 1024) return `${(value / 1024).toFixed(value < 10 * 1024 ? 1 : 0)} KB`;
    return `${(value / (1024 * 1024)).toFixed(1)} MB`;
  };

  const extensionLabel = (path) => {
    const name = basename(path);
    const index = name.lastIndexOf(".");
    return index > 0 ? name.slice(index + 1).toUpperCase() : "TEXT";
  };

  const languageHint = (path) => {
    const extension = extensionLabel(path).toLowerCase();
    const aliases = { js: "javascript", jsx: "jsx", ts: "typescript", tsx: "tsx", py: "python", go: "go", rs: "rust", java: "java", cs: "csharp", cpp: "cpp", c: "c", h: "c", html: "html", css: "css", json: "json", md: "markdown", sh: "bash", ps1: "powershell", yml: "yaml", yaml: "yaml", xml: "xml", sql: "sql" };
    return aliases[extension] || "";
  };

  const byteSize = (text) => {
    try { return new TextEncoder().encode(String(text || "")).length; }
    catch (_) { return String(text || "").length; }
  };

  const isDirty = (tab) => !!tab && !tab.viewOnly && tab.content !== tab.savedContent;
  const activeTab = () => K.state.editorTabs.find((tab) => pathKey(tab.path) === pathKey(K.state.activeEditorPath)) || null;
  const tabFor = (path) => K.state.editorTabs.find((tab) => pathKey(tab.path) === pathKey(path)) || null;

  const localRequest = async (path, options = {}) => {
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
      const detail = payload && typeof payload === "object"
        ? payload.error || payload.message || JSON.stringify(payload)
        : String(payload || `${response.status} ${response.statusText}`);
      const error = new Error(detail);
      error.status = response.status;
      error.payload = payload;
      throw error;
    }
    return payload;
  };

  const setSelectedEntry = (entry) => {
    K.state.selectedFileEntry = entry || null;
    renderFiles();
  };

  const updateSelectionActions = () => {
    const selected = K.state.selectedFileEntry;
    if (ui.rename) ui.rename.disabled = !selected;
    if (ui.remove) ui.remove.disabled = !selected;
  };

  const renderFiles = () => {
    if (!ui.list || !ui.breadcrumb || !ui.up) return;
    ui.breadcrumb.textContent = K.state.filesPath || "Project root";
    ui.breadcrumb.title = K.state.filesPath || K.state.local?.project || "Project root";
    ui.up.disabled = !K.state.filesPath;
    ui.list.textContent = "";
    updateSelectionActions();

    if (K.state.filesLoading) {
      const loading = document.createElement("div");
      loading.className = "files-empty";
      loading.textContent = "Reading folder…";
      ui.list.appendChild(loading);
      return;
    }

    if (!K.state.filesEntries.length) {
      const empty = document.createElement("div");
      empty.className = "files-empty";
      empty.textContent = "This folder is empty.";
      ui.list.appendChild(empty);
      return;
    }

    for (const entry of K.state.filesEntries) {
      const row = document.createElement("button");
      row.type = "button";
      row.className = `file-row file-${entry.type || "file"}`;
      if (pathKey(K.state.selectedFileEntry?.path) === pathKey(entry.path)) row.classList.add("selected");
      row.title = entry.type === "directory" ? `${entry.path || entry.name}\nDouble-click to open folder` : entry.path || entry.name;

      const icon = document.createElement("span");
      icon.className = "file-icon";
      icon.textContent = entry.type === "directory" ? "▸" : entry.type === "symlink" ? "↗" : "·";

      const copy = document.createElement("span");
      copy.className = "file-copy";
      const name = document.createElement("strong");
      name.textContent = entry.name || entry.path || "Untitled";
      const meta = document.createElement("small");
      meta.textContent = entry.type === "directory" ? "Folder" : entry.type === "symlink" ? "Symlink" : sizeText(entry.size);
      copy.append(name, meta);
      row.append(icon, copy);

      row.addEventListener("click", () => {
        setSelectedEntry(entry);
        if (entry.type === "file") openEditor(entry.path);
        else if (entry.type === "symlink") K.showError("Symlink editing is intentionally disabled in the workspace editor.");
      });
      row.addEventListener("dblclick", () => {
        if (entry.type === "directory") loadDirectory(entry.path);
      });
      row.addEventListener("keydown", (event) => {
        if (event.key === "Enter" && entry.type === "directory") {
          event.preventDefault();
          loadDirectory(entry.path);
        }
      });
      ui.list.appendChild(row);
    }
  };

  const renderTabs = () => {
    if (!ui.tabs) return;
    ui.tabs.textContent = "";
    for (const tab of K.state.editorTabs) {
      const wrapper = document.createElement("div");
      wrapper.className = "file-tab";
      if (pathKey(tab.path) === pathKey(K.state.activeEditorPath)) wrapper.classList.add("active");
      if (isDirty(tab)) wrapper.classList.add("dirty");
      if (tab.externalChanged) wrapper.classList.add("external-change");
      wrapper.setAttribute("role", "presentation");

      const open = document.createElement("button");
      open.type = "button";
      open.className = "file-tab-open";
      open.setAttribute("role", "tab");
      open.setAttribute("aria-selected", pathKey(tab.path) === pathKey(K.state.activeEditorPath) ? "true" : "false");
      open.title = tab.path;
      const dot = document.createElement("span");
      dot.className = "file-tab-dot";
      dot.textContent = isDirty(tab) ? "●" : tab.externalChanged ? "!" : "";
      const label = document.createElement("span");
      label.textContent = basename(tab.path) || tab.path;
      open.append(dot, label);
      open.addEventListener("click", () => activateTab(tab.path));

      const close = document.createElement("button");
      close.type = "button";
      close.className = "file-tab-close";
      close.textContent = "×";
      close.title = `Close ${basename(tab.path)}`;
      close.setAttribute("aria-label", `Close ${basename(tab.path)}`);
      close.addEventListener("click", (event) => {
        event.stopPropagation();
        closeTab(tab.path);
      });
      wrapper.append(open, close);
      ui.tabs.appendChild(wrapper);
    }
    window.dispatchEvent(new CustomEvent("tl-studio:editor-tabs", {
      detail: { paths: K.state.editorTabs.map((tab) => tab.path) },
    }));
  };

  const renderLineNumbers = (content) => {
    if (!ui.gutter) return;
    const count = Math.max(1, String(content ?? "").split("\n").length);
    ui.gutter.textContent = Array.from({ length: count }, (_, index) => String(index + 1)).join("\n");
  };

  const updateCursor = () => {
    if (!ui.cursor || !ui.editor || ui.surface?.classList.contains("hidden")) return;
    const value = ui.editor.value;
    const offset = Math.max(0, ui.editor.selectionStart || 0);
    const before = value.slice(0, offset);
    const line = before.split("\n").length;
    const lastBreak = before.lastIndexOf("\n");
    const column = offset - lastBreak;
    ui.cursor.textContent = `Ln ${line}, Col ${column}`;
  };

  const updateEditorChrome = () => {
    const tab = activeTab();
    if (!tab) {
      if (ui.title) ui.title.textContent = "No file open";
      if (ui.path) ui.path.textContent = "Select a file from the explorer";
      if (ui.meta) ui.meta.textContent = "";
      if (ui.status) ui.status.textContent = "Ready";
      if (ui.cursor) ui.cursor.textContent = "Ln 1, Col 1";
      if (ui.save) ui.save.disabled = true;
      if (ui.reload) ui.reload.disabled = true;
      if (ui.ask) ui.ask.disabled = true;
      return;
    }
    if (ui.title) ui.title.textContent = basename(tab.path) || tab.path;
    if (ui.path) { ui.path.textContent = tab.path; ui.path.title = tab.path; }
    if (ui.meta) ui.meta.textContent = `${extensionLabel(tab.path)} · ${sizeText(tab.viewOnly ? tab.size : byteSize(tab.content))}`;
    if (ui.save) ui.save.disabled = tab.viewOnly || !isDirty(tab);
    if (ui.reload) ui.reload.disabled = false;
    if (ui.ask) ui.ask.disabled = !!tab.viewOnly;
    if (ui.status) {
      ui.status.classList.toggle("warning", !!tab.externalChanged);
      ui.status.classList.toggle("dirty", isDirty(tab));
      ui.status.textContent = tab.viewOnly
        ? `${tab.previewName || "Preview"} · view only`
        : tab.externalChanged
          ? "Changed on disk · reload or save to resolve"
          : isDirty(tab) ? "Unsaved changes" : "Saved";
    }
    if (ui.cursor && tab.viewOnly) ui.cursor.textContent = "View only";
    else updateCursor();
  };

  const notifyEditorRender = (tab) => {
    window.dispatchEvent(new CustomEvent("tl-studio:editor-render", {
      detail: {
        path: tab?.path || "",
        content: tab?.content || "",
        language: tab && !tab.viewOnly ? languageHint(tab.path) : "",
        viewOnly: !!tab?.viewOnly,
        previewKind: tab?.previewKind || "",
        previewName: tab?.previewName || "",
        mime: tab?.mime || "",
      },
    }));
  };

  const renderEditor = () => {
    const tab = activeTab();
    renderTabs();
    if (!ui.empty || !ui.surface || !ui.editor) return;
    ui.empty.classList.toggle("hidden", !!tab);
    ui.previewOnly?.classList.toggle("hidden", !tab?.viewOnly);
    ui.surface.classList.toggle("hidden", !tab || !!tab.viewOnly);
    if (!tab) {
      ui.editor.value = "";
      renderLineNumbers("");
      updateEditorChrome();
      notifyEditorRender(null);
      return;
    }
    if (tab.viewOnly) {
      ui.editor.value = "";
      renderLineNumbers("");
      if (ui.previewOnlyTitle) ui.previewOnlyTitle.textContent = `${tab.previewName || "Preview"} file`;
      if (ui.previewOnlyHint) ui.previewOnlyHint.textContent = `${basename(tab.path)} is shown in Live Preview. Binary editing is intentionally disabled.`;
      updateEditorChrome();
      notifyEditorRender(tab);
      return;
    }
    if (ui.editor.value !== tab.content) ui.editor.value = tab.content;
    renderLineNumbers(tab.content);
    updateEditorChrome();
    notifyEditorRender(tab);
  };

  const activateTab = (path) => {
    const tab = tabFor(path);
    if (!tab) return;
    K.state.activeEditorPath = tab.path;
    renderEditor();
    if (!tab.viewOnly) window.setTimeout(() => ui.editor?.focus(), 0);
  };

  const closeTab = (path, options = {}) => {
    const index = K.state.editorTabs.findIndex((tab) => pathKey(tab.path) === pathKey(path));
    if (index < 0) return true;
    const tab = K.state.editorTabs[index];
    if (!options.force && isDirty(tab) && !window.confirm(`Close ${basename(tab.path)} and discard unsaved changes?`)) return false;
    K.state.editorTabs.splice(index, 1);
    if (pathKey(K.state.activeEditorPath) === pathKey(tab.path)) {
      const next = K.state.editorTabs[index] || K.state.editorTabs[index - 1] || null;
      K.state.activeEditorPath = next?.path || "";
    }
    renderEditor();
    return true;
  };

  const applyPreviewToTab = (tab, preview) => {
    tab.path = preview.path || tab.path;
    tab.content = preview.content || "";
    tab.savedContent = preview.content || "";
    tab.sha256 = preview.sha256 || "";
    tab.modified = preview.modified || "";
    tab.size = preview.size || 0;
    tab.mime = preview.mime || "text/plain";
    tab.externalChanged = false;
  };

  const openEditor = async (path) => {
    if (!path) return;
    const existing = tabFor(path);
    if (existing) {
      K.state.activeEditorPath = existing.path;
      renderEditor();
      return;
    }
    K.showError("");
    try {
      const preview = await K.request(`/local/file?${new URLSearchParams({ path })}`);
      const viewOnly = !!preview?.binary && !!preview?.previewable;
      if (preview?.binary && !viewOnly) throw new Error(`Binary files without a TL Studio preview cannot be opened yet (${preview.mime || "unknown type"}).`);
      const tab = {
        path: preview.path || path,
        content: viewOnly ? "" : preview.content || "",
        savedContent: viewOnly ? "" : preview.content || "",
        sha256: preview.sha256 || "",
        modified: preview.modified || "",
        size: preview.size || 0,
        mime: preview.mime || "text/plain",
        viewOnly,
        previewKind: preview.previewKind || "",
        previewName: preview.previewName || "",
        previewCapabilityID: preview.previewCapabilityID || "",
        externalChanged: false,
      };
      K.state.editorTabs.push(tab);
      K.state.activeEditorPath = tab.path;
      renderEditor();
      if (!tab.viewOnly) window.setTimeout(() => ui.editor?.focus(), 0);
    } catch (error) {
      K.showError(error.message || String(error));
    }
  };

  const refreshTabFromDisk = async (tab, options = {}) => {
    if (!tab?.path) return;
    try {
      const preview = await K.request(`/local/file?${new URLSearchParams({ path: tab.path })}`);
      if (preview?.binary) {
        if (!preview.previewable) return;
        tab.viewOnly = true;
        tab.content = "";
        tab.savedContent = "";
        tab.sha256 = preview.sha256 || "";
        tab.modified = preview.modified || "";
        tab.size = preview.size || 0;
        tab.mime = preview.mime || "application/octet-stream";
        tab.previewKind = preview.previewKind || "";
        tab.previewName = preview.previewName || "";
        tab.previewCapabilityID = preview.previewCapabilityID || "";
        tab.externalChanged = false;
        if (pathKey(tab.path) === pathKey(K.state.activeEditorPath)) renderEditor();
        else renderTabs();
        return;
      }
      tab.viewOnly = false;
      if (!options.force && isDirty(tab)) {
        if (preview.sha256 && preview.sha256 !== tab.sha256) tab.externalChanged = true;
      } else {
        applyPreviewToTab(tab, preview);
      }
      if (pathKey(tab.path) === pathKey(K.state.activeEditorPath)) renderEditor();
      else renderTabs();
    } catch (error) {
      if (options.silent) return;
      K.showError(error.message || String(error));
    }
  };

  const saveActive = async (force = false) => {
    const tab = activeTab();
    if (!tab || tab.viewOnly || !isDirty(tab)) return;
    K.showError("");
    if (ui.save) ui.save.disabled = true;
    try {
      const preview = await localRequest("/local/file", {
        method: "PUT",
        body: JSON.stringify({
          path: tab.path,
          content: tab.content,
          expectedSha256: tab.sha256,
          force,
        }),
      });
      applyPreviewToTab(tab, preview);
      renderEditor();
      await loadDirectory(K.state.filesPath || "", { preserveSelection: true });
    } catch (error) {
      if (error.status === 409 && !force) {
        tab.externalChanged = true;
        renderEditor();
        const overwrite = window.confirm(`${basename(tab.path)} changed on disk after you opened it. Overwrite the disk version with your editor contents?`);
        if (overwrite) return saveActive(true);
        return;
      }
      K.showError(error.message || String(error));
    } finally {
      updateEditorChrome();
    }
  };

  const reloadActive = async () => {
    const tab = activeTab();
    if (!tab) return;
    if ((isDirty(tab) || tab.externalChanged) && !window.confirm(`Reload ${basename(tab.path)} from disk and discard the current editor buffer?`)) return;
    await refreshTabFromDisk(tab, { force: true });
  };

  const askAgentAboutSelection = () => {
    const tab = activeTab();
    if (!tab || tab.viewOnly || !ui.editor || !K.els?.prompt) return;
    const start = ui.editor.selectionStart || 0;
    const end = ui.editor.selectionEnd || 0;
    const selected = ui.editor.value.slice(Math.min(start, end), Math.max(start, end));
    const language = languageHint(tab.path);
    const prompt = selected
      ? `In \`${tab.path}\`, help me with this selected code:\n\n\`\`\`${language}\n${selected}\n\`\`\``
      : `Review \`${tab.path}\` and help me improve or debug it.`;
    K.els.prompt.value = prompt;
    K.els.prompt.dispatchEvent(new Event("input", { bubbles: true }));
    ui.panel?.classList.add("hidden");
    window.setTimeout(() => K.els.prompt.focus(), 0);
  };

  const loadDirectory = async (path = "", options = {}) => {
    K.state.filesLoading = true;
    K.state.filesPath = normalizePath(path);
    renderFiles();
    try {
      const params = new URLSearchParams();
      if (path) params.set("path", path);
      const payload = await K.request(`/local/files${params.size ? `?${params}` : ""}`);
      K.state.filesPath = payload?.path || "";
      K.state.filesEntries = Array.isArray(payload?.entries) ? payload.entries : [];
      if (!options.preserveSelection) K.state.selectedFileEntry = null;
      else if (K.state.selectedFileEntry) {
        const fresh = K.state.filesEntries.find((entry) => pathKey(entry.path) === pathKey(K.state.selectedFileEntry.path));
        K.state.selectedFileEntry = fresh || null;
      }
    } catch (error) {
      K.state.filesEntries = [];
      K.showError(error.message || String(error));
    } finally {
      K.state.filesLoading = false;
      renderFiles();
    }
  };

  const validEntryName = (value) => {
    const name = String(value || "").trim();
    if (!name || name === "." || name === ".." || /[\\/]/.test(name)) return "";
    return name;
  };

  const createEntry = async (type) => {
    const label = type === "directory" ? "folder" : "file";
    const input = window.prompt(`New ${label} name:`);
    if (input == null) return;
    const name = validEntryName(input);
    if (!name) return K.showError(`${label[0].toUpperCase() + label.slice(1)} name must be a single valid name without slashes.`);
    const path = joinPath(K.state.filesPath, name);
    K.showError("");
    try {
      const entry = await localRequest("/local/entry", { method: "POST", body: JSON.stringify({ path, type }) });
      await loadDirectory(K.state.filesPath || "");
      const fresh = K.state.filesEntries.find((item) => pathKey(item.path) === pathKey(entry?.path || path));
      if (fresh) setSelectedEntry(fresh);
      if (type === "file") await openEditor(entry?.path || path);
    } catch (error) {
      K.showError(error.message || String(error));
    }
  };

  const renameSelected = async () => {
    const selected = K.state.selectedFileEntry;
    if (!selected) return;
    const input = window.prompt("Rename to:", selected.name || basename(selected.path));
    if (input == null) return;
    const name = validEntryName(input);
    if (!name) return K.showError("Name must be a single valid name without slashes.");
    const source = selected.path;
    const target = joinPath(parentPath(source), name);
    if (pathKey(source) === pathKey(target)) return;
    K.showError("");
    try {
      const renamed = await localRequest("/local/entry", { method: "PATCH", body: JSON.stringify({ path: source, newPath: target }) });
      const sourcePrefix = `${normalizePath(source)}/`;
      for (const tab of K.state.editorTabs) {
        if (pathKey(tab.path) === pathKey(source)) tab.path = renamed?.path || target;
        else if (selected.type === "directory" && normalizePath(tab.path).toLowerCase().startsWith(sourcePrefix.toLowerCase())) {
          tab.path = `${normalizePath(renamed?.path || target)}${normalizePath(tab.path).slice(normalizePath(source).length)}`;
        }
      }
      if (pathKey(K.state.activeEditorPath) === pathKey(source)) K.state.activeEditorPath = renamed?.path || target;
      else if (selected.type === "directory" && normalizePath(K.state.activeEditorPath).toLowerCase().startsWith(sourcePrefix.toLowerCase())) {
        K.state.activeEditorPath = `${normalizePath(renamed?.path || target)}${normalizePath(K.state.activeEditorPath).slice(normalizePath(source).length)}`;
      }
      await loadDirectory(K.state.filesPath || "");
      const fresh = K.state.filesEntries.find((item) => pathKey(item.path) === pathKey(renamed?.path || target));
      if (fresh) K.state.selectedFileEntry = fresh;
      renderFiles();
      renderEditor();
    } catch (error) {
      K.showError(error.message || String(error));
    }
  };

  const deleteSelected = async () => {
    const selected = K.state.selectedFileEntry;
    if (!selected) return;
    const affectedTabs = K.state.editorTabs.filter((tab) => pathKey(tab.path) === pathKey(selected.path) || (selected.type === "directory" && normalizePath(tab.path).toLowerCase().startsWith(`${normalizePath(selected.path).toLowerCase()}/`)));
    const dirty = affectedTabs.some(isDirty);
    const detail = dirty ? " Unsaved editor changes inside it will also be discarded." : "";
    if (!window.confirm(`Delete ${selected.type === "directory" ? "folder" : "file"} “${selected.name || basename(selected.path)}”?${detail}`)) return;
    K.showError("");
    try {
      const params = new URLSearchParams({ path: selected.path });
      if (selected.type === "directory") params.set("recursive", "true");
      await localRequest(`/local/entry?${params}`, { method: "DELETE" });
      const deleted = normalizePath(selected.path).toLowerCase();
      K.state.editorTabs = K.state.editorTabs.filter((tab) => {
        const path = normalizePath(tab.path).toLowerCase();
        return path !== deleted && !(selected.type === "directory" && path.startsWith(`${deleted}/`));
      });
      if (!tabFor(K.state.activeEditorPath)) K.state.activeEditorPath = K.state.editorTabs.at(-1)?.path || "";
      K.state.selectedFileEntry = null;
      await loadDirectory(K.state.filesPath || "");
      renderEditor();
    } catch (error) {
      K.showError(error.message || String(error));
    }
  };

  const resetFiles = () => {
    K.state.filesPath = "";
    K.state.filesEntries = [];
    K.state.filesProject = K.state.local?.project || "";
    K.state.filesLoading = false;
    K.state.selectedFileEntry = null;
    K.state.editorTabs = [];
    K.state.activeEditorPath = "";
    renderFiles();
    renderEditor();
  };

  const openFiles = async () => {
    if (!ui.panel) return;
    ui.changesPanel?.classList.add("hidden");
    ui.panel.classList.remove("hidden");
    const project = K.state.local?.project || "";
    if (K.state.filesProject !== project) resetFiles();
    await loadDirectory(K.state.filesPath || "", { preserveSelection: true });
    const tab = activeTab();
    if (tab) await refreshTabFromDisk(tab, { silent: true });
  };

  const openEditorAt = async ({ path, line = 1, column = 1, match = "" } = {}) => {
    if (!path) return;
    await openFiles();
    await openEditor(path);
    const tab = tabFor(path);
    if (!tab || !ui.editor || tab.viewOnly) return;
    const content = String(tab.content || "");
    const lines = content.split("\n");
    const lineIndex = Math.max(0, Math.min(lines.length - 1, Number(line || 1) - 1));
    let offset = 0;
    for (let index = 0; index < lineIndex; index++) offset += lines[index].length + 1;
    const lineText = lines[lineIndex] || "";
    const runeColumn = Math.max(0, Number(column || 1) - 1);
    const columnPrefix = Array.from(lineText).slice(0, runeColumn).join("");
    const start = Math.min(content.length, offset + columnPrefix.length);
    const end = Math.min(content.length, start + String(match || "").length);
    ui.editor.focus();
    ui.editor.setSelectionRange(start, Math.max(start, end));
    const lineHeight = Number.parseFloat(window.getComputedStyle(ui.editor).lineHeight) || 20;
    const scrollTop = Math.max(0, (lineIndex - 2) * lineHeight);
    ui.editor.scrollTop = scrollTop;
    if (ui.gutter) ui.gutter.scrollTop = scrollTop;
    updateCursor();
    window.dispatchEvent(new CustomEvent("tl-studio:editor-reveal", {
      detail: { path: tab.path, line: lineIndex + 1, column: runeColumn + 1, match: String(match || "") },
    }));
  };

  K.openWorkspace = openFiles;
  K.openWorkspaceFileAt = openEditorAt;

  ui.button?.addEventListener("click", openFiles);
  ui.close?.addEventListener("click", () => ui.panel.classList.add("hidden"));
  ui.refresh?.addEventListener("click", async () => {
    await loadDirectory(K.state.filesPath || "", { preserveSelection: true });
    const tab = activeTab();
    if (tab) await refreshTabFromDisk(tab, { silent: true });
  });
  ui.up?.addEventListener("click", () => loadDirectory(parentPath(K.state.filesPath)));
  ui.newFile?.addEventListener("click", () => createEntry("file"));
  ui.newFolder?.addEventListener("click", () => createEntry("directory"));
  ui.rename?.addEventListener("click", renameSelected);
  ui.remove?.addEventListener("click", deleteSelected);
  ui.save?.addEventListener("click", () => saveActive(false));
  ui.reload?.addEventListener("click", reloadActive);
  ui.ask?.addEventListener("click", askAgentAboutSelection);
  ui.changesButton?.addEventListener("click", () => ui.panel?.classList.add("hidden"));

  ui.editor?.addEventListener("input", () => {
    const tab = activeTab();
    if (!tab || tab.viewOnly) return;
    tab.content = ui.editor.value;
    renderLineNumbers(tab.content);
    renderTabs();
    updateEditorChrome();
  });
  ui.editor?.addEventListener("scroll", () => {
    if (ui.gutter) ui.gutter.scrollTop = ui.editor.scrollTop;
  });
  for (const eventName of ["click", "keyup", "select"]) ui.editor?.addEventListener(eventName, updateCursor);
  ui.editor?.addEventListener("keydown", (event) => {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s") {
      event.preventDefault();
      saveActive(false);
      return;
    }
    if (event.key === "Tab" && !event.ctrlKey && !event.metaKey && !event.altKey) {
      event.preventDefault();
      const start = ui.editor.selectionStart;
      const end = ui.editor.selectionEnd;
      ui.editor.setRangeText("  ", start, end, "end");
      ui.editor.dispatchEvent(new Event("input", { bubbles: true }));
    }
  });
  document.addEventListener("keydown", (event) => {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s" && !ui.panel?.classList.contains("hidden")) {
      event.preventDefault();
      saveActive(false);
    }
  });

  const originalAfterProjectChange = K.afterProjectChange;
  K.afterProjectChange = async (...args) => {
    resetFiles();
    ui.panel?.classList.add("hidden");
    return originalAfterProjectChange(...args);
  };

  const originalHandleRuntimeEvent = K.handleRuntimeEvent;
  K.handleRuntimeEvent = (event) => {
    originalHandleRuntimeEvent(event);
    const type = event?.type || "";
    const shouldRefresh = type.startsWith("file.") || type === "session.diff" || type === "session.idle";
    if (!shouldRefresh) return;
    window.setTimeout(async () => {
      if (!ui.panel?.classList.contains("hidden")) await loadDirectory(K.state.filesPath || "", { preserveSelection: true });
      const tab = activeTab();
      if (tab) await refreshTabFromDisk(tab, { silent: true });
    }, 120);
  };

  resetFiles();
})();
