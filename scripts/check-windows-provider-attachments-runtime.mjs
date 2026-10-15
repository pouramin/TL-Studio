#!/usr/bin/env node
// Real Windows attachment delivery and persisted-provider status acceptance test.
// Uses a temporary project/state and a local mock provider; no paid model calls.
import { spawn } from "node:child_process";
import fs from "node:fs";
import http from "node:http";
import net from "node:net";
import os from "node:os";
import path from "node:path";

const executable = process.argv[2] ? path.resolve(process.argv[2]) : "";
if (process.platform !== "win32" || !executable || !fs.existsSync(executable)) {
  throw Error("Run on a Windows runner with the built TL Studio executable as argument");
}
const temp = fs.mkdtempSync(path.join(os.tmpdir(), "tl-provider-acceptance-"));
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
const modelRequests = [];
const attachmentPath = path.join(temp, "test.txt");
const marker = "TEST-ATTACHMENT-731";
fs.writeFileSync(attachmentPath, marker + "\n", "utf8");
const fixture = http.createServer(async (req, res) => {
  let raw = "";
  for await (const chunk of req) raw += chunk;
  res.setHeader("Content-Type", "application/json");
  if (req.url === "/v1/models") {
    res.end(JSON.stringify({data:[{id:"fixture-model",name:"Fixture model"}]})); return;
  }
  if (req.url !== "/v1/chat/completions") { res.writeHead(404); res.end("{}"); return; }
  const input = JSON.parse(raw);
  modelRequests.push(input);
  const system = input.messages.find(message => message.role === "system")?.content || "";
  const text = JSON.stringify(input.messages).includes(marker) && system.includes("filenames are labels") ? marker : "MISSING_ATTACHMENT_OR_GUIDANCE";
  res.end(JSON.stringify({choices:[{message:{role:"assistant",content:text},finish_reason:"stop"}]}));
});
await new Promise(resolve => fixture.listen(0, "127.0.0.1", resolve));
const providerURL = `http://127.0.0.1:${fixture.address().port}/v1`;

const startNative = async () => {
  processLog = ""; baseURL = ""; launchKey = "";
  native = spawn(executable, ["--no-browser", "--project", project], {
    cwd: temp, env: { ...process.env, TL_STUDIO_STATE_DIR: stateDir, TL_STUDIO_SQLITE_SESSIONS: process.argv.includes("--sqlite") ? "1" : "" },
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
  launchKey = (processLog.match(/Auth:\s+([a-f0-9]{64})/) || [])[1] || "";
  assert(launchKey, "Native Windows launcher did not expose a scoped authorization key");
  return baseURL + "/#tl_auth=" + encodeURIComponent(launchKey);

};
try {
  let authenticatedURL = await startNative();

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
  const auth = await evaluate('(async () => { const token=sessionStorage.getItem("tl-studio-local-capability"); const plain=await fetch("/local/status"); const allowed=await fetch("/local/status",{headers:{Authorization:"Bearer "+token}}); const controller=new AbortController(); const stream=await fetch("/local/events",{headers:{Authorization:"Bearer "+token},signal:controller.signal}); const reader=stream.body.getReader(); const first=await reader.read(); controller.abort(); return {plain:plain.status,allowed:allowed.status,stream:stream.status,first:new TextDecoder().decode(first.value),hash:location.hash}; })()');
  assert(auth.plain === 401 && auth.allowed === 200, "Control API still accepts ambient authority");
  assert(auth.stream === 200 && auth.first.includes("stream.ready"), "Authorized fetch event stream failed");
  assert(auth.hash === "", "Launch capability remained in URL");
  const cookies = await cdp("Network.getCookies", {urls:[baseURL]});
  assert(!cookies.cookies.some(cookie => cookie.name.startsWith("tl_studio_auth_")), "Control auth leaked into a cookie");
  await evaluate('globalThis.__beforeAuthReload = true');
  await cdp("Page.reload");
  await waitFor('globalThis.__beforeAuthReload !== true && Array.from(document.querySelectorAll(".file-row.file-file")).some(e => e.textContent.includes("note.txt"))', "authorized refresh");
  outcomes.push("explicit header authorization, cookie-free SSE, and refresh persistence");

  const configured = await evaluate('(async () => { const r=await fetch("/local/providers/config/openrouter", {method:"PUT",headers:{"Content-Type":"application/json",Authorization:"Bearer "+sessionStorage.getItem("tl-studio-local-capability")},body:JSON.stringify({provider:{id:"openrouter",name:"OpenRouter",protocol:"openai-compatible",baseURL:' + JSON.stringify(providerURL) + ',models:[{id:"fixture-model",name:"Fixture model",toolCall:true,contextLimit:32768,outputLimit:4096}]},apiKey:"local-fixture-key-not-real"})}); return {status:r.status,body:await r.json()}; })()');
  assert(configured.status === 200, "Fixture provider configuration failed: " + JSON.stringify(configured));
  await evaluate('document.getElementById("refreshButton").click()');
  await waitFor('Array.from(document.getElementById("modelSelect").options).some(o=>o.value==="openrouter::fixture-model")', "fixture model in catalog");
  await evaluate('document.getElementById("modelSelect").value="openrouter::fixture-model"; document.getElementById("modelSelect").dispatchEvent(new Event("change"))');
  const doc = await cdp("DOM.getDocument");
  const input = await cdp("DOM.querySelector", {nodeId:doc.root.nodeId,selector:"#attachmentInput"});
  await evaluate('(() => { const read=FileReader.prototype.readAsDataURL; FileReader.prototype.readAsDataURL=function(file) { setTimeout(()=>read.call(this,file),250); }; })()');
  await cdp("DOM.setFileInputFiles", {nodeId:input.nodeId,files:[attachmentPath]});
  // Send while FileReader is intentionally unfinished, as a fast user can.
  await evaluate('document.getElementById("prompt").value="What exact marker is in the attached test.txt?"; document.getElementById("sendButton").click()');
  for (let n=0; n<150 && modelRequests.length===0; n++) await delay(120);
  assert(modelRequests.length > 0, "Browser prompt did not reach mock provider: " + await evaluate('document.getElementById("errorBanner").textContent'));
  assert(JSON.stringify(modelRequests[0].messages).includes(marker), "File selected with + did not reach provider messages: " + JSON.stringify(modelRequests[0].messages));
  await waitFor('document.getElementById("conversation").textContent.includes("TEST-ATTACHMENT-731")', "provider answer marker");
  assert(await evaluate('document.getElementById("conversation").querySelector(".message-attachment")?.textContent.includes("test.txt")'), "Uploaded filename is absent from persisted message display");
  outcomes.push("file picker attachment reaches actual provider HTTP request and answer");
  assert(!fs.existsSync(path.join(project, "test.txt")), "Upload should not write an unsolicited workspace copy");
  // Delay the catalog to reproduce account cards rendering before API keys.
  await cdp("Page.addScriptToEvaluateOnNewDocument", {source:'(() => { const original=window.fetch.bind(window); window.fetch=async (...args) => { const response=await original(...args); if(String(args[0]).includes("/local/providers/catalog")) await new Promise(resolve=>setTimeout(resolve,1000)); return response; }; })();'});
  await evaluate('globalThis.__beforeProviderReload=true');
  const stopped = new Promise(resolve => native.once("exit", resolve));
  native.kill();
  await Promise.race([stopped, delay(5000).then(() => { throw Error("Launcher did not stop for restart test"); })]);
  authenticatedURL = await startNative();
  await cdp("Page.navigate", {url:authenticatedURL});
  await waitFor('globalThis.__beforeProviderReload!==true && Array.from(document.getElementById("modelSelect")?.options || []).some(o=>o.value==="openrouter::fixture-model")', "provider catalog restored");
  await evaluate('document.getElementById("settingsButton").click(); document.querySelector("[data-settings-section=providers]").click()');
  await waitFor('document.querySelector("[data-provider-account-id=openrouter]") !== null', "OpenRouter settings card");
  await delay(600);
  const card = await evaluate('document.querySelector("[data-provider-account-id=openrouter]").textContent');
  assert(card.includes("API connected"), "Configured OpenRouter incorrectly shown disconnected after process restart: " + card);
  const button = await evaluate('({signedIn:document.getElementById("accountButton").classList.contains("signed-in"),title:document.getElementById("accountButton").title})');
  assert(button.signedIn && button.title.includes("1 provider"), "Header ignores API-key connections: " + JSON.stringify(button));
  outcomes.push("provider card and header agree with persisted API credential after process restart");
  console.log(JSON.stringify({ok:true,platform:process.platform,backend:process.argv.includes("--sqlite")?"sqlite":"json",native:true,realBrowser:true,scenarios:outcomes},null,2));
} catch (error) {
  console.error("WINDOWS PROVIDER/ATTACHMENT ACCEPTANCE FAILED:", error.stack || error);
  console.error("Native process output:", processLog.slice(-3000));
  console.error("Browser output:", chromeLog.slice(-3000));
  process.exitCode = 1;
} finally {
  dispose();
  fixture.closeAllConnections();
  await new Promise(resolve => fixture.close(resolve));
  assert(path.dirname(path.resolve(temp)) === path.resolve(os.tmpdir()) &&
    path.basename(temp).startsWith("tl-provider-acceptance-"), "Refusing cleanup outside the acceptance temp directory");
  // Do not remove until after child processes have been terminated.
  for (let n = 0; n < 15; n++) {
    try { fs.rmSync(temp, { recursive: true, force: true }); break; }
    catch { await delay(100); }
  }
}
