(() => {
  "use strict";
  const K = window.KLU;
  if (!K || K.__diagnosticsUiInstalled) return;
  K.__diagnosticsUiInstalled = true;

  const baseRenderMessages = K.renderMessages;
  const RESUME_PROMPT = "Continue the current task from the existing workspace state. Inspect what is already complete, do not repeat finished work, and finish the user's latest request.";

  const partsOf = (message) => Array.isArray(message?.parts)
    ? message.parts
    : Array.isArray(message?.content) ? message.content : [];

  const messageRole = (message) => message?.info?.role || message?.type || "";
  const messageTime = (message) => message?.info?.time || message?.time || {};

  const exactUserText = (message) => {
    if (typeof message?.text === "string") return message.text;
    return partsOf(message)
      .filter((part) => part?.type === "text" && !part.ignored)
      .map((part) => typeof part.text === "string" ? part.text : "")
      .filter(Boolean)
      .join("\n");
  };

  const isResumeMessage = (message) => messageRole(message) === "user"
    && exactUserText(message).trim() === RESUME_PROMPT;

  const routedModelSteps = (message) => partsOf(message)
    .filter((part) => part?.type === "step-finish" && part?.model?.modelID)
    .map((part) => ({
      providerID: String(part.model.providerID || ""),
      modelID: String(part.model.modelID || ""),
      elapsed: Number(part?.time?.elapsed || 0),
    }));

  const modelLabel = (model) => {
    const modelID = String(model?.modelID || "").trim();
    const providerID = String(model?.providerID || "").trim();
    if (!modelID) return "";
    if (!providerID || providerID === K.api.hosted.providerID || modelID.includes("/")) return modelID;
    return `${providerID}/${modelID}`;
  };

  const attemptNumberAt = (targetIndex, messages = K.state.messages) => {
    let attempt = 1;
    let seenTurn = false;
    for (let index = 0; index <= targetIndex && index < messages.length; index++) {
      const message = messages[index];
      if (messageRole(message) !== "user") continue;
      if (isResumeMessage(message)) {
        if (seenTurn) attempt += 1;
        else seenTurn = true;
        continue;
      }
      seenTurn = true;
      attempt = 1;
    }
    return attempt;
  };

  const attemptStartIndex = (targetIndex, messages = K.state.messages) => {
    for (let index = Math.min(targetIndex, messages.length - 1); index >= 0; index--) {
      if (messageRole(messages[index]) === "user") return index;
    }
    return 0;
  };

  const lastRecordedModelInAttempt = (targetIndex, messages = K.state.messages) => {
    const start = attemptStartIndex(targetIndex, messages);
    for (let index = Math.min(targetIndex, messages.length - 1); index > start; index--) {
      const steps = routedModelSteps(messages[index]);
      if (!steps.length) continue;
      const model = steps[steps.length - 1];
      const label = modelLabel(model);
      if (label) return { ...model, label, messageIndex: index };
    }
    return null;
  };

  const timestampOf = (value) => {
    const number = Number(value || 0);
    return Number.isFinite(number) ? number : 0;
  };

  const lastActivityAt = (messages = K.state.messages) => {
    let latest = 0;
    for (const message of messages) {
      const time = messageTime(message);
      latest = Math.max(latest,
        timestampOf(time.created),
        timestampOf(time.updated),
        timestampOf(time.completed));
      for (const part of partsOf(message)) {
        const partTime = part?.time || {};
        const stateTime = part?.state?.time || {};
        latest = Math.max(latest,
          timestampOf(partTime.start),
          timestampOf(partTime.end),
          timestampOf(partTime.completed),
          timestampOf(stateTime.start),
          timestampOf(stateTime.end));
      }
    }
    return latest;
  };

  const shortDuration = (milliseconds) => {
    const seconds = Math.max(0, Math.floor(Number(milliseconds || 0) / 1000));
    if (seconds < 60) return `${seconds}s`;
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
    const hours = Math.floor(minutes / 60);
    return `${hours}h ${minutes % 60}m`;
  };

  const retryMessage = (input) => {
    const raw = String(input || "").trim();
    if (!raw) return "";
    let value = raw;
    try {
      const parsed = JSON.parse(raw);
      value = parsed?.message || parsed?.error?.message || raw;
    } catch {}
    if (/upstream idle timeout exceeded/i.test(value)) return "Upstream model idle timeout";
    if (/upstream provider timed out while sending the response/i.test(value)) return "Upstream provider timeout";
    return String(value).replace(/\s+/g, " ").slice(0, 180);
  };

  const workingStatusSnapshot = (now = Date.now(), messages = K.state.messages) => {
    const sessionID = K.state.session?.id;
    const status = sessionID ? K.state.activeSessions?.[sessionID] : undefined;
    const targetIndex = Math.max(0, messages.length - 1);
    const tlAttempt = attemptNumberAt(targetIndex, messages);
    const routed = lastRecordedModelInAttempt(targetIndex, messages);
    const lastActivity = lastActivityAt(messages);
    const idleFor = lastActivity ? Math.max(0, now - lastActivity) : 0;

    if (status?.type === "retry") {
      const next = timestampOf(status.next);
      const untilNext = next ? next - now : 0;
      const retry = Number(status.attempt || 0);
      return {
        type: "retry",
        title: "Retrying upstream",
        meta: [
          `TL attempt ${tlAttempt}`,
          `provider retry ${retry}`,
          next ? (untilNext > 0 ? `next in ${shortDuration(untilNext)}` : `retry due ${shortDuration(-untilNext)} ago`) : "",
        ].filter(Boolean).join(" · "),
        detail: retryMessage(status.message),
        model: routed?.label || "",
        idleFor,
        stale: false,
      };
    }

    if (status?.type === "busy") {
      const stale = idleFor >= 120_000;
      return {
        type: "busy",
        title: stale ? "Still busy" : "Working",
        meta: [
          `TL attempt ${tlAttempt}`,
          stale ? `no new session activity for ${shortDuration(idleFor)}` : "",
        ].filter(Boolean).join(" · "),
        detail: stale ? "The runtime still reports this session as busy." : "",
        model: routed?.label || "",
        idleFor,
        stale,
      };
    }

    return {
      type: K.state.sending ? "starting" : String(status?.type || "unknown"),
      title: K.state.sending ? "Starting" : "Working",
      meta: `TL attempt ${tlAttempt}`,
      detail: "",
      model: routed?.label || "",
      idleFor,
      stale: false,
    };
  };

  let recovering = false;

  const waitForSessionIdle = async (sessionID, timeoutMs = 7000) => {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      try { await K.loadActiveSessions?.(); } catch {}
      if (!K.isSessionRunning?.(sessionID)) return true;
      await new Promise((resolve) => window.setTimeout(resolve, 250));
    }
    try { await K.loadActiveSessions?.(); } catch {}
    return !K.isSessionRunning?.(sessionID);
  };

  const recoverStalledSession = async (button) => {
    const session = K.state.session;
    const snapshot = workingStatusSnapshot();
    if (!session || recovering || snapshot.type !== "busy" || !snapshot.stale) return false;

    recovering = true;
    const previousLabel = button?.textContent || "Recover";
    if (button) {
      button.disabled = true;
      button.textContent = "Recovering…";
    }
    K.showError?.("");

    try {
      await K.api.sessions.abort(session.id, { scope: "session" });
      K.state.sending = false;
      K.stopSessionPolling?.();

      const idle = await waitForSessionIdle(session.id);
      if (!idle) throw new Error("The runtime still reports this session as busy after interrupt.");

      const reloads = [];
      if (K.loadMessages) reloads.push(K.loadMessages().catch(() => []));
      if (K.loadAttention) reloads.push(K.loadAttention().catch(() => {}));
      if (reloads.length) await Promise.all(reloads);
      K.renderMessages?.();

      if (!K.els.prompt) throw new Error("Prompt editor is unavailable.");
      K.els.prompt.value = RESUME_PROMPT;
      K.resizePrompt?.();
      await K.sendPrompt?.();
      return true;
    } catch (error) {
      K.showError?.(`Recovery failed: ${error.message || String(error)}`);
      return false;
    } finally {
      recovering = false;
      if (button?.isConnected) {
        button.disabled = false;
        button.textContent = previousLabel;
      }
    }
  };

  const timeoutEntries = () => K.state.messages
    .map((message, index) => ({ message, index }))
    .filter(({ message }) => messageRole(message) === "assistant");

  const correctTimeoutModelMetadata = () => {
    const view = K.els.conversation;
    if (!view) return;
    const rows = [...view.querySelectorAll(".message.assistant:not(.working-message)")];
    const entries = timeoutEntries();
    rows.forEach((row, rowIndex) => {
      if (!row.classList.contains("error")) return;
      const meta = row.querySelector(".timeout-recovery-meta");
      if (!meta) return;
      const entry = entries[rowIndex];
      if (!entry) return;
      const attempt = attemptNumberAt(entry.index);
      const routed = lastRecordedModelInAttempt(entry.index);
      meta.textContent = routed
        ? `Attempt ${attempt} · Last model recorded in this attempt · ${routed.label}`
        : `Attempt ${attempt} · No completed routed model was recorded in this attempt`;
      meta.title = routed
        ? "This model was recorded by the runtime on a completed LLM step inside this attempt."
        : "The failed attempt ended before the runtime recorded a completed routed-model step, so TL Studio will not reuse a model from an earlier attempt.";
    });
  };

  const decorateWorkingStatus = () => {
    const view = K.els.conversation;
    if (!view) return;
    const row = view.querySelector(".working-message");
    if (!row) return;
    const content = row.querySelector(".message-content");
    const body = row.querySelector(".message-text");
    if (!content || !body) return;

    const snapshot = workingStatusSnapshot();
    row.dataset.sessionStatus = snapshot.type;
    row.classList.toggle("stalled", snapshot.stale);

    body.textContent = "";
    const title = document.createElement("span");
    title.className = "session-live-title";
    title.textContent = snapshot.title;
    body.appendChild(title);
    if (snapshot.type === "busy" && !snapshot.stale || snapshot.type === "starting") {
      const typing = document.createElement("span");
      typing.className = "typing";
      typing.append(document.createElement("i"), document.createElement("i"), document.createElement("i"));
      body.append(" ", typing);
    }

    content.querySelector(".session-live-meta")?.remove();
    content.querySelector(".session-recovery-actions")?.remove();
    const meta = document.createElement("div");
    meta.className = "session-live-meta";
    const lines = [snapshot.meta];
    if (snapshot.detail) lines.push(snapshot.detail);
    if (snapshot.model) lines.push(`Last completed model in this attempt · ${snapshot.model}`);
    else lines.push("No completed routed model recorded in this attempt yet");
    meta.textContent = lines.filter(Boolean).join("\n");
    content.appendChild(meta);

    if (snapshot.type === "busy" && snapshot.stale) {
      const actions = document.createElement("div");
      actions.className = "session-recovery-actions";
      const button = document.createElement("button");
      button.type = "button";
      button.className = "timeout-resume-button session-recover-button";
      button.textContent = "Recover";
      button.title = "Interrupt the stuck turn and resume from the preserved workspace state";
      button.addEventListener("click", () => recoverStalledSession(button));
      actions.appendChild(button);
      content.appendChild(actions);
    }
  };

  const installStyles = () => {
    if (document.getElementById("tl-session-diagnostics-style")) return;
    const style = document.createElement("style");
    style.id = "tl-session-diagnostics-style";
    style.textContent = `
      .session-live-title { font-weight: 650; }
      .session-live-meta { margin-top: 5px; white-space: pre-line; color: var(--muted-2); font-size: 9px; line-height: 1.5; }
      .working-message[data-session-status="retry"] .session-live-title { color: #d8b96f; }
      .working-message.stalled .session-live-title { color: #d8b96f; }
      .session-recovery-actions { margin-top: 10px; display: flex; justify-content: flex-start; }
      .session-recover-button { min-width: 76px; }
    `;
    document.head.appendChild(style);
  };

  K.renderMessages = (...args) => {
    const result = baseRenderMessages(...args);
    correctTimeoutModelMetadata();
    decorateWorkingStatus();
    return result;
  };

  K.__statusDiagnostics = {
    attemptNumberAt,
    attemptStartIndex,
    lastRecordedModelInAttempt,
    lastActivityAt,
    retryMessage,
    workingStatusSnapshot,
    waitForSessionIdle,
    recoverStalledSession,
  };

  installStyles();
  K.renderMessages();
})();