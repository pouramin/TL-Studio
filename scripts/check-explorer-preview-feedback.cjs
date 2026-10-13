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
  click() { return this.handlers.click?.(); }
  querySelector(selector) { return selector === "strong" ? (this.strong ||= new FakeElement("strong")) : null; }
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
    open() {},
    fetch: null,
  };
  const K = {
    state: { local: { project: "/old" }, activeEditorPath: "", preview: {} },
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


async function main() {
  await testExplorer();
  await testPreview();
  console.log("Explorer project switches and active-tab Preview: ok");
}
main().catch(error => { console.error(error); process.exitCode = 1; });
