import { K } from "./kernel";

(() => {
  "use strict";
  

  const safeJSON = (value: any) => {
    try { return JSON.stringify(value, null, 2); }
    catch { return String(value); }
  };

  const errorText = (error: any) => {
    if (!error) return "";
    if (typeof error === "string") return error;
    const message = error.message || error.data?.message || error.error?.message || "";
    const ref = error.ref || error.data?.ref || error.error?.ref || "";
    if (message) return ref && !String(message).includes(ref) ? `${message} [${ref}]` : String(message);
    return safeJSON(error);
  };

  const partsOf = (message: any) => Array.isArray(message?.parts)
    ? message.parts
    : Array.isArray(message?.content) ? message.content : [];

  const textOf = (message: any) => {
    if (typeof message?.text === "string" && message.text) return message.text;
    return partsOf(message)
      .filter((item: any) => item?.type === "text" && !item.ignored)
      .map((item: any) => item.text || "")
      .filter(Boolean)
      .join("\n")
      .trim();
  };

  const toolSummary = (item: any) => {
    const state = item?.state || {};
    const status = state.status || item.status || "pending";
    const details = [];
    try {
      if (state.input && Object.keys(state.input).length) details.push(safeJSON(state.input));
      if (state.title) details.push(state.title);
      const output = state.output ?? state.result ?? item.output ?? item.result;
      if (output !== undefined && output !== "") details.push(typeof output === "string" ? output : safeJSON(output));
      if (state.error) details.push(errorText(state.error));
      if (state.metadata && Object.keys(state.metadata).length) details.push(safeJSON(state.metadata));
    } catch {}
    return { status, detail: details.filter(Boolean).join("\n\n") };
  };

  const toolDescriptor = (item: any) => K.toolDescriptor?.(item?.tool || item?.name || "") || {
    name: "Runtime tool",
    category: "runtime",
    permissionClass: "runtime",
  };

  const messageNode = (kind: any, author: any, text: any, time: any, error: any = "") => {
    const row = document.createElement("article");
    const avatar = document.createElement("div");
    const content = document.createElement("div");
    row.className = `message ${kind}${error ? " error" : ""}`;
    avatar.className = "avatar";
    avatar.textContent = kind === "user" ? "YOU" : kind === "system" ? "SYS" : "AI";
    content.className = "message-content";

    const head = document.createElement("div");
    const strong = document.createElement("strong");
    const stamp = document.createElement("span");
    const body = document.createElement("div");
    head.className = "message-head";
    strong.textContent = author;
    stamp.textContent = K.formatTime(time);
    head.append(strong, stamp);
    body.className = "message-text";
    body.textContent = error ? `${text}${text ? "\n\n" : ""}${error}` : text;
    content.append(head, body);
    row.append(avatar, content);
    return row;
  };

  const toolNode = (name: any, status: any, detail: any) => {
    const card = document.createElement("div");
    const head = document.createElement("div");
    const title = document.createElement("strong");
    const label = document.createElement("span");
    card.className = "tool-card";
    head.className = "tool-head";
    title.textContent = name;
    label.textContent = status;
    head.append(title, label);
    card.appendChild(head);
    if (detail) {
      const body = document.createElement("div");
      body.className = "tool-body";
      body.textContent = detail;
      card.appendChild(body);
    }
    return card;
  };

  const appendParts = (node: any, message: any) => {
    const content = node.querySelector(".message-content");
    for (const item of partsOf(message)) {
      if (item?.type === "reasoning" && item.text) content.appendChild(toolNode("Reasoning", "completed", item.text));
      if (item?.type === "tool") {
        const summary = toolSummary(item);
        const descriptor = toolDescriptor(item);
        content.appendChild(toolNode(descriptor.name, summary.status, summary.detail));
      }
      if (item?.type === "subtask") {
        content.appendChild(toolNode("Subtask", item.status || "created", item.description || item.prompt || ""));
      }
    }
  };

  const renderEnvelope = (view: any, message: any) => {
    if (!message?.info || !Array.isArray(message.parts)) return false;
    const info = message.info;
    const time = info.time?.created ?? info.time?.completed;
    if (info.role === "user") {
      view.appendChild(messageNode("user", "You", textOf(message), time));
      return true;
    }
    if (info.role === "assistant") {
      const node = messageNode("assistant", info.agent || "Agent", textOf(message), time, errorText(info.error));
      appendParts(node, message);
      view.appendChild(node);
      return true;
    }
    return false;
  };

  K.showConversation = () => {
    K.els.emptyState.classList.add("hidden");
    K.els.conversation.classList.remove("hidden");
  };

  K.renderMessages = () => {
    const view = K.els.conversation;
    view.textContent = "";
    for (const message of K.state.messages) {
      if (renderEnvelope(view, message)) continue;

      // Keep alpha-created projected sessions readable while the repository is
      // transitioning from the experimental /api Session model.
      if (message?.type === "user") {
        view.appendChild(messageNode("user", "You", message.text || textOf(message), message.time?.created));
      } else if (message?.type === "assistant") {
        const node = messageNode("assistant", message.agent || "Agent", textOf(message), message.time?.created, errorText(message.error));
        appendParts(node, message);
        view.appendChild(node);
      } else if (message?.type === "shell") {
        const node = messageNode("assistant", "Shell", "", message.time?.created);
        node.querySelector<HTMLElement>(".message-content")!.appendChild(toolNode(message.command || "command", message.time?.completed ? "completed" : "running", message.output || ""));
        view.appendChild(node);
      } else if (message?.type === "system" || message?.type === "synthetic") {
        view.appendChild(messageNode("system", "System", message.text || "", message.time?.created));
      }
    }

    if (K.state.session && (K.isSessionRunning(K.state.session.id) || K.state.sending)) {
      const row = messageNode("assistant", K.state.session.agent || "Agent", "", Date.now());
      row.querySelector<HTMLElement>(".message-text")!.innerHTML = 'Working <span class="typing"><i></i><i></i><i></i></span>';
      view.appendChild(row);
    }
    requestAnimationFrame(() => { view.scrollTop = view.scrollHeight; });
  };

  K.stopSessionPolling = () => {
    if (K.state.sessionPolling) window.clearInterval(K.state.sessionPolling);
    K.state.sessionPolling = null;
  };

  K.stopEvents = () => {
    if (K.state.eventSource) K.state.eventSource.close();
    K.state.eventSource = null;
    if (K.state.fallbackPolling) window.clearInterval(K.state.fallbackPolling);
    K.state.fallbackPolling = null;
    K.stopSessionPolling();
  };

  K.newSession = () => {
    K.stopSessionPolling();
    Object.assign(K.state, { session: null, messages: [], sending: false, attentionKey: "" });
    K.showError("");
    K.renderSessions();
    K.renderSessionHeader();
    K.els.conversation.textContent = "";
    K.els.conversation.classList.add("hidden");
    K.els.emptyState.classList.remove("hidden");
    K.els.prompt.focus();
  };

  K.loadMessages = async () => {
    if (!K.state.session) return [];
    const revision = ++K.state.revision;
    const messages = await K.api.sessionView.messages(K.state.session.id, { limit: 200 });
    if (revision === K.state.revision) K.state.messages = Array.isArray(messages) ? messages : [];
    return K.state.messages;
  };

  K.selectSession = async (session) => {
    K.stopSessionPolling();
    K.state.session = session;
    K.state.messages = [];
    K.renderSessions();
    K.renderSessionHeader();
    K.showConversation();
    K.showError("");
    K.syncSelectors();
    await Promise.all([K.loadMessages(), K.loadActiveSessions(), K.loadAttention?.()]);
    K.renderMessages();
    if (K.isSessionRunning(session.id)) K.startSessionPolling();
  };

  K.createSession = async () => {
    const input: any = {};
    const agent = K.els.agentSelect.value || undefined;
    const model = K.selectedModel();
    if (agent) input.agent = agent;
    if (model) input.model = model;
    const created = (await K.api.sessions.create(input))?.data;
    if (!created?.id) throw new Error("Runtime did not return a session ID");
    const session = await K.api.sessionView.get(created.id).catch(() => null);
    K.state.session = session || { id: created.id, title: created.title || "", directory: K.state.local?.project || "" };
    if (agent) K.state.session.agent = agent;
    if (model) K.state.session.model = model;
    K.showConversation();
    K.renderSessionHeader();
    await K.loadSessions().catch(() => {});
    return session;
  };

  K.ensureSessionSelection = () => {
    const session = K.state.session;
    if (!session) return;
    session.agent = K.els.agentSelect.value || undefined;
    session.model = K.selectedModel();
    K.renderSessionHeader();
  };

  let refreshTimer: number | null = null;
  const scheduleSelectedRefresh = (delay = 80) => {
    if (refreshTimer) window.clearTimeout(refreshTimer);
    refreshTimer = window.setTimeout(async () => {
      refreshTimer = null;
      if (!K.state.session) return;
      try {
        await Promise.all([K.loadMessages(), K.loadActiveSessions(), K.loadAttention?.()]);
        const fresh = await K.api.sessionView.get(K.state.session.id).catch(() => null);
        if (fresh) K.state.session = fresh;
        K.renderMessages();
        K.renderSessionHeader();
        K.renderSessions();
      } catch (err) { K.showError((err as any).message || String(err)); }
    }, delay);
  };

  K.handleLiveEvent = (event) => {
    const type = event?.type || "";
    const selectedID = K.state.session?.id;
    const sessionID = event?.sessionID || "";

    if (type === "stream.ready") return;
    if (type === "attention.changed") K.loadAttention?.().catch(() => {});
    if (sessionID && sessionID !== selectedID) {
      if (type === "session.changed" || type === "message.changed") {
        window.setTimeout(() => K.loadSessions().catch(() => {}), 100);
      }
      return;
    }
    if (type === "session.changed" || type === "message.changed" || type === "workspace.changed") {
      scheduleSelectedRefresh(30);
    }
  };

  K.startEvents = () => {
    if (K.state.eventSource) K.state.eventSource.close();
    K.state.eventSource = null;
    if (K.state.fallbackPolling) window.clearInterval(K.state.fallbackPolling);
    K.state.fallbackPolling = null;

    if (typeof globalThis.EventSource === "undefined") {
      K.state.fallbackPolling = window.setInterval(() => scheduleSelectedRefresh(0), 1200);
      return;
    }
    K.state.eventSource = K.api.events.subscribe({
      onEvent: K.handleLiveEvent,
      onError: () => {
        // Native EventSource reconnects automatically. Persisted semantic Session
        // reads and prompt polling remain reconnect-safe sources of truth.
      },
    });
  };

  const assistantAfter = (timestamp: any) => K.state.messages.some((message) => {
    if (message?.role !== "assistant") return false;
    const created = Number(message.createdAt || 0);
    return created >= timestamp - 1000 && (message.text || message.error || (message.activities || []).some((activity) => activity?.kind === "tool"));
  });

  K.startSessionPolling = (startedAt = Date.now()) => {
    K.stopSessionPolling();
    let sawRunning = K.state.session ? K.isSessionRunning(K.state.session.id) : false;
    K.state.sessionPolling = window.setInterval(async () => {
      if (!K.state.session) return K.stopSessionPolling();
      try {
        await Promise.all([K.loadMessages(), K.loadActiveSessions(), K.loadAttention?.()]);
        const running = K.isSessionRunning(K.state.session.id);
        if (running) sawRunning = true;
        K.renderMessages();

        const elapsed = Date.now() - startedAt;
        const settled = !running && !K.els.attentionDialog.open && (sawRunning || assistantAfter(startedAt) || elapsed > 4000);
        if (settled) {
          K.state.sending = false;
          K.stopSessionPolling();
          await K.loadSessions().catch(() => {});
          const fresh = K.state.sessions.find((item) => item.id === K.state.session?.id);
          if (fresh) K.state.session = fresh;
          K.renderSessionHeader();
          K.renderSessions();
          K.renderMessages();
        }
      } catch (err) {
        K.state.sending = false;
        K.stopSessionPolling();
        K.showError((err as any).message || String(err));
        K.renderMessages();
      }
    }, 700);
  };

  K.sendPrompt = async () => {
    const text = K.els.prompt.value.trim();
    if (!text || K.state.sending) return;
    K.showError("");
    K.state.sending = true;
    K.els.sendButton.disabled = true;
    const startedAt = Date.now();
    try {
      if (!K.state.session) await K.createSession();
      K.ensureSessionSelection();
      const agent = K.els.agentSelect.value || undefined;
      const model = K.selectedModel();

      K.els.prompt.value = "";
      K.resizePrompt();
      K.state.messages.push({
        role: "user",
        createdAt: startedAt,
        agent: agent || "",
        model: model ? { providerID: model.providerID, id: model.id } : undefined,
        text,
        activities: [],
        attachments: [],
        usage: { input: 0, output: 0, reasoning: 0, cacheRead: 0, cacheWrite: 0 },
        changes: [],
      });
      K.renderMessages();

      await K.api.sessions.promptAsync(K.state.session!.id, { text, agent, model, variant: model?.variant });
      await Promise.all([K.loadMessages(), K.loadActiveSessions(), K.loadAttention?.()]);
      K.renderMessages();
      K.startSessionPolling(startedAt);
    } catch (err) {
      K.state.sending = false;
      K.showError((err as any).message || String(err));
      K.renderMessages();
    } finally {
      K.els.sendButton.disabled = false;
      K.els.prompt.focus();
    }
  };

  K.pickProject = async () => {
    K.showError("");
    try {
      K.state.local = await K.request("/local/pick-directory", { method: "POST" });
      await K.afterProjectChange();
    } catch (err) {
      if (/no supported folder picker/i.test((err as any).message || "")) return K.openManualProject();
      K.showError((err as any).message || String(err));
    }
  };

  K.openManualProject = () => {
    K.els.pathInput.value = K.state.local?.project || "";
    K.els.pathDialog.showModal();
    window.setTimeout(() => { K.els.pathInput.focus(); K.els.pathInput.select(); }, 0);
  };

  K.setManualProject = async (event) => {
    event.preventDefault();
    if (event.submitter?.value === "cancel") return K.els.pathDialog.close();
    const path = K.els.pathInput.value.trim();
    if (!path) return;
    try {
      K.state.local = await K.request("/local/project", { method: "POST", body: JSON.stringify({ path }) });
      K.els.pathDialog.close();
      await K.afterProjectChange();
    } catch (err) { K.showError((err as any).message || String(err)); }
  };

  K.afterProjectChange = async () => {
    K.stopEvents();
    Object.assign(K.state, { session: null, sessions: [], messages: [], activeSessions: {}, sending: false });
    K.els.projectName.textContent = K.basename(K.state.local!.project);
    K.els.projectPath.textContent = K.state.local!.project;
    K.els.projectPath.title = K.state.local!.project;
    K.newSession();
    await Promise.all([K.loadCatalog(), K.loadSessions(), K.loadActiveSessions()]);
    K.startEvents();
  };

  K.switchAgent = () => {
    if (!K.state.session) return;
    K.state.session.agent = K.els.agentSelect.value || undefined;
    K.renderSessionHeader();
  };

  K.switchModel = () => {
    if (!K.state.session) return;
    K.state.session.model = K.selectedModel();
    K.renderSessionHeader();
  };

  K.resizePrompt = () => {
    K.els.prompt.style.height = "auto";
    K.els.prompt.style.height = `${Math.min(190, Math.max(56, K.els.prompt.scrollHeight))}px`;
  };
})();
