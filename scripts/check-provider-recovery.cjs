"use strict";

const assert = require("node:assert/strict");
const vm = require("node:vm");
const { loadBrowserModule } = require("./browser-source-harness.cjs");

const source = loadBrowserModule("provider-recovery-ui.ts");
const RESUME_PROMPT = "Continue the current task from the existing workspace state. Inspect what is already complete, do not repeat finished work, and finish the user's latest request.";

const K = {
  renderMessages: () => {},
  state: {
    session: { id: "session-1" },
    messages: [],
    sending: false,
  },
  els: {
    conversation: null,
    prompt: { value: "" },
  },
};

const context = vm.createContext({
  K,
  window: {},
  document: {},
  console,
  JSON,
  Promise,
});
vm.runInContext(source, context, { filename: "provider-recovery-ui.ts" });

const hooks = K.__providerRecovery;
assert.ok(hooks, "provider recovery hooks should be installed");

const retryable502 = {
  info: {
    role: "assistant",
    error: {
      name: "APIError",
      data: {
        message: "Provider returned error",
        statusCode: 502,
        isRetryable: true,
      },
    },
  },
  parts: [],
};
let info = hooks.providerErrorInfo(retryable502);
assert.equal(info?.retryable, true);
assert.equal(info?.statusCode, 502);
assert.equal(info?.message, "Provider returned error");

const retryableGeneric = {
  info: {
    role: "assistant",
    error: {
      name: "APIError",
      data: {
        message: "Provider returned error",
        isRetryable: true,
      },
    },
  },
  parts: [],
};
assert.equal(hooks.providerErrorInfo(retryableGeneric)?.retryable, true);

const nonRetryableAuth = {
  info: {
    role: "assistant",
    error: {
      name: "ProviderAuthError",
      data: { message: "Please reauthenticate", providerID: "kilo" },
    },
  },
  parts: [],
};
assert.equal(hooks.providerErrorInfo(nonRetryableAuth)?.retryable, false);

const usageLimit = {
  info: {
    role: "assistant",
    error: {
      name: "APIError",
      data: {
        message: "Provider returned error",
        isRetryable: true,
        responseBody: "FreeUsageLimitError",
      },
    },
  },
  parts: [],
};
assert.equal(hooks.providerErrorInfo(usageLimit)?.retryable, false);

async function testResume() {
  let sendCalls = 0;
  let shownError = "";
  K.isSessionRunning = () => false;
  K.resizePrompt = () => {};
  K.showError = (value) => { shownError = value || ""; };
  K.sendPrompt = async () => {
    sendCalls += 1;
    assert.equal(K.els.prompt.value, RESUME_PROMPT);
  };

  const resumed = await hooks.resumeProviderFailure();
  assert.equal(resumed, true);
  assert.equal(sendCalls, 1);
  assert.equal(shownError, "");
}

testResume()
  .then(() => console.log("provider recovery regressions: ok"))
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
