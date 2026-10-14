"use strict";

const assert = require("node:assert/strict");
const vm = require("node:vm");
const { loadBrowserModule } = require("./browser-source-harness.cjs");

const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
};
class FakeElement {
  constructor(tag = "div") {
    this.tagName = tag;
    this.className = "";
    this.children = [];
    this.handlers = {};
    this.textContent = "";
    this.title = "";
    this.value = "";
    this.disabled = false;
    this.src = "";
    this.style = {};
    this.attributes = {};
    this.classList = {
      contains: name => this.className.split(/\s+/).includes(name),
      add: name => { if (!this.classList.contains(name)) this.className = [this.className, name].filter(Boolean).join(" "); },
      remove: name => { this.className = this.className.split(/\s+/).filter(x => x !== name).join(" "); },
      toggle: (name, enabled) => {
        const state = enabled === undefined ? !this.classList.contains(name) : !!enabled;
        if (state) this.classList.add(name);
        else this.classList.remove(name);
        return state;
      },
    };
  }
  appendChild(element) { this.children.push(element); return element; }
  append(...elements) { this.children.push(...elements); }
  insertBefore(element) { this.children.unshift(element); }
  setAttribute(name, value) { this.attributes[name] = value; }
  removeAttribute(name) { delete this.attributes[name]; if (name === "src") this.src = ""; }
  addEventListener(name, fn) { this.handlers[name] = fn; }
  focus() {}
  setSelectionRange(start, end) { this.selectionStart = start; this.selectionEnd = end; }
  click() { return this.handlers.click?.(); }
  querySelector(selector) {
    if (selector === "strong") return this.strong ||= new FakeElement("strong");
    if (selector === ".file-preview") return this.filePreview ||= new FakeElement();
    return null;
  }
  querySelectorAll() { return []; }
}

const fixture = (moduleName, setup) => {
  const elementIDs = [
    "filesButton", "filesPanel", "closeFiles", "refreshFiles", "filesUp",
    "filesBreadcrumb", "filesList", "changesPanel", "changesButton",
    "previewStart", "previewStop", "previewReload", "previewExternal",
    "previewClose", "previewStatus", "previewKind", "previewURL",
    "previewEntryRow", "previewEntry", "previewEmpty", "previewHint",
    "previewLogs", "previewFrame",
    "fileTabs", "fileEditorEmpty", "filePreviewOnly", "filePreviewOnlyTitle",
    "filePreviewOnlyHint", "fileEditorSurface", "fileLineNumbers", "fileEditor",
    "fileEditorTitle", "fileEditorPath", "fileEditorMeta", "askFile",
    "reloadFile", "saveFile", "fileEditorBody", "editorStatus", "editorCursor",
  ];
  const elements = Object.fromEntries(elementIDs.map(id => [id, new FakeElement()]));
  const toolbar = new FakeElement(), main = new FakeElement();
  const listeners = new Map();
  const document = {
    head: new FakeElement(),
    createElement: tag => new FakeElement(tag),
    getElementById: id => elements[id] || null,
    querySelector: selector => {
      if (selector === ".toolbar-controls") return toolbar;
      if (selector === ".main-pane") return main;
      return null;
    },
    addEventListener() {},
  };
  const window = {
    location: { href: "http://127.0.0.1:8765/" },
    addEventListener: (name, cb) => listeners.set(name, cb),
    setTimeout, clearTimeout, setInterval, clearInterval,
    dispatchEvent() {},
    getComputedStyle: () => ({ lineHeight: "20px" }),
    open() {},
    fetch: null,
  };
  const K = {
    state: { local: { project: "/old" }, activeEditorPath: "", preview: {} },
    // This isolated Explorer/Preview fixture models an already-authorized
    // browser. Cold-start ordering and denied credentials are covered by
    // check-browser-auth-startup.cjs.
    ensureLocalAuth: async () => {},
    handleLiveEvent() {},
    afterProjectChange: async () => {},
    showError(message) { if (message) throw Error("Unexpected UI error: " + message); },
  };
  const extra = setup({ elements, toolbar, main, listeners, window, K }) || {};
  const context = vm.createContext({
    K, document, window, console, fetch: window.fetch,
    URL, URLSearchParams, Request, Event, CustomEvent: class { constructor(type, args) { this.type = type; this.detail = args?.detail; } },
    setTimeout, clearTimeout, setInterval, clearInterval,
  });
  vm.runInContext(loadBrowserModule(moduleName), context, { filename: moduleName });
  return { K, elements, listeners, window, ...extra };
};

async function testExplorer() {
  const pendingA = deferred(), pendingB = deferred();
  const { K } = fixture("files.ts", ({ K }) => {
    K.request = url => {
      assert.match(url, /^\/local\/files/);
      return K.state.local.project === "/old" ? pendingA.promise : pendingB.promise;
    };
  });
  const oldLoad = K.openWorkspace();
  assert.equal(K.state.filesLoading, true);
  K.state.local = { project: "/new" };
  const newLoad = K.afterProjectChange();
  pendingB.resolve({ path: "", entries: [{ type: "file", path: "fresh.html", name: "fresh.html" }] });
  await newLoad;
  assert.equal(K.state.filesEntries[0]?.name, "fresh.html", "new project must load automatically");
  pendingA.resolve({ path: "", entries: [] });
  await oldLoad;
  assert.equal(K.state.filesEntries[0]?.name, "fresh.html", "stale old root must not clear new project");
  assert.equal(K.state.filesLoading, false);
}

async function testPreview() {
  let entry = "index.html";
  const requests = [];
  const selected = (name) => ({
    available: true, running: true, kind: "file", entry: name,
    url: name === "index.html"
      ? "http://127.0.0.1:9876/"
      : "http://127.0.0.1:9876/.tl-preview/text?file=" + encodeURIComponent(name),
    entries: [{ path: "index.html", name: "HTML", capabilityID: "web.html" }],
  });
  const { K, elements, listeners, window } = fixture("preview.ts", ({ K, window }) => {
    K.state.activeEditorPath = "tl";
    const get = async (input, init = {}) => {
      const url = new URL(String(input), "http://127.0.0.1:8765/");
      const method = String(init.method || "GET").toUpperCase();
      requests.push({ path: url.pathname, method, entry: url.searchParams.get("entry") });
      const body = url.pathname === "/local/preview/capabilities"
        ? { version: 1, capabilities: [
            { id: "web.html", kind: "html", extensions: [".html"] },
            { id: "document.source", kind: "text", extensions: [".css", ".js"] },
          ]}
        : url.pathname === "/local/preview"
          ? (method === "POST" ? selected(entry = url.searchParams.get("entry") || "index.html") : selected(entry))
          : {};
      return { ok: true, status: 200, headers: { get: () => "application/json" }, json: async () => body };
    };
    window.fetch = get;
  });
  assert.equal(typeof K.preview.open, "function");
  await K.preview.open();
  await new Promise(resolve => setTimeout(resolve, 30));
  assert.equal(K.state.preview.snapshot?.entry, "tl", "active extensionless file must override root index");
  assert.ok(requests.some(x => x.method === "POST" && x.entry === "tl"));
  assert.match(elements.previewFrame.src, /file=tl$/, "active tl text file was not shown");

  K.state.activeEditorPath = "styles.css";
  await listeners.get("tl-studio:editor-render")({ detail: { path: "styles.css" } });
  await new Promise(resolve => setTimeout(resolve, 100));
  assert.equal(K.state.preview.snapshot?.entry, "styles.css", "tab change must switch preview");
  assert.match(elements.previewFrame.src, /styles\.css$/);

  K.state.activeEditorPath = "data.bin";
  const priorRequestCount = requests.length;
  await listeners.get("tl-studio:editor-render")({ detail: { path: "data.bin" } });
  assert.equal(elements.previewStatus.textContent, "Unsupported");
  assert.equal(elements.previewFrame.classList.contains("hidden"), true);
  assert.equal(elements.previewFrame.src, "", "stale HTML preview must not remain visible");
  assert.equal(elements.previewStart.disabled, true);
  assert.equal(requests.length, priorRequestCount, "unsupported file must not silently request index");

  K.state.activeEditorPath = "index.html";
  await listeners.get("tl-studio:editor-render")({ detail: { path: "index.html" } });
  await new Promise(resolve => setTimeout(resolve, 100));
  assert.equal(K.state.preview.snapshot?.entry, "index.html");
  assert.ok(elements.previewFrame.src.endsWith("/"), "restoring selected HTML tab must show its page");
}


async function testPreviewWaitsForAuthorization() {
  const authorization = deferred();
  const requests = [];
  const { K } = fixture("preview.ts", ({ K, window }) => {
    K.ensureLocalAuth = () => authorization.promise;
    window.fetch = async input => {
      requests.push(String(input));
      return {
        ok: true, status: 200,
        headers: { get: () => "application/json" },
        json: async () => ({ version: 1, capabilities: [] }),
      };
    };
  });
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(requests.length, 0, "Preview dispatched an API call before authorization completed");
  authorization.resolve();
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.deepEqual(requests, ["/local/preview/capabilities"],
    "Preview must load its registry only after authorization completes");
  assert.equal(K.state.preview.registry?.version, 1);
}

async function testEditorSaveIsolation() {
  const saveReply = deferred();
  let submitted = null;
  const { K, elements } = fixture("files.ts", ({ K, window }) => {
    K.request = async url => url.startsWith("/local/files")
      ? { path: "", entries: [{ type: "file", name: "note.txt", path: "note.txt" }] }
      : { path: "note.txt", content: "original", sha256: "sha-old", size: 8 };
    window.fetch = async (_url, options) => {
      submitted = JSON.parse(options.body);
      return saveReply.promise;
    };
  });
  await K.openWorkspaceFileAt({ path: "note.txt" });
  const tab = K.state.editorTabs[0];
  assert.ok(tab, "editor must open the selected project file");
  tab.content = "snapshot";
  const saving = elements.saveFile.click();
  assert.equal(submitted.content, "snapshot");
  // The user continues typing while the disk write is in flight.
  tab.content = "snapshot + later edit";
  saveReply.resolve({
    ok: true, status: 200, headers: { get: () => "application/json" },
    json: async () => ({ path: "note.txt", content: "snapshot", sha256: "sha-new", size: 8 }),
  });
  await saving;
  assert.equal(tab.savedContent, "snapshot", "saved disk snapshot must advance");
  assert.equal(tab.sha256, "sha-new", "future saves must use the latest disk revision");
  assert.equal(tab.content, "snapshot + later edit", "late save response must not destroy new typing");
}

async function testOldProjectEditorReadIsDiscarded() {
  const staleRead = deferred();
  let started = false;
  const { K } = fixture("files.ts", ({ K }) => {
    K.request = url => {
      if (url.startsWith("/local/files")) return Promise.resolve({ path: "", entries: [] });
      started = true;
      return staleRead.promise;
    };
  });
  const opening = K.openWorkspaceFileAt({ path: "note.txt" });
  for (let i = 0; i < 10 && !started; i++) await Promise.resolve();
  assert.equal(started, true, "old project file read did not begin");
  K.state.local = { project: "/new" };
  await K.afterProjectChange();
  staleRead.resolve({ path: "note.txt", content: "old project data", sha256: "old" });
  await opening;
  assert.equal(K.state.editorTabs.length, 0, "late old-project file must not open in new project");
}

async function main() {
  await testExplorer();
  await testPreview();
  await testPreviewWaitsForAuthorization();
  await testEditorSaveIsolation();
  await testOldProjectEditorReadIsDiscarded();
  console.log("Explorer, Preview and Editor async state isolation: ok");
}
main().catch(error => { console.error(error); process.exitCode = 1; });
