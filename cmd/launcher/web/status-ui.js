(() => {
  "use strict";
  const K = window.KLU;
  const baseRenderMessages = K.renderMessages;
  const RESUME_PROMPT = "Continue the current task from the existing workspace state. Inspect what is already complete, do not repeat finished work, and finish the user's latest request.";
  const PROJECT_MESSAGE_LIMIT = 1000;
  const projectUsageCache = new Map();
  let projectUsageLoading = false;
  let projectUsageTimer = null;

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
      .filter((text) => text.length > 0)
      .join("\n");
  };

  const isResumeMessage = (message) => messageRole(message) === "user"
    && exactUserText(message).trim() === RESUME_PROMPT;

  const hideResumeMessages = () => {
    const rows = [...(K.els.conversation?.querySelectorAll(".message.user") || [])];
    const messages = K.state.messages.filter((message) => messageRole(message) === "user");
    rows.forEach((row, index) => {
      if (isResumeMessage(messages[index])) row.remove();
    });
  };

  const normalizeCancellation = () => {
    const rows = K.els.conversation?.querySelectorAll(".message.error") || [];
    for (const row of rows) {
      const error = row.querySelector(".message-error-text");
      const text = String(error?.textContent || "").trim();
      if (!/\b(aborted|cancelled|canceled|interrupted)\b/i.test(text)) continue;
      row.classList.remove("error");
      row.classList.add("cancelled");
      if (error) {
        error.classList.remove("message-error-text");
        error.classList.add("message-cancelled-text");
        error.textContent = "Cancelled by user";
      }
    }
  };

  const normalizeRetryableToolErrors = () => {
    const cards = K.els.conversation?.querySelectorAll('.activity-card[data-status="failed"]') || [];
    const running = !!K.state.session && (K.state.sending || K.isSessionRunning(K.state.session.id));
    for (const card of cards) {
      const text = String(card.textContent || "");
      const invalidArguments = /invalid arguments/i.test(text)
        && /is missing and is required/i.test(text);
      if (!invalidArguments) continue;

      card.dataset.status = "retrying";
      if (running) card.open = false;

      const status = card.querySelector(".activity-status");
      if (status) status.textContent = running ? "retrying" : "invalid args";

      const meta = card.querySelector(".activity-meta");
      if (meta && !meta.textContent.trim()) {
        meta.textContent = running ? "agent is correcting the tool call" : "tool call was rejected before execution";
      }
    }
  };

  const fallbackCopy = (text) => {
    const area = document.createElement("textarea");
    area.value = text;
    area.setAttribute("readonly", "");
    area.style.position = "fixed";
    area.style.opacity = "0";
    area.style.pointerEvents = "none";
    document.body.appendChild(area);
    area.select();
    const ok = document.execCommand("copy");
    area.remove();
    if (!ok) throw new Error("Copy command failed");
  };

  const copyText = async (text, button) => {
    try {
      if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(text);
      else fallbackCopy(text);
      const previous = button.getAttribute("aria-label") || "Copy prompt";
      button.classList.add("copied");
      button.setAttribute("aria-label", "Copied");
      button.title = "Copied";
      window.setTimeout(() => {
        button.classList.remove("copied");
        button.setAttribute("aria-label", previous);
        button.title = previous;
      }, 1300);
    } catch (error) {
      K.showError?.(`Could not copy prompt: ${error.message || String(error)}`);
    }
  };

  const copyIcon = () => {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("viewBox", "0 0 24 24");
    svg.setAttribute("aria-hidden", "true");
    svg.innerHTML = '<rect x="8" y="8" width="11" height="11" rx="2" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/>';
    return svg;
  };

  const addPromptCopyButtons = () => {
    const prompts = K.state.messages
      .filter((message) => messageRole(message) === "user" && !isResumeMessage(message))
      .map(exactUserText);
    const rows = K.els.conversation?.querySelectorAll(".message.user") || [];
    rows.forEach((row, index) => {
      const content = row.querySelector(".message-content");
      if (!content || content.querySelector(".prompt-copy-button")) return;
      const text = prompts[index] ?? String(row.querySelector(".message-text")?.textContent || "");
      const actions = document.createElement("div");
      actions.className = "user-message-actions";
      const button = document.createElement("button");
      button.type = "button";
      button.className = "prompt-copy-button";
      button.setAttribute("aria-label", "Copy prompt");
      button.title = "Copy prompt";
      button.appendChild(copyIcon());
      button.addEventListener("click", () => copyText(text, button));
      actions.appendChild(button);
      content.appendChild(actions);
    });
  };

  const emptyTokens = () => ({ input: 0, output: 0, reasoning: 0, cacheRead: 0, cacheWrite: 0 });

  const tokenShape = (value) => {
    const tokens = value && typeof value === "object" ? value : {};
    const cache = tokens.cache && typeof tokens.cache === "object" ? tokens.cache : {};
    return {
      input: Number(tokens.input || 0),
      output: Number(tokens.output || 0),
      reasoning: Number(tokens.reasoning || 0),
      cacheRead: Number(cache.read || 0),
      cacheWrite: Number(cache.write || 0),
    };
  };

  const addTokens = (target, source) => {
    target.input += Number(source.input || 0);
    target.output += Number(source.output || 0);
    target.reasoning += Number(source.reasoning || 0);
    target.cacheRead += Number(source.cacheRead || 0);
    target.cacheWrite += Number(source.cacheWrite || 0);
    return target;
  };

  const tokenTotal = (tokens) => tokens.input + tokens.output + tokens.reasoning + tokens.cacheRead + tokens.cacheWrite;

  const assistantTokens = (message) => {
    const direct = tokenShape(message?.info?.tokens || message?.tokens);
    if (tokenTotal(direct) > 0) return direct;

    const total = emptyTokens();
    for (const part of partsOf(message)) {
      if (part?.type !== "step-finish") continue;
      addTokens(total, tokenShape(part.tokens));
    }
    return total;
  };

  const partTimestamp = (part) => {
    const time = part?.time || {};
    return Number(time.end || time.completed || time.start || 0);
  };

  const endTimestamp = (message) => {
    const time = messageTime(message);
    let end = Number(time.completed || time.updated || time.created || 0);
    for (const part of partsOf(message)) end = Math.max(end, partTimestamp(part));
    return end;
  };

  const activeWorkMs = (messages, { running = false } = {}) => {
    let total = 0;
    for (let index = 0; index < messages.length; index++) {
      if (messageRole(messages[index]) !== "user") continue;
      const start = Number(messageTime(messages[index]).created || 0);
      if (!start) continue;

      let end = start;
      let cursor = index + 1;
      for (; cursor < messages.length; cursor++) {
        if (messageRole(messages[cursor]) === "user") break;
        end = Math.max(end, endTimestamp(messages[cursor]));
      }

      const isLastTurn = cursor >= messages.length;
      if (isLastTurn && running) end = Math.max(end, Date.now());
      if (end > start) total += end - start;
    }
    return total;
  };

  const usageForMessages = (messages, { running = false } = {}) => {
    const totals = emptyTokens();
    let requests = 0;
    for (const message of messages) {
      if (messageRole(message) !== "assistant") continue;
      requests++;
      addTokens(totals, assistantTokens(message));
    }
    return {
      tokens: tokenTotal(totals),
      requests,
      duration: activeWorkMs(messages, { running }),
      breakdown: totals,
    };
  };

  const compactNumber = (value) => {
    const number = Number(value || 0);
    if (number < 1000) return String(Math.round(number));
    if (number < 1_000_000) return `${(number / 1000).toFixed(number < 10_000 ? 1 : 0)}K`;
    return `${(number / 1_000_000).toFixed(number < 10_000_000 ? 1 : 0)}M`;
  };

  const formatDuration = (milliseconds) => {
    const seconds = Math.max(0, Math.round(Number(milliseconds || 0) / 1000));
    if (seconds < 60) return `${seconds}s`;
    const minutes = Math.floor(seconds / 60);
    const remaining = seconds % 60;
    if (minutes < 60) return `${minutes}m ${remaining}s`;
    const hours = Math.floor(minutes / 60);
    return `${hours}h ${minutes % 60}m`;
  };

  const usageTitle = (breakdown) => `Input ${compactNumber(breakdown.input)} · Output ${compactNumber(breakdown.output)} · Reasoning ${compactNumber(breakdown.reasoning)} · Cache read ${compactNumber(breakdown.cacheRead)} · Cache write ${compactNumber(breakdown.cacheWrite)}`;

  const turnGroups = () => {
    const turns = [];
    let current = null;
    for (const message of K.state.messages) {
      if (messageRole(message) === "user") {
        if (isResumeMessage(message)) {
          if (!current) current = [message];
          else current.push(message);
          continue;
        }
        if (current) turns.push(current);
        current = [message];
        continue;
      }
      if (current) current.push(message);
    }
    if (current) turns.push(current);
    return turns;
  };

  const turnUsageLine = (stats) => {
    const line = document.createElement("div");
    line.className = "turn-usage";
    line.title = usageTitle(stats.breakdown);
    line.textContent = `Usage · ${compactNumber(stats.tokens)} tokens · ${stats.requests} request${stats.requests === 1 ? "" : "s"} · ${formatDuration(stats.duration)}`;
    return line;
  };

  const renderTurnUsage = () => {
    const view = K.els.conversation;
    if (!view) return;
    const userRows = [...view.querySelectorAll(".message.user")];
    const turns = turnGroups();
    const running = !!K.state.session && (K.state.sending || K.isSessionRunning(K.state.session.id));

    turns.forEach((messages, index) => {
      const stats = usageForMessages(messages, { running: running && index === turns.length - 1 });
      if (!stats.requests && !stats.tokens) return;
      const line = turnUsageLine(stats);
      const nextUser = userRows[index + 1];
      if (nextUser) {
        view.insertBefore(line, nextUser);
        return;
      }
      const working = view.querySelector(".working-message");
      if (working) view.insertBefore(line, working);
      else view.appendChild(line);
    });
  };

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

  const modelRouteSummary = (message) => {
    const steps = routedModelSteps(message);
    const groups = [];
    for (const step of steps) {
      const label = modelLabel(step);
      if (!label) continue;
      const previous = groups[groups.length - 1];
      if (previous?.label === label) previous.count += 1;
      else groups.push({ label, count: 1 });
    }
    return groups.map((item) => item.count > 1 ? `${item.label} ×${item.count}` : item.label).join(" → ");
  };

  const attemptNumberAt = (targetIndex) => {
    let attempt = 1;
    let seenTurn = false;
    for (let index = 0; index <= targetIndex && index < K.state.messages.length; index++) {
      const message = K.state.messages[index];
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

  const lastRecordedModelBefore = (targetIndex) => {
    for (let index = Math.min(targetIndex, K.state.messages.length - 1); index >= 0; index--) {
      const steps = routedModelSteps(K.state.messages[index]);
      if (!steps.length) continue;
      const model = steps[steps.length - 1];
      const label = modelLabel(model);
      if (label) return { ...model, label, messageIndex: index };
    }
    return null;
  };

  const assistantEntries = () => K.state.messages
    .map((message, index) => ({ message, index }))
    .filter(({ message }) => messageRole(message) === "assistant");

  const renderRoutedModels = () => {
    const view = K.els.conversation;
    if (!view) return;
    const rows = [...view.querySelectorAll(".message.assistant:not(.working-message)")];
    const entries = assistantEntries();
    rows.forEach((row, rowIndex) => {
      const entry = entries[rowIndex];
      if (!entry) return;
      const summary = modelRouteSummary(entry.message);
      if (!summary) return;
      const content = row.querySelector(".message-content");
      if (!content || content.querySelector(".routed-model-meta")) return;
      const meta = document.createElement("div");
      meta.className = "routed-model-meta";
      meta.textContent = `Attempt ${attemptNumberAt(entry.index)} · Model route · ${summary}`;
      meta.title = "Routed model IDs recorded by the runtime for completed LLM steps in this attempt";
      content.appendChild(meta);
    });
  };

  const sessionStamp = (session) => String(session?.time?.updated || session?.time?.created || "");

  const normalizePath = (value) => {
    let path = String(value || "").replace(/[\\/]+$/, "").replace(/\\/g, "/");
    if (K.state.local?.platform === "windows") path = path.toLowerCase();
    return path;
  };

  const samePath = (a, b) => normalizePath(a) === normalizePath(b);
  const sessionDirectory = (session) => session?.directory || session?.path || "";
  const usageCacheKey = (session) => `${normalizePath(sessionDirectory(session) || K.state.local?.project || "")}\n${session?.id || ""}`;

  const activeProjectSessions = () => {
    const sessions = Array.isArray(K.state.sessions) ? K.state.sessions : [];
    const activeDirectory = K.state.local?.project || "";
    const currentID = K.state.session?.id || "";
    return sessions.filter((session) => {
      if (!session?.id) return false;
      const directory = sessionDirectory(session);
      if (!directory) return session.id === currentID;
      return !!activeDirectory && samePath(directory, activeDirectory);
    });
  };

  const mergeUsage = (target, source) => {
    target.tokens += Number(source.tokens || 0);
    target.requests += Number(source.requests || 0);
    target.duration += Number(source.duration || 0);
    addTokens(target.breakdown, source.breakdown || emptyTokens());
    return target;
  };

  const projectUsageSnapshot = () => {
    const total = { tokens: 0, requests: 0, duration: 0, breakdown: emptyTokens() };
    const sessions = activeProjectSessions();
    const currentID = K.state.session?.id;
    let complete = true;
    let countedCurrent = false;

    for (const session of sessions) {
      if (session.id === currentID) {
        const running = K.state.sending || K.isSessionRunning(session.id);
        mergeUsage(total, usageForMessages(K.state.messages, { running }));
        countedCurrent = true;
        continue;
      }

      const cached = projectUsageCache.get(usageCacheKey(session));
      if (cached?.stamp === sessionStamp(session)) mergeUsage(total, cached.usage);
      else {
        complete = false;
        if (cached?.usage) mergeUsage(total, cached.usage);
      }
    }

    if (currentID && !countedCurrent) {
      const running = K.state.sending || K.isSessionRunning(currentID);
      mergeUsage(total, usageForMessages(K.state.messages, { running }));
    }

    return { ...total, complete };
  };

  const renderProjectUsage = () => {
    const view = K.els.conversation;
    if (!view || !K.state.session || !K.state.messages.length) return;
    view.querySelector(".project-usage")?.remove();

    const stats = projectUsageSnapshot();
    const footer = document.createElement("section");
    footer.className = "project-usage";
    footer.setAttribute("aria-label", "Project usage totals");
    footer.title = usageTitle(stats.breakdown);

    const label = document.createElement("span");
    label.className = "project-usage-label";
    label.textContent = stats.complete ? "Project total" : "Project total · updating";

    const value = document.createElement("span");
    value.className = "project-usage-value";
    value.textContent = `${compactNumber(stats.tokens)} tokens · ${stats.requests} request${stats.requests === 1 ? "" : "s"} · ${formatDuration(stats.duration)}`;

    footer.append(label, value);
    view.appendChild(footer);
  };

  const refreshProjectUsage = async () => {
    if (projectUsageLoading) return;
    const sessions = activeProjectSessions().filter((session) => session.id !== K.state.session?.id);
    const pending = sessions.filter((session) => projectUsageCache.get(usageCacheKey(session))?.stamp !== sessionStamp(session));
    if (!pending.length) return;

    projectUsageLoading = true;
    let cursor = 0;
    const worker = async () => {
      while (cursor < pending.length) {
        const session = pending[cursor++];
        try {
          const directory = sessionDirectory(session) || K.state.local?.project || undefined;
          const payload = await K.api.sessions.messages(session.id, { limit: PROJECT_MESSAGE_LIMIT, directory });
          const messages = Array.isArray(payload?.data) ? payload.data : [];
          projectUsageCache.set(usageCacheKey(session), {
            stamp: sessionStamp(session),
            usage: usageForMessages(messages),
          });
        } catch (error) {
          console.warn("[TL Studio] Could not load project usage for session", session.id, error);
        }
      }
    };

    const workers = Array.from({ length: Math.min(4, pending.length) }, () => worker());
    await Promise.all(workers);
    projectUsageLoading = false;
    renderProjectUsage();
  };

  const scheduleProjectUsageRefresh = () => {
    if (projectUsageTimer) window.clearTimeout(projectUsageTimer);
    projectUsageTimer = window.setTimeout(() => {
      projectUsageTimer = null;
      refreshProjectUsage().catch((error) => console.warn("[TL Studio] Project usage refresh failed", error));
    }, 120);
  };

  const timeoutKind = (text) => {
    const value = String(text || "");
    if (/upstream idle timeout exceeded/i.test(value)) return "idle";
    if (/upstream provider timed out while sending the response/i.test(value)) return "provider";
    if (/"type"\s*:\s*"timeout"/i.test(value) && /timed out/i.test(value)) return "provider";
    if (/"code"\s*:\s*503/i.test(value) && /timed out/i.test(value)) return "provider";
    return "";
  };

  const addTimeoutRecovery = () => {
    const view = K.els.conversation;
    const rows = [...(view?.querySelectorAll(".message.error") || [])];
    const assistantRows = [...(view?.querySelectorAll(".message.assistant:not(.working-message)") || [])];
    const entries = assistantEntries();

    for (const row of rows) {
      const error = row.querySelector(".message-error-text");
      const raw = String(error?.textContent || "").trim();
      const kind = timeoutKind(raw);
      if (!error || !kind) continue;
      const content = row.querySelector(".message-content");
      if (!content || content.querySelector(".timeout-recovery")) continue;

      const assistantIndex = assistantRows.indexOf(row);
      const entry = assistantIndex >= 0 ? entries[assistantIndex] : null;
      const attempt = entry ? attemptNumberAt(entry.index) : 1;
      const routed = entry ? lastRecordedModelBefore(entry.index) : null;

      error.dataset.rawError = raw;
      error.title = raw;
      error.textContent = kind === "provider" ? "Upstream provider timeout" : "Upstream model idle timeout";

      const recovery = document.createElement("div");
      recovery.className = "timeout-recovery";
      recovery.title = raw;

      const copy = document.createElement("div");
      copy.className = "timeout-recovery-copy";
      const summary = document.createElement("span");
      summary.textContent = kind === "provider"
        ? "The upstream provider stopped responding before the turn completed. Existing file changes are preserved."
        : "The upstream model stopped responding. Existing file changes are preserved.";
      const meta = document.createElement("span");
      meta.className = "timeout-recovery-meta";
      meta.textContent = routed
        ? `Attempt ${attempt} · Last recorded model · ${routed.label}`
        : `Attempt ${attempt} · Routed model was not recorded before the timeout`;
      copy.append(summary, meta);

      const button = document.createElement("button");
      button.type = "button";
      button.className = "timeout-resume-button";
      button.textContent = "Resume";
      button.title = "Continue the task from the current workspace state";
      button.addEventListener("click", async () => {
        if (K.state.sending || (K.state.session && K.isSessionRunning(K.state.session.id))) return;
        K.els.prompt.value = RESUME_PROMPT;
        K.resizePrompt?.();
        await K.sendPrompt?.();
      });
      recovery.append(copy, button);
      content.appendChild(recovery);
    }
  };

  K.renderMessages = (...args) => {
    const result = baseRenderMessages(...args);
    hideResumeMessages();
    normalizeCancellation();
    normalizeRetryableToolErrors();
    addPromptCopyButtons();
    renderRoutedModels();
    addTimeoutRecovery();
    renderTurnUsage();
    renderProjectUsage();
    scheduleProjectUsageRefresh();
    return result;
  };
})();