"use strict";

const assert = require("node:assert/strict");
const vm = require("node:vm");
const { loadBrowserModule } = require("./browser-source-harness.cjs");

class FakeElement {
  constructor(tag = "div") {
    this.tagName = tag.toUpperCase();
    this.id = "";
    this.className = "";
    this.textContent = "";
    this.dataset = {};
    this.children = [];
    this.disabled = false;
    this.isConnected = true;
  }
  appendChild(child) { this.children.push(child); return child; }
  append(...children) { this.children.push(...children); }
  addEventListener() {}
}

const source = loadBrowserModule("diagnostics-ui.ts");
const RESUME_PROMPT = "Continue the current task from the existing workspace state. Inspect what is already complete, do not repeat finished work, and finish the user's latest request.";

const originalUser = (created = 1000) => ({
  role: "user",
  createdAt: created,
  text: "Build the site",
  activities: [],
});
const resumeUser = (created = 3000) => ({
  role: "user",
  createdAt: created,
  text: RESUME_PROMPT,
  activities: [],
});
const assistantStep = (modelID, created = 1500, completed = 2000) => ({
  role: "assistant",
  createdAt: created,
  completedAt: completed,
  activities: [{
    kind: "model",
    status: "completed",
    startAt: created,
    endAt: completed,
    model: { providerID: "kilo", id: modelID },
  }],
});
const assistantError = (created = 2500) => ({
  role: "assistant",
  createdAt: created,
  completedAt: created + 10,
  error: { message: "Upstream idle timeout exceeded" },
  activities: [],
});

const K = {
  __diagnosticsUiInstalled: false,
  renderMessages: () => {},
  state: {
    session: { id: "session-1" },
    activeSessions: {},
    messages: [],
    sending: false,
  },
  els: { conversation: null, prompt: { value: "" } },
  api: { hosted: { providerID: "kilo" } },
};

const document = {
  head: new FakeElement("head"),
  createElement: (tag) => new FakeElement(tag),
  getElementById: () => null,
};

const context = vm.createContext({
  K,
  window: { setTimeout },
  document,
  console,
  Date,
  JSON,
  Promise,
  setTimeout,
});
vm.runInContext(source, context, { filename: "diagnostics-ui.ts" });

const hooks = K.__statusDiagnostics;
assert.ok(hooks, "diagnostics hooks should be installed");

const inheritedModelMessages = [
  originalUser(),
  assistantStep("nvidia/nemotron-3-ultra-550b-a55b:free"),
  assistantError(),
  resumeUser(),
  assistantError(3500),
];
assert.equal(hooks.attemptNumberAt(4, inheritedModelMessages), 2);
assert.equal(
  hooks.lastRecordedModelInAttempt(4, inheritedModelMessages),
  null,
  "a timeout in a new Resume attempt must not inherit a routed model from the previous attempt",
);

const currentAttemptModelMessages = [
  ...inheritedModelMessages.slice(0, 4),
  assistantStep("qwen/qwen3-coder:free", 3200, 3400),
  assistantError(3600),
];
const current = hooks.lastRecordedModelInAttempt(5, currentAttemptModelMessages);
assert.equal(current?.label, "qwen/qwen3-coder:free");
assert.equal(hooks.attemptNumberAt(5, currentAttemptModelMessages), 2);

const now = 1_000_000;
K.state.messages = currentAttemptModelMessages;
K.state.activeSessions["session-1"] = {
  state: "retrying",
  active: true,
  attempt: 3,
  message: "Upstream idle timeout exceeded",
  nextAt: now + 5_000,
};
let snapshot = hooks.workingStatusSnapshot(now, currentAttemptModelMessages);
assert.equal(snapshot.type, "retry");
assert.match(snapshot.meta, /provider retry 3/);
assert.match(snapshot.meta, /next in 5s/);
assert.equal(snapshot.detail, "Upstream model idle timeout");
assert.equal(snapshot.model, "qwen/qwen3-coder:free");

const staleMessages = [
  originalUser(now - 600_000),
  assistantStep("qwen/qwen3-coder:free", now - 400_000, now - 300_000),
];
K.state.activeSessions["session-1"] = { state: "running", active: true };
snapshot = hooks.workingStatusSnapshot(now, staleMessages);
assert.equal(snapshot.type, "busy");
assert.equal(snapshot.stale, true);
assert.match(snapshot.title, /Still busy/);
assert.match(snapshot.meta, /no new session activity for 5m/);

async function testRecovery() {
  let abortCalls = 0;
  let sendCalls = 0;
  let errorMessage = "";

  K.state.messages = staleMessages;
  K.state.activeSessions["session-1"] = { state: "running", active: true };
  K.api = {
    hosted: { providerID: "kilo" },
    sessions: {
      abort: async (sessionID, options) => {
        abortCalls += 1;
        assert.equal(sessionID, "session-1");
        assert.equal(options.scope, "session");
        K.state.activeSessions[sessionID] = { state: "idle", active: false };
      },
    },
  };
  K.loadActiveSessions = async () => {};
  K.isSessionRunning = (sessionID) => K.state.activeSessions[sessionID]?.active === true;
  K.stopSessionPolling = () => {};
  K.loadMessages = async () => [];
  K.loadAttention = async () => {};
  K.resizePrompt = () => {};
  K.showError = (message) => { errorMessage = message || ""; };
  K.sendPrompt = async () => {
    sendCalls += 1;
    assert.equal(K.els.prompt.value, RESUME_PROMPT);
  };

  const recovered = await hooks.recoverStalledSession();
  assert.equal(recovered, true);
  assert.equal(abortCalls, 1, "recovery should interrupt the stuck runtime session exactly once");
  assert.equal(sendCalls, 1, "recovery should resume the task exactly once after the runtime becomes idle");
  assert.equal(errorMessage, "");
}

testRecovery()
  .then(() => console.log("status diagnostics regressions: ok"))
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
