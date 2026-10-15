"use strict";

const assert = require("node:assert/strict");
const vm = require("node:vm");
const { transformSync } = require("esbuild");
const { loadBrowserModule, readBrowserTypeScript } = require("./browser-source-harness.cjs");

const deferred = () => {
  let resolve, reject;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
};

const scopeModule = transformSync(readBrowserTypeScript("view-scope.ts"), {
  loader: "ts", format: "cjs", target: "es2020",
}).code;
const scopeExports = { exports: {} };
vm.runInNewContext(scopeModule, { module: scopeExports, exports: scopeExports.exports });
const makeScope = scopeExports.exports.createViewScope;

class FakeNode {
  constructor() {
    this.textContent = "";
    this.className = "";
    this.value = "";
    this.title = "";
    this.disabled = false;
    this.open = false;
    this.children = [];
    this.handlers = {};
    this.style = {};
    this.classList = { add() {}, remove() {}, toggle() {} };
  }
  append(...items) { this.children.push(...items); }
  appendChild(item) { this.children.push(item); return item; }
  focus() {}
  close() { this.open = false; }
  showModal() { this.open = true; }
  addEventListener(name, fn) { this.handlers[name] = fn; }
  querySelectorAll() { return []; }
}

function createFixture() {
  const scope = makeScope();
  const nodes = {};
  for (const name of [
    "prompt", "sendButton", "attentionDialog", "attentionTitle", "attentionBody",
    "attentionActions", "conversation", "emptyState", "backendStatus",
    "projectName", "projectPath", "versionLabel", "errorBanner", "sessions",
    "sessionTitle", "sessionMeta", "agentSelect", "modelSelect", "accountButton",
    "authDialog", "authCode"
  ]) nodes[name] = new FakeNode();
  nodes.agentSelect.value = "code";
  nodes.modelSelect.value = "";
  let timer = 1;
  const K = {
    viewScope: scope,
    state: {
      local: { project: "/project-a" }, session: { id: "A" },
      sessions: [], messages: [], activeSessions: {}, revision: 0,
      sending: false, agents: [], providers: [], providerAccounts: [],
      models: [], connectedProviders: new Set(), providerDefaults: {},
      sessionPolling: null, eventSource: null, fallbackPolling: null,
      attentionKey: "",
    },
    els: nodes,
    api: {
      sessionView: {
        messages: async () => [],
        list: async () => [],
        status: async () => ({}),
        get: async (id) => ({ id }),
      },
      sessionCommands: { create: async () => ({ id: "created" }), run: async () => {} },
      permissions: { list: async () => [], reply: async () => {} },
      questions: { list: async () => [], reply: async () => {}, reject: async () => {} },
      agents: async () => ({ agents: [] }),
      providerState: async () => ({ all: [], connected: new Set(), defaults: {} }),
    },
    isSessionRunning: () => false,
  };
  const context = vm.createContext({
    K,
    console,
    document: { createElement: () => new FakeNode() },
    window: {
      setInterval: () => timer++, clearInterval: () => {},
      setTimeout: () => timer++, clearTimeout: () => {},
    },
    Date, Promise, JSON,
    requestAnimationFrame: () => {},
  });
  vm.runInContext(loadBrowserModule("core.ts"), context, { filename: "core.ts" });
  vm.runInContext(loadBrowserModule("chat.ts"), context, { filename: "chat.ts" });
  vm.runInContext(loadBrowserModule("attention.ts"), context, { filename: "attention.ts" });
  K.renderSessions = () => {};
  K.renderSessionHeader = () => {};
  K.renderMessages = () => {};
  K.renderAgents = () => {};
  K.renderModels = () => {};
  K.renderAccount = () => {};
  K.syncSelectors = () => {};
  K.showConversation = () => {};
  K.loadAttention ??= async () => {};
  K.selectedModel = () => undefined;
  K.startEvents = () => {};
  K.stopEvents = () => {};
  return { K, nodes, scope };
}

async function main() {
  const testScope = makeScope();
  const a = testScope.captureSession();
  const project = testScope.captureProject();
  testScope.enterSession();
  assert.equal(testScope.isProjectCurrent(project), true);
  assert.equal(testScope.isSessionCurrent(a), false);
  const session = testScope.captureSession();
  testScope.enterProject();
  assert.equal(testScope.isProjectCurrent(project), false);
  assert.equal(testScope.isSessionCurrent(session), false);

  // Slow A replies after B is already selected: B must remain visible.
  {
    const { K, scope } = createFixture();
    const slow = deferred();
    K.api.sessionView.messages = (id) => id === "A" ? slow.promise : Promise.resolve([{ id: "B-message" }]);
    const pendingA = K.loadMessages();
    scope.enterSession();
    K.state.session = { id: "B" };
    await K.loadMessages();
    slow.resolve([{ id: "stale-A" }]);
    await pendingA;
    assert.equal(K.state.messages.length, 1);
    assert.equal(K.state.messages[0].id, "B-message", "late session A must not replace B");
  }

  // A project-wide list response must not replace a newer project's list/status.
  {
    const { K, scope } = createFixture();
    const oldList = deferred();
    const oldStatus = deferred();
    K.api.sessionView.list = () => oldList.promise;
    K.api.sessionView.status = () => oldStatus.promise;
    const previousList = K.loadSessions();
    const previousStatus = K.loadActiveSessions();
    scope.enterProject();
    K.state.local = { project: "/project-b" };
    K.state.sessions = [{ id: "session-B" }];
    K.state.activeSessions = { B: { active: true } };
    oldList.resolve([{ id: "session-A" }]);
    oldStatus.resolve({ A: { active: true } });
    await Promise.all([previousList, previousStatus]);
    assert.equal(K.state.sessions[0].id, "session-B");
    assert.ok(K.state.activeSessions.B);
    assert.equal(K.state.activeSessions.A, undefined);
  }

  // A stale permission request must neither open the dialog nor send an
  // authorization to another session after switching.
  {
    const { K, scope, nodes } = createFixture();
    const pendingPermission = deferred();
    K.api.permissions.list = () => pendingPermission.promise;
    const pendingLoad = K.loadAttention();
    scope.enterSession();
    K.state.session = { id: "B" };
    pendingPermission.resolve([{ id: "old-permission", permission: "files.write" }]);
    await pendingLoad;
    assert.equal(nodes.attentionDialog.open, false, "stale permission was displayed");
    assert.equal(nodes.attentionActions.children.length, 0);
  }
  {
    const { K, scope, nodes } = createFixture();
    const calls = [];
    K.api.permissions.list = async () => [{ id: "p-A", permission: "files.write" }];
    K.api.permissions.reply = async (...args) => calls.push(args);
    await K.loadAttention();
    assert.equal(nodes.attentionDialog.open, true);
    const allow = nodes.attentionActions.children.find(n => n.textContent === "Allow once");
    assert.ok(allow);
    scope.enterSession();
    K.state.session = { id: "B" };
    await allow.handlers.click();
    assert.deepEqual(calls, [], "old-session approval must not be submitted against B");
  }

  // A prompt started for A may finish after switching to B, but must not
  // schedule polling, render old results or unset B's in-flight state.
  {
    const { K, nodes } = createFixture();
    const pendingRun = deferred();
    K.api.sessionCommands.run = () => pendingRun.promise;
    K.api.sessionView.messages = async () => [];
    let polls = 0;
    K.startSessionPolling = () => { polls += 1; };
    nodes.prompt.value = "Edit the file";
    const oldSend = K.sendPrompt();
    assert.equal(K.state.sending, true);
    await K.selectSession({ id: "B" });
    K.state.sending = true; // A new B run is in progress.
    pendingRun.resolve();
    await oldSend;
    assert.equal(polls, 0, "old prompt must not start polling B");
    assert.equal(K.state.session.id, "B");
    assert.equal(K.state.sending, true, "old prompt must not overwrite B's send state");
  }

  // A pending create request cannot silently run a prompt against a
  // different session when the user navigates away.
  {
    const { K, nodes } = createFixture();
    K.state.session = null;
    nodes.prompt.value = "Create something";
    const pendingCreate = deferred();
    K.api.sessionCommands.create = () => pendingCreate.promise;
    let dispatched = 0;
    K.api.sessionCommands.run = async () => { dispatched += 1; };
    const oldSend = K.sendPrompt();
    K.newSession();
    pendingCreate.resolve({ id: "ghost-A" });
    await oldSend;
    assert.equal(K.state.session, null);
    assert.equal(dispatched, 0, "abandoned create must not trigger a tool run");
  }

  // A fast Send must wait for FileReader and keep the common limits/session
  // guards, rather than taking a separate attachment-only submission path.
  {
    const { K, nodes } = createFixture();
    const pendingRead = deferred();
    K.waitForAttachments = () => pendingRead.promise;
    const calls = [];
    K.api.sessionCommands.run = async (...args) => calls.push(args);
    K.api.sessionView.messages = async () => [];
    K.startSessionPolling = () => {};
    let cleared = false;
    K.clearAttachments = () => { cleared = true; K.state.attachments = []; };
    nodes.prompt.value = "What is in the attachment?";
    const sending = K.sendPrompt();
    assert.equal(calls.length, 0, "send raced ahead of the file read");
    K.state.attachments = [{ name: "test.txt", mime: "text/plain", url: "data:text/plain;base64,bWFya2Vy" }];
    pendingRead.resolve();
    await sending;
    assert.equal(calls.length, 1);
    assert.equal(calls[0][1].parts[1].filename, "test.txt");
    assert.equal(calls[0][1].parts[1].url, "data:text/plain;base64,bWFya2Vy");
    assert.equal(calls[0][1].maxToolRounds, 24);
    assert.equal(cleared, true);
  }
  {
    const { K, nodes } = createFixture();
    const pendingRead = deferred();
    K.waitForAttachments = () => pendingRead.promise;
    let dispatched = false;
    K.api.sessionCommands.run = async () => { dispatched = true; };
    nodes.prompt.value = "Analyze this upload";
    const sending = K.sendPrompt();
    K.newSession();
    pendingRead.resolve();
    await sending;
    assert.equal(dispatched, false, "pending attachment sent into a different session");
  }

  console.log("frontend navigation state isolation: ok");
}

main().catch(err => { console.error(err); process.exitCode = 1; });
