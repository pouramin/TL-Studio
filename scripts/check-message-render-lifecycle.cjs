"use strict";

const assert = require("node:assert/strict");
const vm = require("node:vm");
const { transformSync } = require("esbuild");
const { readBrowserTypeScript } = require("./browser-source-harness.cjs");

// Exercise the real typed Browser kernel, not a separate test implementation.
const source = transformSync(readBrowserTypeScript("kernel.ts"), {
  loader: "ts",
  format: "cjs",
  target: "es2020",
  sourcefile: "kernel.ts",
}).code;
const moduleRef = { exports: {} };
const viewScopeModule = { exports: {} };
vm.runInNewContext(transformSync(readBrowserTypeScript("view-scope.ts"), {
  loader: "ts", format: "cjs", target: "es2020",
}).code, { module: viewScopeModule, exports: viewScopeModule.exports });
const logged = [];
vm.runInNewContext(source, {
  module: moduleRef,
  exports: moduleRef.exports,
  require: (name) => {
    if (name === "./view-scope") return viewScopeModule.exports;
    throw new Error(`Unexpected kernel dependency: ${name}`);
  },
  document: { getElementById: () => null },
  console: { error: (...args) => logged.push(args) },
}, { filename: "kernel.ts" });

const K = moduleRef.exports.K;
assert.equal(typeof K.onMessagesRendered, "function");
assert.equal(typeof K.emitMessagesRendered, "function");
const called = [];

const stopFirst = K.onMessagesRendered(() => called.push("status"));
K.onMessagesRendered(() => {
  called.push("broken");
  throw new Error("one decorator failed");
});
const stopDiagnostics = K.onMessagesRendered(() => called.push("diagnostics"));
K.emitMessagesRendered();
assert.deepEqual(called, ["status", "broken", "diagnostics"], "render observers must fire in insertion order");
assert.equal(logged.length, 1, "failed decorators must be logged");
assert.match(String(logged[0][0]), /message render effect failed/);

stopFirst();
stopDiagnostics();
called.length = 0;
K.emitMessagesRendered();
assert.deepEqual(called, ["broken"], "unsubscribe must not remove unrelated observers");
stopFirst(); // unsubscribing twice must be harmless

// New listeners registered during dispatch only take effect on the next render.
const calls = [];
const other = K.onMessagesRendered(() => {
  calls.push("early");
  if (calls.filter(x => x === "early").length === 1) K.onMessagesRendered(() => calls.push("later"));
});
K.emitMessagesRendered();
assert.deepEqual(calls, ["early"]);
K.emitMessagesRendered();
assert.deepEqual(calls, ["early", "early", "later"]);
other();

assert.throws(() => K.onMessagesRendered(null), /function/);

// Assert the graph uses one owner for message rendering and registered observers
// rather than fragile order-dependent mutation of K.renderMessages.
const presentation = readBrowserTypeScript("presentation.ts");
const status = readBrowserTypeScript("status-ui.ts");
const diagnostics = readBrowserTypeScript("diagnostics-ui.ts");
assert.match(presentation, /K\.emitMessagesRendered\(\)/);
assert.match(status, /K\.onMessagesRendered\(/);
assert.match(diagnostics, /K\.onMessagesRendered\(/);
assert.doesNotMatch(status, /K\.renderMessages\s*=/);
assert.doesNotMatch(diagnostics, /K\.renderMessages\s*=/);
console.log("message rendering lifecycle regressions: ok");
