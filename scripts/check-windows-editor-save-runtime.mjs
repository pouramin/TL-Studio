#!/usr/bin/env node
// Real Windows native launcher + Chromium/Edge UI + filesystem acceptance test.
// Operates only on a temporary project and never contacts a model provider.
import { spawn } from "node:child_process";
import fs from "node:fs";
import http from "node:http";
import net from "node:net";
import os from "node:os";
import path from "node:path";

const executable = process.argv[2];
if (process.platform !== "win32" || !executable || !fs.existsSync(executable)) {
  throw Error("Run on a Windows runner with the built TL Studio executable as argument");
}
const temp = fs.mkdtempSync(path.join(os.tmpdir(), "tl-editor-acceptance-"));
const project = path.join(temp, "project");
const stateDir = path.join(temp, "state");
fs.mkdirSync(project);
fs.mkdirSync(stateDir);
const filePath = path.join(project, "note.txt");
fs.writeFileSync(filePath, "seed\n");
const freePort = () => new Promise((resolve, reject) => {
  const server = net.createServer();
  server.once("error", reject);
  server.listen(0, "127.0.0.1", () => {
    const port = server.address().port;
    server.close(err => err ? reject(err) : resolve(port));
  });
});
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
const assert = (condition, message) => { if (!condition) throw Error(message); };
const requestJSON = url => new Promise((resolve, reject) => {
  http.get(url, response => {
    let body = "";
    response.on("data", chunk => body += chunk);
    response.on("end", () => {
      try { resolve({ status: response.statusCode, body: JSON.parse(body) }); }
      catch { resolve({ status: response.statusCode, body }); }
    });
  }).on("error", reject);
});
const chromePaths = [
  process.env.CHROME_BIN,
  path.join(process.env.PROGRAMFILES || "", "Google", "Chrome", "Application", "chrome.exe"),
  path.join(process.env["PROGRAMFILES(X86)"] || "", "Google", "Chrome", "Application", "chrome.exe"),
  path.join(process.env.PROGRAMFILES || "", "Microsoft", "Edge", "Application", "msedge.exe"),
  path.join(process.env["PROGRAMFILES(X86)"] || "", "Microsoft", "Edge", "Application", "msedge.exe")
];
const chromePath = chromePaths.find(p => p && fs.existsSync(p));
assert(chromePath, "Chrome or Edge executable unavailable on Windows runner");

let processLog = "";
let chromeLog = "";
let native, chrome, ws;
const dispose = () => {
  try { ws?.close(); } catch {}
  try { chrome?.kill(); } catch {}
  try { native?.kill(); } catch {}
};
process.on("exit", dispose);
process.on("SIGINT", () => { dispose(); process.exit(130); });

let baseURL = "";
let launchKey = "";
const outcomes = [];
try {
  native = spawn(executable, ["--no-browser", "--project", project], {
    cwd: temp, env: { ...process.env, TL_STUDIO_STATE_DIR: stateDir },
    stdio: ["ignore", "pipe", "pipe"], windowsHide: true
  });
  native.stdout.on("data", data => { processLog += String(data); });
  native.stderr.on("data", data => { processLog += String(data); });
  for (let n = 0; n < 150; n++) {
    const match = processLog.match(/Local:\s+(http:\/\/127\.0\.0\.1:\d+)/);
    if (match) {
      const health = await requestJSON(match[1] + "/local/health").catch(() => null);
      if (health && health.status === 200) { baseURL = match[1]; break; }
    }
    if (native.exitCode !== null) break;
    await delay(120);
  }
  assert(baseURL, "Native Windows launcher not healthy: " + processLog.slice(-3000));
  launchKey = (processLog.match(/Auth:\\s+([a-f0-9]{64})/) || [])[1] || "";
  assert(launchKey, "Native Windows launcher did not expose a scoped authorization key");
  const authenticatedURL = baseURL + "/#tl_auth=" + encodeURIComponent(launchKey);

  const port = await freePort();
  const profile = path.join(temp, "chrome-profile");
  chrome = spawn(chromePath, [
    "--headless=new", "--disable-gpu", "--disable-dev-shm-usage",
    "--no-first-run", "--no-default-browser-check", "--disable-background-networking",
    "--remote-debugging-port=" + port, "--user-data-dir=" + profile,
    "--window-size=1400,850", authenticatedURL
  ], { stdio: ["ignore", "pipe", "pipe"], windowsHide: true });
  chrome.stderr.on("data", data => { chromeLog += String(data); });
  let target = null;
  for (let n = 0; n < 200; n++) {
    const response = await requestJSON("http://127.0.0.1:" + port + "/json/list").catch(() => null);
    target = Array.isArray(response?.body) ?
      response.body.find(item => item.type === "page" && item.webSocketDebuggerUrl) : null;
    if (target) break;
    if (chrome.exitCode !== null) break;
    await delay(100);
  }
  assert(target, "Windows Chrome DevTools unavailable: " + chromeLog.slice(-2500));
  ws = new WebSocket(target.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(Error("Chrome CDP connection timed out")), 10000);
    ws.addEventListener("open", () => { clearTimeout(timer); resolve(); }, { once: true });
    ws.addEventListener("error", () => { clearTimeout(timer); reject(Error("Chrome CDP connection error")); }, { once: true });
  });
  let id = 0;
  const pending = new Map();
  ws.addEventListener("message", event => {
    const packet = JSON.parse(String(event.data));
    if (packet.id && pending.has(packet.id)) {
      const { resolve, reject } = pending.get(packet.id);
      pending.delete(packet.id);
      if (packet.error) reject(Error(JSON.stringify(packet.error)));
      else resolve(packet.result);
    }
  });
  const cdp = (method, params = {}) => new Promise((resolve, reject) => {
    const next = ++id;
    pending.set(next, { resolve, reject });
    ws.send(JSON.stringify({ id: next, method, params }));
  });
  const evaluate = async expr => {
    const res = await cdp("Runtime.evaluate", {
      expression: expr, awaitPromise: true, returnByValue: true, userGesture: true
    });
    if (res.exceptionDetails) throw Error(res.exceptionDetails.exception?.description || res.exceptionDetails.text);
    return res.result?.value;
  };
  const waitFor = async (expr, name, attempts = 150) => {
    for (let n = 0; n < attempts; n++) {
      if (await evaluate(expr)) return;
      await delay(120);
    }
    throw Error("Timed out: " + name);
  };
  await cdp("Runtime.enable");
  await cdp("Page.enable");
  await cdp("Page.navigate", { url: authenticatedURL });
  await waitFor('document.querySelector("#app.workspace-v2") !== null', "workbench ready");
  await waitFor('Array.from(document.querySelectorAll(".file-row.file-file")).some(e => e.textContent.includes("note.txt"))', "Explorer note file");
  await evaluate('Array.from(document.querySelectorAll(".file-row.file-file")).find(e => e.textContent.includes("note.txt")).click()');
  await waitFor('document.getElementById("fileEditorPath")?.textContent === "note.txt"', "Editor note file");
  await waitFor('document.querySelector("#fileEditorSurface")?.classList.contains("hidden") === false', "Editor visible");
  await waitFor('globalThis.TLMonaco?.editor?.getModels?.().some(m => m.uri.path.endsWith("/note.txt"))', "Monaco loaded");
  const setEditor = text => evaluate(
    '(async () => { const m=globalThis.TLMonaco.editor.getModels().find(m=>m.uri.path.endsWith("/note.txt")); m.setValue(' +
      JSON.stringify(text) + '); return document.getElementById("saveFile")?.disabled === false; })()'
  );
  const clickSave = () => evaluate('document.getElementById("saveFile").click()');
  const editorContent = () => evaluate('document.getElementById("fileEditor").value');
  const disk = () => fs.readFileSync(filePath, "utf8");

  // 1. The live editor writes through the actual HTTP endpoint and updates SHA.
  assert(await setEditor("saved-on-windows\n"), "Editor did not mark basic save dirty");
  await clickSave();
  await waitFor('document.getElementById("saveFile")?.disabled === true && document.getElementById("editorStatus")?.textContent === "Saved"', "normal save completion");
  assert(disk() === "saved-on-windows\n", "Normal Editor save did not reach Windows disk");
  outcomes.push("basic editor save and revision refresh");

  // 2. A file externally changed after it was opened must not be overwritten.
  fs.writeFileSync(filePath, "external-change\n");
  assert(await setEditor("stale-unsafe-overwrite\n"), "Editor did not mark conflicting edit dirty");
  await evaluate('globalThis.__editorConflictConfirmed=false; window.confirm=() => { globalThis.__editorConflictConfirmed=true; return false; }');
  await clickSave();
  await waitFor('globalThis.__editorConflictConfirmed === true', "stale file conflict confirmation");
  assert(disk() === "external-change\n", "Conflict incorrectly overwrote external disk content");
  assert((await editorContent()) === "stale-unsafe-overwrite\n", "Conflict lost user editor content");
  outcomes.push("external change 409 conflict preserves disk and unsaved buffer");

  // 3. Recover from 409 by explicitly reloading fresh disk contents.
  await evaluate('window.confirm=() => true; document.getElementById("reloadFile").click()');
  await waitFor('document.getElementById("fileEditor").value === "external-change\\n"', "reload from disk");
  outcomes.push("explicit reload restores fresh disk snapshot");

  // 4. Delay the successful save response while the user continues editing.
  // The browser still issues the real HTTP write, only the response delivery
  // back to the Editor is delayed to reproduce a deterministic network race.
  await evaluate('(() => { const original=window.fetch.bind(window); window.__saveHeld=false; window.__saveRelease=null; window.fetch=async (input, init={}) => { const url=new URL(typeof input==="string"?input:input.url, location.href); if(url.pathname==="/local/file" && String(init.method||"GET").toUpperCase()==="PUT") { const response=await original(input,init); window.__saveHeld=true; await new Promise(resolve => { window.__saveRelease=resolve; }); return response; } return original(input,init); }; })()');
  assert(await setEditor("snapshot-written\n"), "Editor did not stage race snapshot");
  await clickSave();
  await waitFor('globalThis.__saveHeld === true', "actual write completed before deferred response");
  assert(disk() === "snapshot-written\n", "Pending response test failed to write snapshot on disk");
  await setEditor("snapshot-written\nlater-keystrokes\n");
  await evaluate('(() => { const release=globalThis.__saveRelease; globalThis.__saveRelease=null; globalThis.__saveHeld=false; release(); })()');
  await waitFor('document.getElementById("saveFile")?.disabled === false && document.getElementById("editorStatus")?.textContent.includes("Unsaved")', "late save preserves dirty buffer");
  assert((await editorContent()) === "snapshot-written\nlater-keystrokes\n", "Late response discarded new keystrokes");
  assert(disk() === "snapshot-written\n", "Late response unexpectedly wrote unsaved keystrokes");
  outcomes.push("in-flight save preserves later keystrokes and correct disk snapshot");

  // 5. A second save must use the new revision and persist the later edit.
  await clickSave();
  await waitFor('globalThis.__saveHeld === true && globalThis.__saveRelease !== null', "second write held");
  await evaluate('(() => { const release=globalThis.__saveRelease; globalThis.__saveRelease=null; globalThis.__saveHeld=false; release(); })()');
  await waitFor('document.getElementById("saveFile")?.disabled === true && document.getElementById("editorStatus")?.textContent === "Saved"', "second save completion");
  assert(disk() === "snapshot-written\nlater-keystrokes\n", "Second save did not preserve latest edits");
  outcomes.push("second save commits later edit using updated SHA");

  console.log(JSON.stringify({ ok: true, platform: process.platform, native: true, realBrowser: true, scenarios: outcomes }, null, 2));
} catch (error) {
  console.error("WINDOWS EDITOR ACCEPTANCE FAILED:", error.stack || error);
  console.error("Native process output:", processLog.slice(-3000));
  console.error("Browser output:", chromeLog.slice(-3000));
  process.exitCode = 1;
} finally {
  dispose();
  // Do not remove until after child processes have been terminated.
  for (let n = 0; n < 15; n++) {
    try { fs.rmSync(temp, { recursive: true, force: true }); break; }
    catch { await delay(100); }
  }
}
