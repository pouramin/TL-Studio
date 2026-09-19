"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const repoRoot = path.resolve(__dirname, "..");
const source = fs.readFileSync(path.join(repoRoot, "cmd", "launcher", "web", "providers-ui.js"), "utf8");
const productSource = fs.readFileSync(path.join(repoRoot, "cmd", "launcher", "web", "product-ui.js"), "utf8");
const providerTsSource = fs.readFileSync(path.join(repoRoot, "cmd", "launcher", "ui", "providers-ui.ts"), "utf8");

const K = { __providersUiInstalled: false };
const context = vm.createContext({
  window: { KLU: K },
  document: { getElementById: () => null },
  console,
  URL,
  Object,
  Number,
  String,
  Set,
});
vm.runInContext(source, context, { filename: "providers-ui.js" });

const hooks = K.__providersUi;
assert.ok(hooks, "provider UI hooks should be installed even when settings DOM is unavailable");
assert.equal(hooks.PROTOCOLS.has("openai-compatible"), true);
assert.equal(hooks.PROTOCOLS.has("openai-responses"), true);
assert.equal(hooks.PROTOCOLS.has("anthropic-messages"), true);

for (const forbidden of ["@ai-sdk/", "K.api.config", "K.api.auth", "config/overlay"]) {
  assert.equal(source.includes(forbidden), false, `provider UI leaked runtime config detail: ${forbidden}`);
}
assert.equal(source.includes("AgentRouter"), false, "provider UI must not hard-code a provider brand");
assert.equal(source.includes("deepseek-v4-flash"), false, "provider UI must not hard-code a model preset");

const draft = {
  providerID: "example-provider",
  name: "Example Provider",
  protocol: "openai-compatible",
  baseURL: "https://api.example.com/v1",
  modelID: "example-model",
  modelName: "Example Model",
  toolCall: true,
  reasoning: true,
  contextLimit: "128000",
  outputLimit: "16384",
  apiKey: "super-secret-should-never-enter-config",
};
assert.equal(hooks.validateDraft(draft), "");

const definition = hooks.buildProviderDefinition(draft);
assert.equal(definition.id, "example-provider");
assert.equal(definition.name, "Example Provider");
assert.equal(definition.protocol, "openai-compatible");
assert.equal(definition.baseURL, "https://api.example.com/v1");
assert.equal(definition.models[0].id, "example-model");
assert.equal(definition.models[0].name, "Example Model");
assert.equal(definition.models[0].toolCall, true);
assert.equal(definition.models[0].reasoning, true);
assert.equal(definition.models[0].contextLimit, 128000);
assert.equal(definition.models[0].outputLimit, 16384);
assert.equal(JSON.stringify(definition).includes("super-secret"), false, "API keys must not enter TL Studio provider config");

const existing = {
  id: "example-provider",
  name: "Existing",
  protocol: "openai-compatible",
  baseURL: "https://old.example/v1",
  models: [{ id: "other-model", name: "Other", toolCall: true, reasoning: false }],
};
const merged = hooks.buildProviderDefinition({
  ...draft,
  baseURL: "https://api.example.com/v1/",
  reasoning: false,
  contextLimit: "",
  outputLimit: "",
}, existing);
assert.equal(merged.baseURL, "https://api.example.com/v1");
assert.ok(merged.models.some((model) => model.id === "other-model"), "editing one model must preserve other TL Studio model definitions");
assert.ok(merged.models.some((model) => model.id === "example-model"));

const entries = hooks.customProviderEntries({
  providers: [
    definition,
    { id: "z-provider", name: "Zed", protocol: "openai-compatible", baseURL: "https://z.example/v1", models: [] },
  ],
});
assert.deepEqual(Array.from(entries, (entry) => entry.id), ["example-provider", "z-provider"]);

const removed = hooks.withoutProvider({ providers: [definition, { id: "keep-me", name: "Keep", models: [] }] }, "example-provider");
assert.deepEqual(Array.from(removed.providers, (provider) => provider.id), ["keep-me"], "successful delete must remove the provider from visible TL Studio state immediately");

assert.match(productSource, /settingsDialog\?\.querySelectorAll\("\[data-settings-section\]"\)/, "settings navigation must query dynamic sections so Providers cannot stay highlighted beside General/About");
assert.match(productSource, /settingsDialog\?\.querySelectorAll\("\[data-settings-panel\]"\)/, "settings navigation must query dynamic panels");
assert.match(productSource, /K\.activateSettingsSection\s*=\s*activateSettingsSection/, "dynamic settings activation should be shared with injected settings sections");
assert.equal(providerTsSource.includes('if (button.dataset.settingsSection === "providers") continue;'), true, "Providers tab must be excluded from leave-section reset");
assert.equal(providerTsSource.includes('button.addEventListener("click", resetTransientForm);'), true, "leaving Providers must reset the transient add/edit form");
assert.equal(providerTsSource.includes('settingsDialog.addEventListener("close", resetTransientForm);'), true, "closing Settings must reset the transient provider form");
assert.equal(typeof hooks.resetTransientForm, "undefined", "DOM-only reset hook must not be installed when provider settings DOM is unavailable");

assert.match(hooks.validateDraft({ ...draft, providerID: "Bad ID" }), /Provider ID/);
assert.match(hooks.validateDraft({ ...draft, baseURL: "not-a-url" }), /Base URL/);
assert.match(hooks.validateDraft({ ...draft, modelID: "" }), /Model ID/);
assert.match(hooks.validateDraft({ ...draft, contextLimit: "12.5" }), /Context limit/);

console.log("custom provider UI regressions: ok");
