"use strict";

const assert = require("node:assert/strict");
const vm = require("node:vm");
const { loadBrowserModule } = require("./browser-source-harness.cjs");

const source = loadBrowserModule("runtime-api.ts");
const calls = [];
const K = {
  state: { local: { project: "C:\\Projects\\demo" } },
  request: async (path, options = {}) => {
    calls.push({ path, options });
    if (path.includes("/providers/catalog")) {
      return {
        all: [{ id: "kilo", name: "Hosted", models: { "kilo-auto/free": { name: "Auto Free" } } }],
        connected: ["kilo"],
        default: { kilo: "kilo-auto/free" },
        failed: [],
        hosted: { providerID: "kilo", preferredModels: ["kilo-auto/free"] },
      };
    }
    if (path === "/runtime/providers/config" && (!options.method || options.method === "GET")) {
      return { providers: [] };
    }
    if (path.includes("/hosted/status")) {
      return {
        authenticated: true,
        type: "oauth",
        organizationId: "org-1",
        providerID: "kilo",
        preferredModels: ["kilo-auto/free"],
      };
    }
    return { ok: true };
  },
};

const context = vm.createContext({
  K,
  window: {},
  console,
  URLSearchParams,
  JSON,
  Object,
  encodeURIComponent,
  EventSource: class {},
});
vm.runInContext(source, context, { filename: "runtime-api.ts" });

async function main() {
  assert.ok(K.api?.providers?.config);
  assert.ok(K.api?.providers?.upsert);
  assert.ok(K.api?.providers?.remove);

  const state = await K.api.providerState();
  assert.equal(state.all[0].id, "kilo");
  assert.equal(K.api.hosted.providerID, "kilo");
  assert.deepEqual(Array.from(K.api.hosted.preferredModels), ["kilo-auto/free"]);
  assert.match(calls.at(-1).path, /^\/runtime\/providers\/catalog\?/);
  assert.match(calls.at(-1).path, /directory=C%3A%5CProjects%5Cdemo/);

  await K.api.providers.config();
  assert.equal(calls.at(-1).path, "/runtime/providers/config");

  const provider = {
    id: "agentrouter",
    name: "AgentRouter",
    protocol: "openai-compatible",
    baseURL: "https://co.agentrouter.org/v1",
    models: [{ id: "deepseek-v4-flash", name: "DeepSeek V4 Flash", toolCall: true, reasoning: false }],
  };
  await K.api.providers.upsert("agentrouter", { provider, apiKey: "secret-key" });
  const put = calls.at(-1);
  assert.equal(put.path, "/runtime/providers/config/agentrouter");
  assert.equal(put.options.method, "PUT");
  const putBody = JSON.parse(put.options.body);
  assert.deepEqual(putBody.provider, provider);
  assert.equal(putBody.apiKey, "secret-key");
  assert.equal(JSON.stringify(putBody).includes("@ai-sdk"), false);

  await K.api.providers.remove("agentrouter");
  const remove = calls.at(-1);
  assert.equal(remove.path, "/runtime/providers/config/agentrouter");
  assert.equal(remove.options.method, "DELETE");

  const hosted = await K.api.hosted.status();
  assert.equal(hosted.authenticated, true);
  assert.equal(K.api.hosted.providerID, "kilo");
  assert.equal(calls.at(-1).path.startsWith("/runtime/hosted/status?"), true);

  for (const forbidden of [
    "/config/overlay",
    "/provider/kilo/",
    "/kilo/auth-status",
    '"/auth/',
    '"kilo-auto/free"',
    'providerID: "kilo"',
    "@ai-sdk/",
  ]) {
    assert.equal(source.includes(forbidden), false, `runtime-api.ts leaked implementation detail: ${forbidden}`);
  }

  console.log("provider API adapter regressions: ok");
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
