#!/usr/bin/env node

import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import http from "node:http";
import net from "node:net";
import os from "node:os";
import path from "node:path";

const baseURL = String(process.argv[2] || "").replace(/\/$/, "");
const launchKey = String(process.env.TL_STUDIO_LAUNCH_KEY || "");
if (!launchKey) throw new Error("Browser acceptance requires an authenticated launcher key");
const authenticatedURL = baseURL + "/#tl_auth=" + encodeURIComponent(launchKey);
if (!/^http:\/\/127\.0\.0\.1:\d+$/.test(baseURL)) {
  console.error("usage: check-workbench-browser-runtime.mjs http://127.0.0.1:<port>");
  process.exit(2);
}
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const assert = (condition, message) => { if (!condition) throw new Error(message); };

let chromeBin = "";
for (const candidate of [process.env.CHROME_BIN, "google-chrome", "google-chrome-stable", "chromium", "chromium-browser"].filter(Boolean)) {
  const found = spawnSync("bash", ["-lc", "command -v " + JSON.stringify(candidate) + " || true"], { encoding: "utf8" }).stdout.trim();
  if (found) { chromeBin = found; break; }
}
assert(chromeBin, "Chrome/Chromium is unavailable on the runtime-smoke runner");

const freePort = () => new Promise((resolve, reject) => {
  const server = net.createServer();
  server.once("error", reject);
  server.listen(0, "127.0.0.1", () => {
    const address = server.address();
    const port = typeof address === "object" && address ? address.port : 0;
    server.close((error) => error ? reject(error) : resolve(port));
  });
});
const getJSON = (url) => new Promise((resolve, reject) => {
  const request = http.get(url, (response) => {
    let body = "";
    response.setEncoding("utf8");
    response.on("data", (chunk) => { body += chunk; });
    response.on("end", () => {
      if ((response.statusCode || 500) >= 400) return reject(new Error("GET " + url + ": " + response.statusCode));
      try { resolve(JSON.parse(body)); } catch (error) { reject(error); }
    });
  });
  request.on("error", reject);
  request.setTimeout(2000, () => request.destroy(new Error("request timeout")));
});

const debugPort = await freePort();
const profile = fs.mkdtempSync(path.join(os.tmpdir(), "tl-studio-browser-smoke-"));
const chrome = spawn(chromeBin, [
  "--headless=new", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage",
  "--disable-background-networking", "--disable-default-apps", "--no-first-run",
  "--no-default-browser-check", "--remote-debugging-port=" + debugPort,
  "--user-data-dir=" + profile, "--window-size=1440,900", authenticatedURL,
], { stdio: ["ignore", "pipe", "pipe"] });

let chromeStderr = "";
chrome.stderr.on("data", (chunk) => { chromeStderr += String(chunk); });

const cleanup = () => {
  try { chrome.kill("SIGTERM"); } catch {}
  try { fs.rmSync(profile, { recursive: true, force: true }); } catch {}
};
process.on("exit", cleanup);

let target = null;
for (let attempt = 0; attempt < 200; attempt++) {
  try {
    const targets = await getJSON("http://127.0.0.1:" + debugPort + "/json/list");
    target = Array.isArray(targets) ? targets.find((item) => item.type === "page" && item.webSocketDebuggerUrl) : null;
    if (target) break;
  } catch {}
  await sleep(100);
}
assert(
  target && target.webSocketDebuggerUrl,
  "Chrome DevTools target did not become available. Chrome stderr: " + chromeStderr.slice(-4000)
);

const ws = new WebSocket(target.webSocketDebuggerUrl);
await new Promise((resolve, reject) => {
  const timer = setTimeout(() => reject(new Error("CDP WebSocket connection timed out")), 5000);
  ws.addEventListener("open", () => { clearTimeout(timer); resolve(); }, { once: true });
  ws.addEventListener("error", () => { clearTimeout(timer); reject(new Error("CDP WebSocket error")); }, { once: true });
});

let nextID = 1;
const pending = new Map();
const exceptions = [];
const consoleErrors = [];
ws.addEventListener("message", (event) => {
  const message = JSON.parse(String(event.data));
  if (message.id && pending.has(message.id)) {
    const pair = pending.get(message.id);
    pending.delete(message.id);
    if (message.error) pair.reject(new Error(message.error.message || JSON.stringify(message.error)));
    else pair.resolve(message.result);
    return;
  }
  if (message.method === "Runtime.exceptionThrown") {
    exceptions.push(message.params?.exceptionDetails?.exception?.description || message.params?.exceptionDetails?.text || "Runtime exception");
  }
  if (message.method === "Runtime.consoleAPICalled" && message.params?.type === "error") {
    consoleErrors.push((message.params?.args || []).map((arg) => arg.value || arg.description || "").join(" "));
  }
});

const cdp = (method, params = {}) => new Promise((resolve, reject) => {
  const id = nextID++;
  pending.set(id, { resolve, reject });
  ws.send(JSON.stringify({ id, method, params }));
});
const evaluate = async (expression) => {
  const response = await cdp("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true, userGesture: true });
  if (response.exceptionDetails) throw new Error(response.exceptionDetails.exception?.description || response.exceptionDetails.text || "Runtime.evaluate failed");
  return response.result?.value;
};
const waitFor = async (expression, message, attempts = 80) => {
  for (let attempt = 0; attempt < attempts; attempt++) {
    if (await evaluate(expression)) return;
    await sleep(100);
  }
  throw new Error(message);
};
const rect = (selector) => evaluate(
  "(() => { const e=document.querySelector(" + JSON.stringify(selector) + "); if(!e)return null; const r=e.getBoundingClientRect(); return {left:r.left,top:r.top,width:r.width,height:r.height}; })()"
);
const drag = async (selector, dx, dy) => {
  const start = await rect(selector);
  assert(start, "drag target missing: " + selector);
  const x = start.left + Math.max(1, start.width / 2);
  const y = start.top + Math.max(1, start.height / 2);
  await cdp("Input.dispatchMouseEvent", { type:"mousePressed", x, y, button:"left", buttons:1, clickCount:1 });
  await cdp("Input.dispatchMouseEvent", { type:"mouseMoved", x:x+dx, y:y+dy, button:"left", buttons:1 });
  await sleep(60);
  await cdp("Input.dispatchMouseEvent", { type:"mouseReleased", x:x+dx, y:y+dy, button:"left", buttons:0, clickCount:1 });
  await sleep(80);
};

await cdp("Runtime.enable");
await cdp("Page.enable");
await cdp("Page.navigate", { url: authenticatedURL });
await waitFor('document.readyState === "complete"', "TL Studio page did not finish loading");
await waitFor('document.getElementById("app")?.classList.contains("workspace-v2") === true', "redesigned workspace did not initialize");

const structure = await evaluate('(() => ({topbar:!!document.querySelector(".workspace-topbar"),rail:!!document.querySelector(".workspace-activity-rail"),context:!!document.querySelector(".workspace-context-sidebar"),editor:!!document.querySelector("#filesPanel.workspace-mounted"),agent:!!document.querySelector(".workspace-agent-panel"),status:!!document.querySelector(".workspace-statusbar"),terminalActivity:!!document.getElementById("workspaceTerminalActivity"),previewHandles:document.querySelectorAll(".preview-resize-handle").length,modelOptions:document.querySelectorAll("#modelSelect option").length,agentOptions:document.querySelectorAll("#agentSelect option").length}))()');
for (const key of ["topbar","rail","context","editor","agent","status","terminalActivity"]) assert(structure[key], "workspace region missing: " + key);
assert(structure.previewHandles === 8, "expected 8 Preview resize handles");
assert(structure.modelOptions > 0, "model selector did not initialize");
assert(structure.agentOptions > 0, "agent selector did not initialize");

// Exercise real Agent budget controls, persistence and safe defaults.
const budgetUI = await evaluate(`(() => {
  const parent = document.getElementById("agentRunControls");
  const rounds = document.getElementById("agentMaxToolRounds");
  const tokens = document.getElementById("agentMaxRunTokens");
  const timeout = document.getElementById("agentModelTimeoutSeconds");
  const idle = document.getElementById("agentStreamIdleSeconds");
  if (!parent || !rounds || !tokens || !timeout || !idle) return { present:false };
  const initial = { rounds:rounds.value, tokens:tokens.value, timeout:timeout.value, idle:idle.value };
  parent.open = true;
  const set = (element, value) => { element.value=value; element.dispatchEvent(new Event("change",{bubbles:true})); };
  set(rounds,"50"); set(tokens,"200000"); set(timeout,"360"); set(idle,"60");
  const values = Object.keys(localStorage).map(key => [key,localStorage.getItem(key)]);
  const persisted = values.some(([key,value])=>key.startsWith("tl-studio.agent.rounds.")&&value==="50") &&
    values.some(([key,value])=>key.startsWith("tl-studio.agent.tokens.")&&value==="200000") &&
    values.some(([key,value])=>key.startsWith("tl-studio.agent.timeout.")&&value==="360") &&
    values.some(([key,value])=>key.startsWith("tl-studio.agent.idle.")&&value==="60");
  set(rounds,initial.rounds); set(tokens,initial.tokens); set(timeout,initial.timeout); set(idle,initial.idle);
  parent.open = false;
  return { present:true, initial, persisted };
})()`);
assert(budgetUI.present, "Agent run preferences not mounted in workbench");
assert(budgetUI.initial.rounds === "24" && budgetUI.initial.tokens === "0", "unexpected Agent budget defaults");
assert(budgetUI.persisted, "Agent budget/timeout preferences were not persisted");


await waitFor('document.querySelectorAll(".file-row").length > 0', "Explorer did not load real project entries");
await waitFor('document.querySelector(".file-row.file-file") !== null', "Explorer did not render a real project file");
await waitFor(`(() => {
  if (document.getElementById("fileEditorTitle")?.textContent !== "No file open") return true;
  const file = document.querySelector(".file-row.file-file");
  if (!file) return false;
  file.click();
  return false;
})()`, "real file did not open in Editor", 120);
await waitFor('document.getElementById("fileEditorSurface") && !document.getElementById("fileEditorSurface").classList.contains("hidden")', "Editor surface did not activate");

await evaluate('document.querySelector(\'[data-workspace-view="search"]\')?.click()');
await waitFor('document.querySelector(\'[data-context="search"]\')?.classList.contains("active") === true', "Search activity did not activate");
await evaluate('(() => { const input=document.getElementById("projectSearchQuery"); input.value="TL Studio"; input.dispatchEvent(new Event("input",{bubbles:true})); })()');
await waitFor('document.querySelectorAll(".project-search-match").length > 0', "real Project Search returned no matches");
await evaluate('document.querySelector(".project-search-match")?.click()');
await sleep(180);
assert(await evaluate('document.getElementById("projectSearchPanel")?.classList.contains("hidden") === false'), "Search context disappeared after result navigation");

for (const mode of ["collapsed","compact","focused"]) {
  await evaluate('document.querySelector(\'[data-agent-mode="' + mode + '"]\')?.click()');
  await sleep(50);
  if (mode === "collapsed") assert(await evaluate('document.getElementById("app").classList.contains("workspace-agent-collapsed")'), "Collapsed Agent state failed");
  if (mode === "focused") assert(await evaluate('document.getElementById("app").classList.contains("workspace-agent-focused")'), "Focused Agent state failed");
  if (mode === "compact") assert(await evaluate('!document.getElementById("app").classList.contains("workspace-agent-collapsed") && !document.getElementById("app").classList.contains("workspace-agent-focused")'), "Compact Agent state failed");
}

await evaluate('document.dispatchEvent(new KeyboardEvent("keydown",{key:"k",ctrlKey:true,bubbles:true}))');
await waitFor('!document.getElementById("workspaceCommandPalette")?.classList.contains("hidden")', "Command Palette did not open with Ctrl+K");
await evaluate('document.dispatchEvent(new KeyboardEvent("keydown",{key:"Escape",bubbles:true}))');
assert(await evaluate('document.getElementById("workspaceCommandPalette")?.classList.contains("hidden") === true'), "Command Palette did not close with Escape");

const themeBefore = await evaluate('document.documentElement.dataset.resolvedTheme');
await evaluate('document.getElementById("workspaceThemeToggle")?.click()');
await sleep(80);
const themeAfter = await evaluate('document.documentElement.dataset.resolvedTheme');
assert(themeBefore !== themeAfter, "Appearance shortcut did not use the real theme preference");

await evaluate('document.getElementById("workspaceTerminalActivity")?.click()');
await waitFor('!document.getElementById("terminalPanel")?.classList.contains("hidden")', "Terminal did not open");
await evaluate('(() => { const input=document.getElementById("terminalInput"); input.value="printf workspace-smoke"; document.getElementById("terminalForm").dispatchEvent(new Event("submit",{bubbles:true,cancelable:true})); })()');
await waitFor('document.getElementById("terminalOutput")?.textContent.includes("workspace-smoke")', "real Terminal command did not execute");

await evaluate('document.getElementById("settingsButton")?.click()');
await waitFor('document.getElementById("settingsDialog")?.open === true', "Settings did not open from Activity Rail");
await evaluate('document.querySelector(\'[data-settings-section="providers"]\')?.click()');
await waitFor('document.querySelector(\'[data-settings-panel="providers"]\')?.classList.contains("hidden") === false', "Providers settings did not activate");
const providerAccountState = await evaluate('(async () => { const accounts=await fetch("/local/provider-accounts").then(r=>r.json()); return {isArray:Array.isArray(accounts),hasKilo:Array.isArray(accounts)&&accounts.some(a=>a?.id==="kilo"),emptyText:document.getElementById("providerAccountList")?.textContent||""}; })()');
assert(providerAccountState?.isArray === true, "Provider account state was unavailable");
assert(providerAccountState.hasKilo === false, "Unavailable Kilo account adapter rendered in native TL Studio");
if (await evaluate('document.querySelectorAll("[data-provider-account-id]").length === 0')) {
  assert(providerAccountState.emptyText.includes("No account-based provider integrations"), "Empty account-provider state did not render");
}
await evaluate('document.getElementById("settingsClose")?.click()');

for (const viewport of [[1280,720],[1440,900],[1920,1080]]) {
  await cdp("Emulation.setDeviceMetricsOverride", { width:viewport[0], height:viewport[1], deviceScaleFactor:1, mobile:false });
  await sleep(80);
  const geometry = await evaluate('(() => ({editor:document.querySelector(".workspace-editor-column")?.getBoundingClientRect().width||0,topbar:document.querySelector(".workspace-topbar")?.getBoundingClientRect().height||0,statusbar:document.querySelector(".workspace-statusbar")?.getBoundingClientRect().height||0}))()');
  assert(geometry.editor >= 420, "Editor became unusable at " + viewport[0] + "x" + viewport[1] + ": " + geometry.editor + "px");
  assert(geometry.topbar > 0 && geometry.statusbar > 0, "workspace chrome missing at " + viewport[0] + "x" + viewport[1]);
}
await cdp("Emulation.setDeviceMetricsOverride", { width:1440, height:900, deviceScaleFactor:1, mobile:false });
await sleep(80);

const sidebarBefore = await evaluate('parseFloat(getComputedStyle(document.getElementById("app")).getPropertyValue("--workspace-context-width"))');
await drag(".workspace-sidebar-splitter", 35, 0);
const sidebarAfter = await evaluate('parseFloat(getComputedStyle(document.getElementById("app")).getPropertyValue("--workspace-context-width"))');
assert(sidebarAfter > sidebarBefore, "Context Sidebar splitter did not change geometry");

const agentBefore = await evaluate('parseFloat(getComputedStyle(document.getElementById("app")).getPropertyValue("--workspace-agent-width"))');
await drag(".workspace-agent-splitter", -35, 0);
const agentAfter = await evaluate('parseFloat(getComputedStyle(document.getElementById("app")).getPropertyValue("--workspace-agent-width"))');
assert(agentAfter > agentBefore, "TL Agent splitter did not change geometry");

const terminalBefore = await evaluate('parseFloat(getComputedStyle(document.getElementById("app")).getPropertyValue("--workspace-terminal-height"))');
await drag(".workspace-terminal-splitter", 0, -35);
const terminalAfter = await evaluate('parseFloat(getComputedStyle(document.getElementById("app")).getPropertyValue("--workspace-terminal-height"))');
assert(terminalAfter > terminalBefore, "Terminal splitter did not change geometry");

const savedLayout = await evaluate('JSON.parse(localStorage.getItem("tl-studio.workspace-layout.v1") || "null")');
assert(savedLayout && savedLayout.sidebarWidth && savedLayout.agentWidth && savedLayout.terminalHeight, "workspace geometry was not persisted");

await evaluate('document.getElementById("previewButton")?.click()');
await waitFor('!document.getElementById("previewPanel")?.classList.contains("hidden")', "Live Preview did not open");
const previewDirections = [
  ["n", 0, -30],
  ["ne", 30, -30],
  ["e", 30, 0],
  ["se", 30, 30],
  ["s", 0, 30],
  ["sw", -30, 30],
  ["w", -30, 0],
  ["nw", -30, -30],
];
const previewResizeResults = {};
for (const [direction, dx, dy] of previewDirections) {
  await evaluate('(() => { const p=document.getElementById("previewPanel"); Object.assign(p.style,{left:"320px",top:"180px",width:"520px",height:"400px"}); })()');
  await sleep(30);
  const before = await rect("#previewPanel");
  await drag(".preview-resize-" + direction, dx, dy);
  const after = await rect("#previewPanel");
  const growsX = direction.includes("e") || direction.includes("w");
  const growsY = direction.includes("n") || direction.includes("s");
  if (growsX) assert(after.width > before.width, "Preview " + direction.toUpperCase() + " resize did not change width");
  if (growsY) assert(after.height > before.height, "Preview " + direction.toUpperCase() + " resize did not change height");
  if (direction.includes("w")) assert(after.left < before.left, "Preview " + direction.toUpperCase() + " resize did not move left edge");
  if (direction.includes("n")) assert(after.top < before.top, "Preview " + direction.toUpperCase() + " resize did not move top edge");
  previewResizeResults[direction] = { before, after };
}

const positionBefore = await rect("#previewPanel");
await drag("#previewPanel .preview-title-wrap", -25, 20);
const positionAfter = await rect("#previewPanel");
assert(positionAfter.left !== positionBefore.left || positionAfter.top !== positionBefore.top, "Preview header drag did not move the window");

const previewGeometry = await evaluate('JSON.parse(localStorage.getItem("tl-studio.preview-window") || "null")');
assert(previewGeometry && previewGeometry.width && previewGeometry.height, "Preview geometry was not persisted");

await sleep(200);
if (exceptions.length || consoleErrors.length) throw new Error("Browser runtime errors detected:\\n" + [...exceptions, ...consoleErrors].join("\\n"));

console.log(JSON.stringify({
  ok:true,
  responsive:["1280x720","1440x900","1920x1080"],
  splitters:{sidebarBefore,sidebarAfter,agentBefore,agentAfter,terminalBefore,terminalAfter},
  preview:previewResizeResults
}, null, 2));

ws.close();
cleanup();
