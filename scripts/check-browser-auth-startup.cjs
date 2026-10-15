#!/usr/bin/env node
"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const ts = require("typescript");

const root = path.resolve(__dirname, "..");
const coreSource = fs.readFileSync(path.join(root, "cmd/launcher/ui/core.ts"), "utf8");
const coreJS = ts.transpileModule(coreSource, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;

function browserHarness({ token = "test-launch-token", existingCookie = false, failBootstrap = false } = {}) {
  const calls = [];
  let authorized = false;
  const expectedToken = token || "test-launch-token";
  let completeBootstrap;
  let prompts = 0;
  const location = { href: "http://127.0.0.1:8765/", origin: "http://127.0.0.1:8765", hash: token ? "#tl_auth=" + token : "", pathname: "/", search: "" };
  const K = { state: {}, els: {} };
  const fetch = (url, options = {}) => {
    calls.push({ url, options, authorizedAtDispatch: authorized });
    if (url === "/local/auth/session") {
      assert.equal(options.headers["X-TL-Studio-Launch-Token"], expectedToken);
      return new Promise(resolve => {
        completeBootstrap = () => {
          if (!failBootstrap) authorized = true;
          resolve({ ok: !failBootstrap, status: failBootstrap ? 401 : 204 });
        };
      });
    }
    if (url === "/local/status") {
      return Promise.resolve({ ok: authorized, status: authorized ? 200 : 401 });
    }
    return Promise.resolve({
      ok: authorized,
      status: authorized ? 200 : 401,
      statusText: authorized ? "OK" : "Unauthorized",
      headers: { get: () => "application/json" },
      json: async () => authorized ? { result: url } : { error: "unauthorized" },
    });
  };
  const window = {
    location,
    sessionStorage: { getItem: () => existingCookie ? expectedToken : null, setItem() {}, removeItem() {} },
    dispatchEvent() {},
    history: { replaceState: (_state, _title, newPath) => {
      assert.equal(newPath, "/");
      location.hash = "";
    } },
    prompt: () => { prompts++; return ""; },
  };
  vm.runInNewContext(coreJS, {
    exports: {},
    require: name => {
      assert.equal(name, "./kernel");
      return { K };
    },
    window,
    fetch,
    URLSearchParams, URL, Headers, Event,
    console,
  }, { filename: "core-transpiled.js" });
  return {
    K, calls, location,
    get promptCount() { return prompts; },
    completeBootstrap() {
      assert.equal(typeof completeBootstrap, "function", "bootstrap did not start");
      completeBootstrap();
    },
  };
}

async function testColdStartSingleFlight() {
  const h = browserHarness();
  const sessions = h.K.request("/local/sessions");
  const permissions = h.K.request("/local/permissions/rules");
  const startup = h.K.ensureLocalAuth();
  assert.equal(startup, h.K.ensureLocalAuth(), "concurrent bootstrap was not shared");
  assert.deepEqual(h.calls.map(x => x.url), ["/local/auth/session"],
    "protected requests were dispatched before bootstrap");
  assert.equal(h.location.hash, "", "launch capability remained in the address bar");
  h.completeBootstrap();
  const results = await Promise.all([sessions, permissions, startup]);
  assert.equal(results[0].result, "/local/sessions");
  assert.equal(results[1].result, "/local/permissions/rules");
  assert.equal(h.promptCount, 0);
  assert.equal(h.calls.filter(x => x.url === "/local/auth/session").length, 1);
  for (const call of h.calls.filter(x => x.url !== "/local/auth/session")) {
    assert.equal(call.authorizedAtDispatch, true, "unauthorized local API request escaped auth gate");
  }
}

async function testMissingOrRejectedAuthFailsClosed() {
  const absent = browserHarness({ token: "" });
  await assert.rejects(absent.K.request("/local/permissions/rules"), /authorization required/i);
  assert.deepEqual(absent.calls.map(x => x.url), [],
    "request escaped a missing-credential bootstrap");
  assert.equal(absent.promptCount, 1);

  const rejected = browserHarness({ failBootstrap: true });
  const attempt = rejected.K.request("/local/plugins");
  rejected.completeBootstrap();
  await assert.rejects(attempt, /authorization failed/i);
  assert.deepEqual(rejected.calls.map(x => x.url), ["/local/auth/session"],
    "request escaped rejected-credential bootstrap");
}

async function testExistingAuthenticatedTab() {
  const h = browserHarness({ token: "", existingCookie: true });
  const pending = h.K.request("/local/sessions");
  h.completeBootstrap();
  const data = await pending;
  assert.equal(data.result, "/local/sessions");
  assert.deepEqual(h.calls.map(x => x.url), ["/local/auth/session", "/local/sessions"]);
  const call = h.calls.at(-1);
  assert.equal(call.options.headers.get("Authorization"), "Bearer test-launch-token");
  await assert.rejects(h.K.localFetch("http://127.0.0.1:9999/local/status"), /outside the control API/);

  assert.equal(h.promptCount, 0);
}

function testPreviewEagerFetchIsAlsoGuarded() {
  const preview = fs.readFileSync(path.join(root, "cmd/launcher/ui/preview.ts"), "utf8");
  assert.match(preview, /const request = async \(path: string, options: RequestInit = \{\}\) => \{\s*\/\/[^\n]*\n\s*await K\.ensureLocalAuth\(\);/,
    "eager Preview registry bypasses the common launch authorization barrier");
}

(async () => {
  await testColdStartSingleFlight();
  await testMissingOrRejectedAuthFailsClosed();
  await testExistingAuthenticatedTab();
  testPreviewEagerFetchIsAlsoGuarded();
  console.log("Browser cold-start authorization regression: PASS");
})().catch(error => {
  console.error(error);
  process.exitCode = 1;
});
