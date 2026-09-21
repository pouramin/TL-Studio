(() => {
  "use strict";
  const K = window.KLU;
  if (!K || K.__terminalInstalled) return;
  K.__terminalInstalled = true;

  const ensureStyles = () => {
    if (document.querySelector('link[href="/terminal.css"]')) return;
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = "/terminal.css";
    document.head.appendChild(link);
  };

  const request = async (path: string, options: RequestInit = {}) => {
    const response = await fetch(path, {
      cache: "no-store",
      ...options,
      headers: { ...(options.body ? { "Content-Type": "application/json" } : {}), ...(options.headers || {}) },
    });
    const type = response.headers.get("content-type") || "";
    const payload = response.status === 204
      ? null
      : type.includes("application/json") ? await response.json().catch(() => null) : await response.text().catch(() => "");
    if (!response.ok) {
      const detail = payload && typeof payload === "object" ? payload.error || payload.message || JSON.stringify(payload) : String(payload || `${response.status} ${response.statusText}`);
      throw new Error(detail);
    }
    return payload;
  };

  ensureStyles();

  const toolbar = document.querySelector(".toolbar-controls");
  const main = document.querySelector(".main-pane");
  if (!toolbar || !main) return;

  const button = document.createElement("button");
  button.id = "terminalButton";
  button.type = "button";
  button.className = "ghost small";
  button.textContent = "Terminal";
  button.title = "Open project terminal";
  toolbar.insertBefore(button, toolbar.firstChild);

  const panel = document.createElement("section");
  panel.id = "terminalPanel";
  panel.className = "terminal-panel hidden";
  panel.setAttribute("aria-label", "Project terminal");
  panel.innerHTML = `
    <div class="terminal-head">
      <div class="terminal-head-copy"><strong>Terminal</strong><span id="terminalCwd">Project root</span></div>
      <div class="terminal-actions">
        <button id="terminalStop" class="ghost small" type="button" disabled>Stop</button>
        <button id="terminalClear" class="ghost small" type="button">Clear</button>
        <button id="terminalClose" class="icon-button" type="button" aria-label="Close terminal">×</button>
      </div>
    </div>
    <pre id="terminalOutput" class="terminal-output" aria-live="polite"></pre>
    <form id="terminalForm" class="terminal-input-row">
      <span class="terminal-prompt">›</span>
      <input id="terminalInput" class="terminal-input" autocomplete="off" spellcheck="false" placeholder="Run a command in this project…" />
      <button id="terminalRun" class="primary small terminal-run" type="submit">Run</button>
    </form>`;
  main.appendChild(panel);

  const ui: TLStudioDynamicRecord = {
    button,
    panel,
    cwd: document.getElementById("terminalCwd"),
    output: document.getElementById("terminalOutput"),
    form: document.getElementById("terminalForm"),
    input: document.getElementById("terminalInput"),
    run: document.getElementById("terminalRun"),
    stop: document.getElementById("terminalStop"),
    clear: document.getElementById("terminalClear"),
    close: document.getElementById("terminalClose"),
  };

  K.state.terminal = { process: null, poll: null, history: [], historyIndex: 0, transcript: "", stoppedByUser: false };

  const projectLabel = () => K.state.local?.project || "Project root";
  const setCwd = (value) => {
    const cwd = value || projectLabel();
    ui.cwd.textContent = cwd;
    ui.cwd.title = cwd;
  };
  const setOpen = (open) => {
    ui.panel.classList.toggle("hidden", !open);
    ui.button.classList.toggle("terminal-toggle-active", open);
    if (open) {
      setCwd(projectLabel());
      window.setTimeout(() => ui.input.focus(), 0);
    }
  };

  const append = (text) => {
    K.state.terminal.transcript += String(text || "");
    if (K.state.terminal.transcript.length > 1_000_000) K.state.terminal.transcript = K.state.terminal.transcript.slice(-1_000_000);
    ui.output.textContent = K.state.terminal.transcript;
    ui.output.scrollTop = ui.output.scrollHeight;
  };

  const stopPolling = () => {
    if (K.state.terminal.poll) window.clearInterval(K.state.terminal.poll);
    K.state.terminal.poll = null;
  };

  const renderSnapshot = (snapshot) => {
    const previous = K.state.terminal.process?.output || "";
    K.state.terminal.process = snapshot;
    if (snapshot?.cwd) setCwd(snapshot.cwd);
    if (snapshot?.output && snapshot.output !== previous) {
      const delta = snapshot.output.startsWith(previous) ? snapshot.output.slice(previous.length) : snapshot.output;
      append(delta);
    }
    const running = !!snapshot?.running;
    ui.stop.disabled = !running;
    ui.run.disabled = running;
    ui.input.disabled = running;
    if (!running && snapshot) {
      stopPolling();
      if (K.state.terminal.stoppedByUser) {
        append("\n[stopped]\n");
      } else {
        const code = snapshot.exitCode;
        append(`\n[exit ${code == null ? "?" : code}]\n`);
      }
      K.state.terminal.stoppedByUser = false;
      K.state.terminal.process = null;
      ui.input.disabled = false;
      ui.run.disabled = false;
      ui.stop.disabled = true;
      ui.input.focus();
    }
  };

  const pollProcess = async () => {
    const id = K.state.terminal.process?.id;
    if (!id) return stopPolling();
    try {
      renderSnapshot(await request(`/local/process/${encodeURIComponent(id)}`));
    } catch (error) {
      stopPolling();
      append(`\n[terminal error] ${error.message || error}\n`);
      ui.input.disabled = false;
      ui.run.disabled = false;
      ui.stop.disabled = true;
    }
  };

  const runCommand = async (command) => {
    command = String(command || "").trim();
    if (!command || K.state.terminal.process?.running) return;
    setOpen(true);
    append(`\n› ${command}\n`);
    K.state.terminal.history.push(command);
    K.state.terminal.historyIndex = K.state.terminal.history.length;
    K.state.terminal.stoppedByUser = false;
    ui.input.value = "";
    ui.input.disabled = true;
    ui.run.disabled = true;
    try {
      const snapshot = await request("/local/process", { method: "POST", body: JSON.stringify({ command }) });
      if (snapshot?.cwd) setCwd(snapshot.cwd);
      K.state.terminal.process = { ...snapshot, output: "" };
      ui.stop.disabled = false;
      stopPolling();
      K.state.terminal.poll = window.setInterval(pollProcess, 250);
      await pollProcess();
    } catch (error) {
      append(`[terminal error] ${error.message || error}\n`);
      K.state.terminal.process = null;
      K.state.terminal.stoppedByUser = false;
      ui.input.disabled = false;
      ui.run.disabled = false;
      ui.stop.disabled = true;
      ui.input.focus();
    }
  };

  const stopProcess = async () => {
    const id = K.state.terminal.process?.id;
    if (!id) return;
    ui.stop.disabled = true;
    K.state.terminal.stoppedByUser = true;
    try {
      await request(`/local/process/${encodeURIComponent(id)}`, { method: "DELETE" });
      append("\n[stopping process…]\n");
    } catch (error) {
      K.state.terminal.stoppedByUser = false;
      append(`\n[stop error] ${error.message || error}\n`);
    }
  };

  ui.button.addEventListener("click", () => setOpen(ui.panel.classList.contains("hidden")));
  ui.close.addEventListener("click", () => setOpen(false));
  ui.clear.addEventListener("click", () => { K.state.terminal.transcript = ""; ui.output.textContent = ""; });
  ui.stop.addEventListener("click", stopProcess);
  ui.form.addEventListener("submit", (event) => { event.preventDefault(); runCommand(ui.input.value); });
  ui.input.addEventListener("keydown", (event) => {
    if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
    const history = K.state.terminal.history;
    if (!history.length) return;
    event.preventDefault();
    if (event.key === "ArrowUp") K.state.terminal.historyIndex = Math.max(0, K.state.terminal.historyIndex - 1);
    else K.state.terminal.historyIndex = Math.min(history.length, K.state.terminal.historyIndex + 1);
    ui.input.value = K.state.terminal.historyIndex >= history.length ? "" : history[K.state.terminal.historyIndex];
    ui.input.setSelectionRange(ui.input.value.length, ui.input.value.length);
  });

  const originalAfterProjectChange = K.afterProjectChange;
  if (typeof originalAfterProjectChange === "function") {
    K.afterProjectChange = async (...args) => {
      if (K.state.terminal.process?.running) await stopProcess();
      const result = await originalAfterProjectChange(...args);
      setCwd(projectLabel());
      return result;
    };
  }

  K.terminal = { open: () => setOpen(true), close: () => setOpen(false), run: runCommand, stop: stopProcess };
})();
