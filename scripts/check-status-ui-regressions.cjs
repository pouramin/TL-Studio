"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

class FakeClassList {
  constructor(element) {
    this.element = element;
    this.values = new Set(String(element.className || "").split(/\s+/).filter(Boolean));
  }

  add(...values) {
    values.forEach((value) => this.values.add(value));
    this.element.className = [...this.values].join(" ");
  }

  remove(...values) {
    values.forEach((value) => this.values.delete(value));
    this.element.className = [...this.values].join(" ");
  }

  contains(value) {
    return this.values.has(value);
  }
}

class FakeElement {
  constructor(tag = "div") {
    this.tagName = tag.toUpperCase();
    this.className = "";
    this.textContent = "";
    this.title = "";
    this.type = "";
    this.value = "";
    this.dataset = {};
    this.children = [];
    this.listeners = new Map();
    this.lookup = new Map();
    this.attributes = new Map();
    this.style = {};
    this.removed = false;
    this.classList = new FakeClassList(this);
  }

  append(...items) {
    this.children.push(...items);
  }

  appendChild(item) {
    this.children.push(item);
    return item;
  }

  insertBefore(item, before) {
    const index = this.children.indexOf(before);
    if (index < 0) this.children.push(item);
    else this.children.splice(index, 0, item);
    return item;
  }

  remove() {
    this.removed = true;
  }

  addEventListener(type, handler) {
    this.listeners.set(type, handler);
  }

  setAttribute(name, value) {
    this.attributes.set(name, String(value));
  }

  getAttribute(name) {
    return this.attributes.get(name) || null;
  }

  select() {}

  querySelector(selector) {
    if (selector === ".timeout-recovery") {
      return this.children.find((item) => item?.className === "timeout-recovery") || null;
    }
    if (selector === ".routed-model-meta") {
      return this.children.find((item) => item?.className === "routed-model-meta") || null;
    }
    return this.lookup.get(selector) || null;
  }
}

const repoRoot = path.resolve(__dirname, "..");
const statusUIPath = path.join(repoRoot, "cmd", "launcher", "web", "status-ui.js");
const source = fs.readFileSync(statusUIPath, "utf8");
const trailer = "\n})();";
const trailerIndex = source.lastIndexOf(trailer);
assert.notEqual(trailerIndex, -1, "status-ui.js must remain an IIFE so the regression harness can instrument it");

const instrumented = `${source.slice(0, trailerIndex)}\n  K.__statusUiRegression = {\n    RESUME_PROMPT,\n    exactUserText,\n    isResumeMessage,\n    hideResumeMessages,\n    turnGroups,\n    routedModelSteps,\n    modelRouteSummary,\n    attemptNumberAt,\n    lastRecordedModelBefore,\n    renderRoutedModels,\n    normalizePath,\n    samePath,\n    sessionDirectory,\n    usageCacheKey,\n    activeProjectSessions,\n    projectUsageSnapshot,\n    projectUsageCache,\n    timeoutKind,\n    addTimeoutRecovery,\n  };${source.slice(trailerIndex)}`;

const document = {
  createElement: (tag) => new FakeElement(tag),
  createElementNS: (_ns, tag) => new FakeElement(tag),
  execCommand: () => true,
  body: new FakeElement("body"),
};

const rootProject = "C:\\Users\\Lenovo\\Desktop\\Ktest";
const childProject = "C:\\Users\\Lenovo\\Desktop\\Ktest\\New folder";
const sessions = [
  { id: "root-current", directory: rootProject, time: { updated: 10 } },
  { id: "root-cached", directory: "c:/users/lenovo/desktop/KTEST/", time: { updated: 20 } },
  { id: "child-current", directory: childProject, time: { updated: 30 } },
  { id: "child-cached", directory: "c:/users/lenovo/desktop/ktest/new folder/", time: { updated: 40 } },
];

const rootMessages = [
  { info: { role: "user", time: { created: 1000 } }, parts: [{ type: "text", text: "root" }] },
  {
    info: {
      role: "assistant",
      time: { created: 1100, completed: 2000 },
      tokens: { input: 100, output: 50, reasoning: 10, cache: { read: 20, write: 5 } },
    },
    parts: [{ type: "text", text: "done" }],
  },
];

const childMessages = [
  { info: { role: "user", time: { created: 3000 } }, parts: [{ type: "text", text: "child" }] },
  {
    info: {
      role: "assistant",
      time: { created: 3100, completed: 5000 },
      tokens: { input: 600, output: 200, reasoning: 50, cache: { read: 40, write: 10 } },
    },
    parts: [{ type: "text", text: "done" }],
  },
];

const K = {
  renderMessages: () => {},
  state: {
    local: { platform: "windows", project: rootProject },
    sessions,
    session: sessions[0],
    messages: rootMessages,
    sending: false,
  },
  els: {
    conversation: null,
    prompt: new FakeElement("textarea"),
  },
  isSessionRunning: () => false,
  resizePrompt: () => {},
  sendPrompt: async () => {},
  showError: () => {},
  api: { hosted: { providerID: "kilo" }, sessions: { messages: async () => ({ data: [] }) } },
};

const context = vm.createContext({
  window: {
    KLU: K,
    setTimeout: () => 0,
    clearTimeout: () => {},
  },
  document,
  navigator: {},
  console,
});
vm.runInContext(instrumented, context, { filename: statusUIPath });

const hooks = K.__statusUiRegression;
assert.ok(hooks, "status-ui regression hooks were not injected");

// Windows path normalization must be exact, case-insensitive, slash-insensitive,
// and must not treat a nested project as the parent project.
assert.equal(hooks.samePath("C:\\Users\\Lenovo\\Desktop\\Ktest\\", "c:/users/lenovo/desktop/ktest"), true);
assert.equal(hooks.samePath(rootProject, childProject), false);
assert.deepEqual(
  Array.from(hooks.activeProjectSessions(), (session) => session.id),
  ["root-current", "root-cached"],
);

const cachedUsage = (tokens, requests, duration) => ({
  tokens,
  requests,
  duration,
  breakdown: { input: tokens, output: 0, reasoning: 0, cacheRead: 0, cacheWrite: 0 },
});
hooks.projectUsageCache.set(hooks.usageCacheKey(sessions[1]), { stamp: "20", usage: cachedUsage(315, 2, 1500) });
hooks.projectUsageCache.set(hooks.usageCacheKey(sessions[3]), { stamp: "40", usage: cachedUsage(100, 1, 500) });
assert.notEqual(
  hooks.usageCacheKey(sessions[1]),
  hooks.usageCacheKey(sessions[3]),
  "usage cache keys must include the normalized project directory",
);

const rootTotal = hooks.projectUsageSnapshot();
assert.equal(rootTotal.tokens, 500, "root project total must not include nested-project usage");
assert.equal(rootTotal.requests, 3);
assert.equal(rootTotal.complete, true);

K.state.local.project = childProject;
K.state.session = sessions[2];
K.state.messages = childMessages;
assert.deepEqual(
  Array.from(hooks.activeProjectSessions(), (session) => session.id),
  ["child-current", "child-cached"],
);
const childTotal = hooks.projectUsageSnapshot();
assert.equal(childTotal.tokens, 1000, "nested project total must not include parent-project usage");
assert.equal(childTotal.requests, 2);
assert.equal(childTotal.complete, true);

const resume = (created) => ({
  info: { role: "user", time: { created } },
  parts: [{ type: "text", text: hooks.RESUME_PROMPT }],
});
const assistant = (created, models = []) => ({
  info: { role: "assistant", time: { created, completed: created + 100 } },
  parts: models.map((modelID, index) => ({
    type: "step-finish",
    model: { providerID: "kilo", modelID },
    time: { start: created + index * 10, end: created + index * 10 + 5, elapsed: 5 },
    tokens: { input: 1, output: 1, reasoning: 0, cache: { read: 0, write: 0 } },
  })),
});

const retryMessages = [
  { info: { role: "user", time: { created: 10000 } }, parts: [{ type: "text", text: "build it" }] },
  assistant(10100, ["anthropic/claude-sonnet-4", "anthropic/claude-sonnet-4"]),
  resume(10300),
  assistant(10400, ["openai/gpt-5", "openai/gpt-5", "google/gemini-2.5-pro"]),
  resume(10600),
  assistant(10700, ["google/gemini-2.5-pro"]),
];
K.state.messages = retryMessages;
assert.equal(hooks.isResumeMessage(retryMessages[2]), true);
assert.equal(hooks.isResumeMessage(retryMessages[0]), false);
assert.equal(hooks.turnGroups().length, 1, "Resume prompts must remain part of the original visible user turn");
assert.equal(hooks.attemptNumberAt(1), 1);
assert.equal(hooks.attemptNumberAt(3), 2);
assert.equal(hooks.attemptNumberAt(5), 3);
assert.equal(
  hooks.modelRouteSummary(retryMessages[3]),
  "openai/gpt-5 ×2 → google/gemini-2.5-pro",
  "routed model history should preserve step order while compacting consecutive duplicates",
);
assert.equal(hooks.lastRecordedModelBefore(3).label, "google/gemini-2.5-pro");

// Resume still exists in Kilo's session context, but TL Studio should not render
// the repeated continuation prompt as another visible user message.
const originalUserRow = new FakeElement("article");
const firstResumeRow = new FakeElement("article");
const secondResumeRow = new FakeElement("article");
const transcript = new FakeElement("section");
transcript.querySelectorAll = (selector) => selector === ".message.user"
  ? [originalUserRow, firstResumeRow, secondResumeRow]
  : [];
K.els.conversation = transcript;
hooks.hideResumeMessages();
assert.equal(originalUserRow.removed, false);
assert.equal(firstResumeRow.removed, true);
assert.equal(secondResumeRow.removed, true);

// Completed step-finish metadata should surface the actual routed model sequence
// and the retry attempt number in the assistant UI.
const assistantRows = retryMessages
  .filter((message) => message.info.role === "assistant")
  .map(() => {
    const row = new FakeElement("article");
    const content = new FakeElement("div");
    row.lookup.set(".message-content", content);
    return { row, content };
  });
const routedConversation = new FakeElement("section");
routedConversation.querySelectorAll = (selector) => selector === ".message.assistant:not(.working-message)"
  ? assistantRows.map((item) => item.row)
  : [];
K.els.conversation = routedConversation;
hooks.renderRoutedModels();
assert.match(assistantRows[0].content.querySelector(".routed-model-meta").textContent, /Attempt 1/);
assert.match(assistantRows[0].content.querySelector(".routed-model-meta").textContent, /anthropic\/claude-sonnet-4 ×2/);
assert.match(assistantRows[1].content.querySelector(".routed-model-meta").textContent, /Attempt 2/);
assert.match(assistantRows[1].content.querySelector(".routed-model-meta").textContent, /openai\/gpt-5 ×2 → google\/gemini-2.5-pro/);

const providerTimeout = JSON.stringify({
  code: 503,
  message: "The upstream provider timed out while sending the response. (request id: fra1:test)",
  type: "timeout",
  param: null,
});
assert.equal(hooks.timeoutKind("Upstream idle timeout exceeded"), "idle");
assert.equal(hooks.timeoutKind("The upstream provider timed out while sending the response."), "provider");
assert.equal(hooks.timeoutKind(providerTimeout), "provider");
assert.equal(hooks.timeoutKind('{"code":503,"message":"Service unavailable"}'), "");

// The provider-timeout card must preserve diagnostics, show the retry attempt and
// last routed model that Kilo actually recorded, and keep the safe Resume path.
const error = new FakeElement("div");
error.textContent = providerTimeout;
const timeoutContent = assistantRows[1].content;
assistantRows[1].row.lookup.set(".message-error-text", error);
const timeoutConversation = new FakeElement("section");
timeoutConversation.querySelectorAll = (selector) => {
  if (selector === ".message.error") return [assistantRows[1].row];
  if (selector === ".message.assistant:not(.working-message)") return assistantRows.map((item) => item.row);
  return [];
};
K.els.conversation = timeoutConversation;
K.state.sending = false;
K.state.session = sessions[2];
let sent = 0;
K.sendPrompt = async () => { sent += 1; };

hooks.addTimeoutRecovery();
assert.equal(error.textContent, "Upstream provider timeout");
assert.equal(error.dataset.rawError, providerTimeout);
assert.equal(error.title, providerTimeout);
const recovery = timeoutContent.querySelector(".timeout-recovery");
assert.ok(recovery, "provider timeout should render a recovery control");
assert.equal(recovery.children[1].textContent, "Resume");
assert.match(recovery.children[0].children[1].textContent, /Attempt 2/);
assert.match(recovery.children[0].children[1].textContent, /google\/gemini-2.5-pro/);

const click = recovery.children[1].listeners.get("click");
assert.equal(typeof click, "function");
Promise.resolve(click()).then(() => {
  assert.equal(sent, 1, "Resume should submit exactly one continuation prompt");
  assert.equal(K.els.prompt.value, hooks.RESUME_PROMPT);
  console.log("status-ui regressions: ok");
}).catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
