(() => {
  "use strict";
  const K = window.KLU;
  const $ = (id) => document.getElementById(id);

  const ui = {
    changesButton: $("changesButton"),
    changesCount: $("changesCount"),
    changesPanel: $("changesPanel"),
    changesSummary: $("changesSummary"),
    changesList: $("changesList"),
    closeChanges: $("closeChanges"),
    stopButton: $("stopButton"),
    sessionMenuButton: $("sessionMenuButton"),
    sessionDialog: $("sessionDialog"),
    sessionTitleInput: $("sessionTitleInput"),
    sessionSave: $("sessionSave"),
    sessionDelete: $("sessionDelete"),
    sessionCancel: $("sessionCancel"),
  };

  K.state.changes = [];
  K.state.changesLoading = false;
  K.state.sseSettling = false;

  const escapeText = (value) => String(value ?? "");
  const basename = (path) => {
    const bits = escapeText(path).split(/[\\/]/);
    return bits[bits.length - 1] || escapeText(path) || "Unknown file";
  };

  const normalizeChange = (candidate, fallbackPath = "") => {
    if (!candidate || typeof candidate !== "object") return null;
    const file = candidate.file || candidate.filePath || candidate.path || fallbackPath;
    if (!file) return null;
    return {
      file: String(file),
      additions: Number(candidate.additions) || 0,
      deletions: Number(candidate.deletions) || 0,
      patch: typeof candidate.patch === "string" ? candidate.patch : "",
    };
  };

  const mergeChanges = (items) => {
    const merged = new Map();
    for (const raw of Array.isArray(items) ? items : []) {
      const item = normalizeChange(raw);
      if (!item) continue;
      const key = item.file.replace(/\\/g, "/").toLowerCase();
      const previous = merged.get(key);
      if (!previous) {
        merged.set(key, item);
        continue;
      }
      merged.set(key, {
        file: item.file || previous.file,
        additions: previous.additions + item.additions,
        deletions: previous.deletions + item.deletions,
        patch: item.patch || previous.patch,
      });
    }
    return [...merged.values()];
  };

  const changesFromMessages = () => {
    const messages = Array.isArray(K.state.messages) ? K.state.messages : [];

    // Prefer Kilo's projected per-turn summaries when present. They already
    // represent a file-diff shape and avoid reinterpreting tool metadata.
    const projected = [];
    for (const message of messages) {
      const diffs = message?.info?.summary?.diffs;
      if (Array.isArray(diffs)) projected.push(...diffs);
    }
    if (projected.length) return mergeChanges(projected);

    // Fresh/non-git projects can legitimately have an empty aggregate
    // /session/:id/diff even though write/edit tools expose authoritative diff
    // metadata. Fall back to those completed tool parts so Changes still works.
    const toolChanges = [];
    for (const message of messages) {
      const parts = Array.isArray(message?.parts) ? message.parts : [];
      for (const part of parts) {
        if (part?.type !== "tool") continue;
        const state = part.state || {};
        const metadata = state.metadata || {};
        const input = state.input || {};
        const output = state.output ?? state.result ?? part.output ?? part.result;
        const fallbackPath = input.filePath || input.path || input.file || metadata.filepath || metadata.path || "";

        const candidates = [
          metadata.filediff,
          metadata.fileDiff,
          output?.filediff,
          output?.fileDiff,
          output && typeof output === "object" && ("patch" in output || "additions" in output || "deletions" in output) ? output : null,
        ];
        const candidate = candidates.find((value) => value && typeof value === "object");
        const change = normalizeChange(candidate, fallbackPath);
        if (change) toolChanges.push(change);
      }
    }
    return mergeChanges(toolChanges);
  };

  K.refreshWorkspaceControls = () => {
    const session = K.state.session;
    const running = !!session && (K.state.sending || K.isSessionRunning(session.id));
    ui.stopButton?.classList.toggle("hidden", !running);
    ui.changesButton?.classList.toggle("hidden", !session);
    ui.sessionMenuButton?.classList.toggle("hidden", !session);
    if (ui.changesCount) ui.changesCount.textContent = String(K.state.changes?.length || 0);
  };

  K.renderChanges = () => {
    if (!ui.changesList || !ui.changesSummary) return;
    const changes = Array.isArray(K.state.changes) ? K.state.changes : [];
    const additions = changes.reduce((sum, item) => sum + (Number(item?.additions) || 0), 0);
    const deletions = changes.reduce((sum, item) => sum + (Number(item?.deletions) || 0), 0);
    ui.changesSummary.textContent = changes.length
      ? `${changes.length} file${changes.length === 1 ? "" : "s"} · +${additions} −${deletions}`
      : K.state.changesLoading ? "Loading…" : "No changes yet";
    if (ui.changesCount) ui.changesCount.textContent = String(changes.length);
    ui.changesList.textContent = "";

    if (!changes.length) {
      const empty = document.createElement("div");
      empty.className = "changes-empty";
      empty.textContent = K.state.changesLoading ? "Reading session changes…" : "Files changed by this session will appear here.";
      ui.changesList.appendChild(empty);
      return;
    }

    for (const item of changes) {
      const details = document.createElement("details");
      details.className = "change-item";
      const summary = document.createElement("summary");
      const name = document.createElement("strong");
      const stats = document.createElement("span");
      name.textContent = basename(item?.file);
      name.title = escapeText(item?.file);
      stats.className = "change-stats";
      stats.innerHTML = `<b>+${Number(item?.additions) || 0}</b> <i>−${Number(item?.deletions) || 0}</i>`;
      summary.append(name, stats);
      details.appendChild(summary);

      const path = document.createElement("div");
      path.className = "change-path";
      path.textContent = escapeText(item?.file);
      details.appendChild(path);

      const pre = document.createElement("pre");
      pre.className = "change-patch";
      pre.textContent = item?.patch || "Patch preview is unavailable for this file.";
      details.appendChild(pre);
      ui.changesList.appendChild(details);
    }
  };

  K.loadChanges = async () => {
    if (!K.state.session) {
      K.state.changes = [];
      K.renderChanges();
      K.refreshWorkspaceControls();
      return [];
    }
    K.state.changesLoading = true;
    K.renderChanges();
    let aggregate = [];
    try {
      const payload = await K.api.sessions.diff(K.state.session.id);
      aggregate = Array.isArray(payload?.data) ? payload.data : [];
    } catch (error) {
      console.warn("[TL Studio] Could not load aggregate session diff", error);
    }
    K.state.changes = aggregate.length ? mergeChanges(aggregate) : changesFromMessages();
    K.state.changesLoading = false;
    K.renderChanges();
    K.refreshWorkspaceControls();
    return K.state.changes;
  };

  const settleSelectedSession = async () => {
    if (!K.state.session || K.state.sseSettling) return;
    K.state.sseSettling = true;
    K.state.sending = false;
    K.stopSessionPolling?.();
    try {
      await Promise.all([
        K.loadMessages().catch(() => []),
        K.loadSessions().catch(() => []),
        K.loadActiveSessions().catch(() => {}),
        K.loadAttention?.().catch(() => {}),
      ]);
      await K.loadChanges().catch(() => []);
      const fresh = K.state.sessions.find((item) => item.id === K.state.session?.id);
      if (fresh) K.state.session = fresh;
      K.renderMessages();
      K.renderSessionHeader();
      K.renderSessions();
    } finally {
      K.state.sseSettling = false;
      K.refreshWorkspaceControls();
    }
  };

  K.stopCurrentSession = async () => {
    const session = K.state.session;
    if (!session) return;
    K.showError("");
    ui.stopButton.disabled = true;
    try {
      await K.api.sessions.abort(session.id, { scope: "session" });
      K.state.sending = false;
      K.stopSessionPolling?.();
      await settleSelectedSession();
    } catch (error) {
      K.showError(error.message || String(error));
    } finally {
      ui.stopButton.disabled = false;
      K.refreshWorkspaceControls();
    }
  };

  const openSessionManager = () => {
    if (!K.state.session || !ui.sessionDialog) return;
    ui.sessionTitleInput.value = K.state.session.title || "";
    ui.sessionDialog.showModal();
    window.setTimeout(() => { ui.sessionTitleInput.focus(); ui.sessionTitleInput.select(); }, 0);
  };

  const saveSessionTitle = async () => {
    const session = K.state.session;
    if (!session) return;
    const title = ui.sessionTitleInput.value.trim();
    if (!title) return K.showError("Session title cannot be empty.");
    try {
      const fresh = (await K.api.sessions.update(session.id, { title }))?.data;
      if (fresh) K.state.session = fresh;
      if (ui.sessionDialog.open) ui.sessionDialog.close();
      await K.loadSessions();
      K.renderSessionHeader();
      K.renderSessions();
    } catch (error) { K.showError(error.message || String(error)); }
  };

  const deleteSession = async () => {
    const session = K.state.session;
    if (!session) return;
    if (!window.confirm(`Delete “${session.title || "Untitled session"}” permanently?`)) return;
    try {
      if (K.isSessionRunning(session.id) || K.state.sending) {
        await K.api.sessions.abort(session.id, { scope: "tree" }).catch(() => {});
      }
      await K.api.sessions.remove(session.id);
      if (ui.sessionDialog.open) ui.sessionDialog.close();
      K.state.changes = [];
      K.newSession();
      await K.loadSessions();
      K.renderChanges();
      K.refreshWorkspaceControls();
    } catch (error) { K.showError(error.message || String(error)); }
  };

  const originalSelectSession = K.selectSession;
  K.selectSession = async (session) => {
    K.state.changes = [];
    const result = await originalSelectSession(session);
    await K.loadChanges();
    K.refreshWorkspaceControls();
    return result;
  };

  const originalNewSession = K.newSession;
  K.newSession = (...args) => {
    K.state.changes = [];
    ui.changesPanel?.classList.add("hidden");
    const result = originalNewSession(...args);
    K.renderChanges();
    K.refreshWorkspaceControls();
    return result;
  };

  const originalHandleKiloEvent = K.handleKiloEvent;
  K.handleKiloEvent = (event) => {
    const type = event?.type || "";
    const props = event?.properties || event?.data || {};
    const sessionID = props.sessionID || props.info?.sessionID || props.part?.sessionID;

    if (type === "session.status" && sessionID && props.status) K.state.activeSessions[sessionID] = props.status;
    if (type === "session.idle" && sessionID) K.state.activeSessions[sessionID] = { type: "idle" };

    originalHandleKiloEvent(event);
    K.refreshWorkspaceControls();

    if (sessionID && sessionID === K.state.session?.id && (type === "session.diff" || type === "session.idle")) {
      window.setTimeout(() => K.loadChanges().catch(() => {}), 60);
    }
    if (sessionID && sessionID === K.state.session?.id && type === "session.idle") {
      window.setTimeout(() => settleSelectedSession().catch((error) => K.showError(error.message || String(error))), 80);
    }
  };

  // Kilo's global SSE stream is the primary source of progress. Keep only a
  // low-frequency reconciliation watchdog while SSE is connected; retain the
  // original fast polling path as a fallback for browsers without EventSource.
  const fallbackStartSessionPolling = K.startSessionPolling;
  K.startSessionPolling = (startedAt = Date.now()) => {
    const source = K.state.eventSource;
    if (!source || source.readyState === 2) return fallbackStartSessionPolling(startedAt);
    K.stopSessionPolling();
    K.state.sessionPolling = window.setInterval(async () => {
      if (!K.state.session) return K.stopSessionPolling();
      try {
        await K.loadActiveSessions();
        const running = K.isSessionRunning(K.state.session.id);
        if (!running) return settleSelectedSession();
        if (Date.now() - startedAt > 8000) {
          await K.loadMessages();
          K.renderMessages();
        }
        K.refreshWorkspaceControls();
      } catch (error) {
        console.warn("[TL Studio] SSE reconciliation failed", error);
      }
    }, 5000);
  };

  ui.changesButton?.addEventListener("click", async () => {
    ui.changesPanel.classList.remove("hidden");
    await K.loadChanges();
  });
  ui.closeChanges?.addEventListener("click", () => ui.changesPanel.classList.add("hidden"));
  ui.stopButton?.addEventListener("click", K.stopCurrentSession);
  ui.sessionMenuButton?.addEventListener("click", openSessionManager);
  ui.sessionCancel?.addEventListener("click", () => ui.sessionDialog.close());
  ui.sessionSave?.addEventListener("click", saveSessionTitle);
  ui.sessionDelete?.addEventListener("click", deleteSession);
  ui.sessionTitleInput?.addEventListener("keydown", (event) => {
    if (event.key === "Enter") { event.preventDefault(); saveSessionTitle(); }
  });

  K.renderChanges();
  K.refreshWorkspaceControls();
})();
