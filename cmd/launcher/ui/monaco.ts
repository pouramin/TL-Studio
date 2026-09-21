(() => {
  "use strict";

  const K = window.KLU;
  if (!K) return;

  const state: TLStudioDynamicRecord = {
    loading: null,
    editor: null,
    monaco: null,
    host: null,
    textarea: null,
    surface: null,
    models: new Map(),
    activePath: "",
    pendingRender: null,
    pendingReveal: null,
    suppressChange: false,
  };

  const pathKey = (value: any) => String(value || "").replace(/\\/g, "/").replace(/^\/+|\/+$/g, "").toLowerCase();

  const languageForPath = (path: any) => {
    const clean = String(path || "").replace(/\\/g, "/");
    const name = clean.split("/").pop() || "";
    if (/^dockerfile(?:\..+)?$/i.test(name)) return "dockerfile";
    const dot = name.lastIndexOf(".");
    const ext = dot >= 0 ? name.slice(dot + 1).toLowerCase() : "";
    return ({
      js: "javascript", mjs: "javascript", cjs: "javascript", jsx: "javascript",
      ts: "typescript", mts: "typescript", cts: "typescript", tsx: "typescript",
      json: "json", jsonc: "json",
      html: "html", htm: "html", css: "css", scss: "scss", less: "less",
      md: "markdown", markdown: "markdown",
      py: "python", go: "go", rs: "rust", java: "java", cs: "csharp",
      c: "c", h: "c", cc: "cpp", cpp: "cpp", cxx: "cpp", hpp: "cpp",
      sh: "shell", bash: "shell", zsh: "shell", ps1: "powershell",
      yml: "yaml", yaml: "yaml", xml: "xml", sql: "sql", php: "php",
      rb: "ruby", lua: "lua", r: "r", swift: "swift", kt: "kotlin", kts: "kotlin",
    })[ext] || "plaintext";
  };

  const currentTheme = () => document.documentElement.dataset.resolvedTheme === "light" ? "vs" : "vs-dark";

  const installWorkerFactory = () => {
    globalThis.MonacoEnvironment = {
      getWorker(_moduleId: any, label: any) {
        const file = label === "json"
          ? "/monaco-json-worker.js"
          : ["css", "scss", "less"].includes(label)
            ? "/monaco-css-worker.js"
            : ["html", "handlebars", "razor"].includes(label)
              ? "/monaco-html-worker.js"
              : ["typescript", "javascript"].includes(label)
                ? "/monaco-ts-worker.js"
                : "/monaco-editor-worker.js";
        return new Worker(file, { name: `TL Studio ${label || "editor"} worker` });
      },
    };
  };

  const loadStylesheet = () => new Promise((resolve, reject) => {
    const existing = document.querySelector('link[data-tl-monaco="style"]');
    if (existing) return resolve();
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = "/monaco-editor.css";
    link.dataset.tlMonaco = "style";
    link.addEventListener("load", () => resolve(), { once: true });
    link.addEventListener("error", () => reject(new Error("Monaco stylesheet failed to load")), { once: true });
    document.head.appendChild(link);
  });

  const loadModule = () => new Promise((resolve, reject) => {
    if (globalThis.TLMonaco?.editor) return resolve(globalThis.TLMonaco);
    const existing = document.querySelector('script[data-tl-monaco="module"]');
    if (existing) {
      existing.addEventListener("load", () => resolve(globalThis.TLMonaco), { once: true });
      existing.addEventListener("error", () => reject(new Error("Monaco module failed to load")), { once: true });
      return;
    }
    const script = document.createElement("script");
    script.type = "module";
    script.src = "/monaco-editor.js";
    script.dataset.tlMonaco = "module";
    script.addEventListener("load", () => {
      if (!globalThis.TLMonaco?.editor) return reject(new Error("Monaco module loaded without editor API"));
      resolve(globalThis.TLMonaco);
    }, { once: true });
    script.addEventListener("error", () => reject(new Error("Monaco module failed to load")), { once: true });
    document.head.appendChild(script);
  });

  const modelUri = (monaco: any, path: any) => {
    const encoded = String(path || "").replace(/\\/g, "/").split("/").filter(Boolean).map(encodeURIComponent).join("/");
    return monaco.Uri.parse(`tl-studio://workspace/${encoded || "untitled"}`);
  };

  const syncSelectionToFallback = () => {
    const editor = state.editor;
    const textarea = state.textarea;
    const model = editor?.getModel?.();
    const selection = editor?.getSelection?.();
    if (!textarea || !model || !selection) return;
    try {
      const start = model.getOffsetAt(selection.getStartPosition());
      const end = model.getOffsetAt(selection.getEndPosition());
      textarea.setSelectionRange(start, end);
      textarea.dispatchEvent(new Event("select", { bubbles: true }));
    } catch (_) {}
  };

  const ensureEditor = async () => {
    if (state.editor) return state.editor;
    if (state.loading) return state.loading;

    state.surface = document.getElementById("fileEditorSurface");
    state.textarea = document.getElementById("fileEditor");
    if (!state.surface || !state.textarea) throw new Error("Workspace editor surface is unavailable");

    state.loading = (async () => {
      state.surface.classList.add("monaco-loading");
      installWorkerFactory();
      const host = document.createElement("div");
      host.id = "monacoEditor";
      host.className = "monaco-editor-host";
      host.setAttribute("aria-label", "Code editor");
      state.surface.appendChild(host);
      state.host = host;

      try {
        const [, monaco] = await Promise.all([loadStylesheet(), loadModule()]);
        state.monaco = monaco;
        state.editor = monaco.editor.create(host, {
          model: null,
          theme: currentTheme(),
          automaticLayout: true,
          fontSize: 12,
          lineHeight: 20,
          fontLigatures: false,
          minimap: { enabled: false },
          stickyScroll: { enabled: false },
          scrollBeyondLastLine: false,
          smoothScrolling: false,
          glyphMargin: false,
          folding: true,
          lineNumbersMinChars: 3,
          renderWhitespace: "selection",
          renderLineHighlight: "line",
          bracketPairColorization: { enabled: false },
          guides: { indentation: false, bracketPairs: false },
          overviewRulerLanes: 0,
          hideCursorInOverviewRuler: true,
          fixedOverflowWidgets: true,
          padding: { top: 12, bottom: 20 },
          wordWrap: "off",
          tabSize: 2,
          insertSpaces: true,
          contextmenu: true,
          quickSuggestions: { other: true, comments: false, strings: false },
          suggest: { showWords: true },
          accessibilitySupport: "auto",
        });

        state.editor.onDidChangeModelContent(() => {
          if (state.suppressChange || !state.textarea) return;
          const model = state.editor.getModel();
          if (!model) return;
          state.textarea.value = model.getValue();
          syncSelectionToFallback();
          state.textarea.dispatchEvent(new Event("input", { bubbles: true }));
        });
        state.editor.onDidChangeCursorSelection(syncSelectionToFallback);
        state.editor.addAction({
          id: "tl-studio-save",
          label: "Save file",
          keybindings: [monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS],
          run: () => document.getElementById("saveFile")?.click(),
        });

        state.textarea.setAttribute("aria-hidden", "true");
        state.surface.classList.add("monaco-ready");
        state.surface.classList.remove("monaco-loading");
        requestAnimationFrame(() => {
          state.editor?.layout?.();
          requestAnimationFrame(() => state.editor?.layout?.());
        });
        return state.editor;
      } catch (error) {
        host.remove();
        state.host = null;
        state.surface.classList.remove("monaco-loading", "monaco-ready");
        state.textarea.removeAttribute("aria-hidden");
        state.loading = null;
        console.warn("TL Studio enhanced editor unavailable; keeping lightweight editor fallback.", error);
        throw error;
      }
    })();

    return state.loading;
  };

  const getOrCreateModel = (path: any, content: any) => {
    const monaco = state.monaco;
    const key = pathKey(path);
    let model = state.models.get(key);
    const language = languageForPath(path);
    if (!model) {
      model = monaco.editor.createModel(String(content ?? ""), language, modelUri(monaco, path));
      state.models.set(key, model);
    } else {
      if (model.getLanguageId() !== language) monaco.editor.setModelLanguage(model, language);
      const next = String(content ?? "");
      if (model.getValue() !== next) {
        state.suppressChange = true;
        try { model.setValue(next); }
        finally { state.suppressChange = false; }
      }
    }
    return model;
  };

  const activateRender = (detail: any) => {
    if (!state.editor || !state.monaco) return;
    const path = String(detail?.path || "");
    if (!path) {
      state.activePath = "";
      state.editor.setModel(null);
      return;
    }
    if (detail?.viewOnly) {
      state.activePath = path;
      state.editor.setModel(null);
      return;
    }
    const currentPath = document.getElementById("fileEditorPath")?.textContent || "";
    const content = pathKey(currentPath) === pathKey(path) && state.textarea
      ? state.textarea.value
      : String(detail?.content ?? "");
    const model = getOrCreateModel(path, content);
    state.activePath = path;
    if (state.editor.getModel() !== model) state.editor.setModel(model);
    window.setTimeout(() => state.editor?.focus?.(), 0);
  };

  const revealInEditor = (detail: any) => {
    if (!state.editor || !state.monaco || pathKey(detail?.path) !== pathKey(state.activePath)) return false;
    const model = state.editor.getModel();
    if (!model) return false;
    const line = Math.max(1, Math.min(model.getLineCount(), Number(detail?.line || 1)));
    const maxColumn = model.getLineMaxColumn(line);
    const column = Math.max(1, Math.min(maxColumn, Number(detail?.column || 1)));
    const start = { lineNumber: line, column };
    const startOffset = model.getOffsetAt(start);
    const end = model.getPositionAt(Math.min(model.getValueLength(), startOffset + String(detail?.match || "").length));
    const range = new state.monaco.Range(line, column, end.lineNumber, end.column);
    state.editor.setSelection(range);
    state.editor.revealRangeInCenterIfOutsideViewport(range);
    state.editor.focus();
    syncSelectionToFallback();
    return true;
  };

  const handleRender = async (detail: any) => {
    state.pendingRender = detail || {};
    if (!detail?.path || detail?.viewOnly) {
      if (state.editor) activateRender(detail);
      return;
    }
    try {
      await ensureEditor();
      if (state.pendingRender) activateRender(state.pendingRender);
      if (state.pendingReveal) {
        const reveal = state.pendingReveal;
        state.pendingReveal = null;
        revealInEditor(reveal);
      }
    } catch (_) {
      // The textarea fallback remains fully functional.
    }
  };

  window.addEventListener("tl-studio:editor-render", (event) => handleRender(event.detail));
  window.addEventListener("tl-studio:editor-tabs", (event) => {
    const keep = new Set((event.detail?.paths || []).map(pathKey));
    for (const [key, model] of state.models) {
      if (keep.has(key)) continue;
      if (state.editor?.getModel?.() === model) state.editor.setModel(null);
      model.dispose();
      state.models.delete(key);
    }
  });
  window.addEventListener("tl-studio:editor-reveal", async (event) => {
    state.pendingReveal = event.detail;
    if (revealInEditor(event.detail)) state.pendingReveal = null;
    else if (event.detail?.path) {
      try {
        await ensureEditor();
        if (state.pendingRender) activateRender(state.pendingRender);
        if (revealInEditor(event.detail)) state.pendingReveal = null;
      } catch (_) {}
    }
  });

  new MutationObserver(() => {
    if (state.editor && state.monaco) state.monaco.editor.setTheme(currentTheme());
  }).observe(document.documentElement, { attributes: true, attributeFilter: ["data-resolved-theme"] });
})();
