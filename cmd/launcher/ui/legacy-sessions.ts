(() => {
  "use strict";

  const K = window.KLU;
  if (!K || K.__legacySessionsInstalled) return;
  K.__legacySessionsInstalled = true;

  const promptPlaceholder = K.els.prompt?.placeholder || "";
  const legacyCache = new Map();

  const normalizePath = (value) => {
    let path = String(value || "").replace(/[\\/]+$/, "").replace(/\\/g, "/");
    if (K.state.local?.platform === "windows") path = path.toLowerCase();
    return path;
  };

  const sessionDirectory = (session) => session?.directory
    || session?.location?.directory
    || session?.location?.project?.directory
    || session?.path
    || "";

  const legacyKey = () => normalizePath(K.state.local?.project || "");
  const isLegacy = (session = K.state.session) => !!session?.__legacy;

  const setLegacyMode = (enabled) => {
    K.state.legacySession = !!enabled;
    if (K.els.prompt) {
      K.els.prompt.disabled = !!enabled;
      K.els.prompt.placeholder = enabled
        ? "Legacy session is read-only · Start a new session to continue"
        : promptPlaceholder;
    }
    if (K.els.sendButton) K.els.sendButton.disabled = !!enabled;
    if (K.els.agentSelect) K.els.agentSelect.disabled = !!enabled;
    if (K.els.modelSelect) K.els.modelSelect.disabled = !!enabled;
    if (K.els.attachButton) K.els.attachButton.disabled = !!enabled;
  };

  const loadLegacyForCurrentProject = async () => {
    const key = legacyKey();
    if (!key || !K.api?.legacySessions?.list) return [];
    if (legacyCache.has(key)) return legacyCache.get(key);

    try {
      const payload = await K.api.legacySessions.list({ order: "desc", limit: 100 });
      const rows = Array.isArray(payload?.data) ? payload.data : [];
      const sessions = rows
        .filter((session) => session?.id)
        .map((session) => {
          const directory = sessionDirectory(session);
          return { ...session, directory, __legacy: true };
        })
        .filter((session) => !!session.directory && normalizePath(session.directory) === key);
      legacyCache.set(key, sessions);
      return sessions;
    } catch (error) {
      console.warn("[TL Studio] Legacy session recovery is unavailable", error);
      return [];
    }
  };

  const baseLoadSessions = K.loadSessions;
  K.loadSessions = async (...args) => {
    const production = await baseLoadSessions(...args);
    const legacy = await loadLegacyForCurrentProject();
    if (!legacy.length) return production;

    const merged = new Map();
    for (const session of Array.isArray(production) ? production : []) {
      if (session?.id) merged.set(session.id, session);
    }
    for (const session of legacy) {
      if (session?.id && !merged.has(session.id)) merged.set(session.id, session);
    }

    K.state.sessions = [...merged.values()].sort((a, b) => {
      const at = Number(a?.time?.updated || a?.time?.created || 0);
      const bt = Number(b?.time?.updated || b?.time?.created || 0);
      return bt - at;
    });
    K.renderSessions();
    return K.state.sessions;
  };

  const baseRenderSessions = K.renderSessions;
  K.renderSessions = (...args) => {
    const result = baseRenderSessions(...args);
    const rows = [...(K.els.sessions?.querySelectorAll(".session-row") || [])];
    rows.forEach((row, index) => {
      const session = K.state.sessions[index];
      if (!session?.__legacy) return;
      row.classList.add("legacy-session");
      row.querySelector(".session-quick-delete")?.remove();
      const meta = row.querySelector(".session-main span");
      if (meta && !/legacy/i.test(meta.textContent || "")) meta.textContent += " · legacy";
      const open = row.querySelector(".session-main");
      if (open) open.title = `${open.title || session.title || "Session"}\nLegacy TL Studio session · read-only`;
    });
    return result;
  };

  const baseLoadMessages = K.loadMessages;
  K.loadMessages = async (...args) => {
    if (!isLegacy()) return baseLoadMessages(...args);
    const revision = ++K.state.revision;
    const payload = await K.api.legacySessions.messages(K.state.session.id, { order: "asc", limit: 500 });
    if (revision === K.state.revision) K.state.messages = Array.isArray(payload?.data) ? payload.data : [];
    return K.state.messages;
  };

  const baseRenderSessionHeader = K.renderSessionHeader;
  K.renderSessionHeader = (...args) => {
    const result = baseRenderSessionHeader(...args);
    if (isLegacy() && K.els.sessionMeta) K.els.sessionMeta.textContent = "Legacy TL Studio session · read-only";
    return result;
  };

  const baseRefreshWorkspaceControls = K.refreshWorkspaceControls;
  if (typeof baseRefreshWorkspaceControls === "function") {
    K.refreshWorkspaceControls = (...args) => {
      const result = baseRefreshWorkspaceControls(...args);
      if (isLegacy()) {
        document.getElementById("changesButton")?.classList.add("hidden");
        document.getElementById("stopButton")?.classList.add("hidden");
        document.getElementById("sessionMenuButton")?.classList.add("hidden");
      }
      return result;
    };
  }

  const openLegacySession = async (session) => {
    K.stopSessionPolling?.();
    K.clearAttachments?.();
    K.state.session = session;
    K.state.messages = [];
    K.state.sending = false;
    K.state.changes = [];
    setLegacyMode(true);
    K.renderSessions();
    K.renderSessionHeader();
    K.showConversation();
    K.showError("");
    K.renderChanges?.();
    K.refreshWorkspaceControls?.();
    try {
      await K.loadMessages();
      K.renderMessages();
    } catch (error) {
      K.showError(`Could not read legacy session: ${error.message || String(error)}`);
    }
  };

  const baseSelectSession = K.selectSession;
  K.selectSession = async (session) => {
    if (session?.__legacy) return openLegacySession(session);
    setLegacyMode(false);
    return baseSelectSession(session);
  };

  const baseNewSession = K.newSession;
  K.newSession = (...args) => {
    setLegacyMode(false);
    return baseNewSession(...args);
  };

  const baseSendPrompt = K.sendPrompt;
  K.sendPrompt = async (...args) => {
    if (isLegacy()) {
      K.showError("This legacy session is read-only. Start a new session to continue working on the project.");
      return;
    }
    return baseSendPrompt(...args);
  };

  const baseDeleteSession = K.deleteSessionFromSidebar;
  if (typeof baseDeleteSession === "function") {
    K.deleteSessionFromSidebar = async (session) => {
      if (session?.__legacy) {
        K.showError("Legacy sessions are shown for recovery and are read-only.");
        return;
      }
      return baseDeleteSession(session);
    };
  }

  const baseAfterProjectChange = K.afterProjectChange;
  K.afterProjectChange = async (...args) => {
    setLegacyMode(false);
    return baseAfterProjectChange(...args);
  };

  K.legacySessions = Object.freeze({
    isLegacy,
    clearCache: () => legacyCache.clear(),
  });
})();
