import { K } from "./kernel";

(() => {
  "use strict";

  K.basename = (path?: string) => {
    if (!path) return "No project";
    const bits = path.replace(/[\\/]+$/, "").split(/[\\/]/);
    return bits[bits.length - 1] || path;
  };

  K.formatTime = (input?: string | number | Date) => {
    if (!input) return "";
    const date = new Date(input);
    return Number.isNaN(date.getTime()) ? "" : new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" }).format(date);
  };

  K.relativeTime = (input?: string | number | Date) => {
    if (!input) return "";
    const ts = new Date(input).getTime();
    if (!Number.isFinite(ts)) return "";
    const diff = Date.now() - ts;
    if (diff < 60_000) return "now";
    if (diff < 3_600_000) return `${Math.max(1, Math.floor(diff / 60_000))}m`;
    if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h`;
    return `${Math.floor(diff / 86_400_000)}d`;
  };

  K.showError = (message?: string) => {
    const e = K.els.errorBanner;
    e.textContent = message || "";
    e.classList.toggle("hidden", !message);
  };

  // Origin-scoped tab storage survives refresh without leaking a capability to
  // untrusted preview ports. Never attach it to cross-origin requests.
  const authStorageKey = "tl-studio-local-capability";
  let capability = "";
  let localAuthReady: Promise<void> | null = null;
  K.ensureLocalAuth = (): Promise<void> => {
    if (!localAuthReady) {
      localAuthReady = (async () => {
        const fragment = new URLSearchParams(window.location.hash.replace(/^#/, ""));
        let token = fragment.get("tl_auth") || "";
        if (token) window.history.replaceState(null, "", window.location.pathname + window.location.search);
        if (!token) { try { token = window.sessionStorage?.getItem(authStorageKey) || ""; } catch {} }
        if (!token) token = window.prompt("Enter the Auth code shown by the TL Studio launcher:") || "";
        if (!token) throw new Error("Local API authorization required; use the launcher Auth code.");
        const response = await fetch("/local/auth/session", {
          method: "POST", credentials: "omit", cache: "no-store",
          headers: { "X-TL-Studio-Launch-Token": token, "X-TL-Studio-Request": "1" },
        });
        if (!response.ok) {
          try { window.sessionStorage?.removeItem(authStorageKey); } catch {}
          throw new Error("Local authorization failed. Reopen TL Studio from its launcher.");
        }
        capability = token;
        try { window.sessionStorage?.setItem(authStorageKey, token); } catch {}
      })().catch(error => { localAuthReady = null; throw error; });
    }
    return localAuthReady;
  };

  K.localFetch = async (path: string, options: RequestInit = {}): Promise<Response> => {
    const url = new URL(path, window.location.href);
    if (url.origin !== window.location.origin || !url.pathname.startsWith("/local/")) {
      throw new Error("Local capabilities cannot be sent outside the control API.");
    }
    await K.ensureLocalAuth();
    const headers = new Headers(options.headers);
    headers.set("Authorization", "Bearer " + capability);
    headers.set("X-TL-Studio-Request", "1");
    const response = await fetch(path, { cache: "no-store", ...options, credentials: "omit", headers });
    if (response.status === 401) {
      capability = ""; localAuthReady = null;
      try { window.sessionStorage?.removeItem(authStorageKey); } catch {}
    }
    if (response.ok && ["/local/file", "/local/entry"].includes(url.pathname)
        && ["PUT", "POST", "PATCH", "DELETE"].includes(String(options.method || "GET").toUpperCase())) {
      window.dispatchEvent(new Event("tl-studio:project-file-changed"));
    }
    return response;
  };

  K.request = async <T = any>(path: string, options: RequestInit = {}): Promise<T> => {
    // Do not dispatch any protected local API request before browser login.
    await K.ensureLocalAuth();
    const response = await K.localFetch(path, {
      cache: "no-store",
      ...options,
      headers: { "X-TL-Studio-Request": "1", ...(options.body ? { "Content-Type": "application/json" } : {}), ...(options.headers || {}) },
    });
    const type = response.headers.get("content-type") || "";
    const payload = type.includes("application/json") ? await response.json().catch(() => null) : await response.text().catch(() => "");
    if (!response.ok) {
      const detail = payload && typeof payload === "object"
        ? payload.error || payload.message || payload.data?.message || payload.data?.data?.message || JSON.stringify(payload)
        : String(payload || `${response.status} ${response.statusText}`);
      const ref = payload && typeof payload === "object" ? payload.ref || payload.data?.ref || payload.data?.data?.ref : "";
      throw new Error(ref && !String(detail).includes(ref) ? `${detail} [${ref}]` : detail);
    }
    return payload as T;
  };

  K.normalizeAgents = (input: any) => {
    if (!Array.isArray(input)) return [];
    return input.flatMap((agent) => {
      if (!agent || agent.hidden || agent.mode === "subagent") return [];
      const id = agent.id || agent.name;
      if (!id) return [];
      const label = agent.displayName || agent.name || agent.id || id;
      return [{ ...agent, id: String(id), label: String(label) }];
    });
  };

  K.extractModels = (providers: any) => {
    const result = [];
    for (const provider of Array.isArray(providers) ? providers : []) {
      if (!provider?.id) continue;
      const models = provider.models || {};
      const entries = Array.isArray(models)
        ? models.map((model) => [model?.id || model?.modelID || model?.name, model])
        : Object.entries(models);
      for (const [id, model] of entries) {
        if (!id || model?.enabled === false) continue;
        result.push({
          providerID: provider.id,
          id: String(id),
          name: model?.name || String(id),
          providerName: provider.name || provider.id,
          ...(model?.kind ? { kind: String(model.kind) } : {}),
          ...(model?.variant ? { variant: model.variant } : {}),
        });
      }
    }
    return result.sort((a, b) => `${a.providerName} ${a.name}`.localeCompare(`${b.providerName} ${b.name}`));
  };

  K.loadLocalStatus = async () => {
    const scope = K.viewScope.captureProject();
    const { els, state } = K;
    const local = await K.request<TLStudioLocalStatus>("/local/status");
    if (!K.viewScope.isProjectCurrent(scope)) return;
    state.local = local;
    els.projectName.textContent = K.basename(local.project);
    els.projectPath.textContent = local.project || "Choose a folder";
    els.projectPath.title = local.project || "";
    els.versionLabel.textContent = `v${local.version} · ${local.platform}/${local.arch}`;
  };

  K.checkBackend = async () => {
    try {
      const health = await K.api.health();
      if (health?.healthy !== true) throw new Error("Local health check did not report healthy");
      K.els.backendStatus.className = "status-dot ok";
      K.els.backendStatus.innerHTML = "<i></i> Local";
      return true;
    } catch (err) {
      K.els.backendStatus.className = "status-dot error";
      K.els.backendStatus.innerHTML = "<i></i> Offline";
      K.showError(`Local service: ${err instanceof Error ? err.message : String(err)}`);
      return false;
    }
  };

  K.modelValue = (model?: TLStudioModelRef | TLStudioSessionModelRef) => model ? `${model.providerID}::${model.id}${model.variant ? `::${model.variant}` : ""}` : "";

  K.renderAccount = () => {
    const connected = K.state.providerAccounts.filter((account) => account.connected).length;
    K.els.accountButton.textContent = "Account";
    K.els.accountButton.classList.toggle("signed-in", connected > 0);
    K.els.accountButton.title = connected
      ? `${connected} provider account${connected === 1 ? "" : "s"} connected — open Providers`
      : "Open provider account settings";
  };

  K.renderAgents = () => {
    const select = K.els.agentSelect;
    const current = select.value;
    select.innerHTML = '<option value="">Default</option>';
    for (const agent of K.state.agents) {
      const option = document.createElement("option");
      option.value = agent.id;
      option.textContent = agent.label || agent.id;
      option.title = agent.description || agent.label || agent.id;
      select.appendChild(option);
    }
    if ([...select.options].some((option) => option.value === current)) select.value = current;
  };

  K.renderModels = () => {
    const select = K.els.modelSelect;
    const current = select.value;
    select.innerHTML = '<option value="">Backend default</option>';

    const routers = K.state.models.filter((model) => model.kind === "router");
    const regular = K.state.models.filter((model) => model.kind !== "router");

    // Routers are product-level choices rather than one fixed underlying LLM,
    // so keep them at the top of the selector instead of nesting them under a
    // provider group such as OpenRouter.
    for (const model of routers) {
      const option = document.createElement("option");
      option.value = K.modelValue(model);
      option.textContent = `${model.name || model.id} · Router`;
      option.title = `${model.providerName || model.providerID} · Router · ${model.id}`;
      select.appendChild(option);
    }

    let group: HTMLOptGroupElement | null = null;
    let last = "";
    for (const model of regular) {
      if (model.providerID !== last) {
        group = document.createElement("optgroup");
        group.label = model.providerName || model.providerID;
        select.appendChild(group);
        last = model.providerID;
      }
      const option = document.createElement("option");
      option.value = K.modelValue(model);
      option.textContent = model.name || model.id;
      option.title = `${model.providerID}/${model.id}`;
      group!.appendChild(option);
    }
    const values = [...select.querySelectorAll("option")].map((option) => option.value);
    if (current && values.includes(current)) {
      select.value = current;
      return;
    }
  };

  K.loadCatalog = async () => {
    const scope = K.viewScope.captureProject();
    const [agents, providers] = await Promise.allSettled([K.api.agents(), K.api.providerState()]);
    if (!K.viewScope.isProjectCurrent(scope)) return;
    if (agents.status === "fulfilled") {
      K.state.agents = K.normalizeAgents(agents.value);
      K.renderAgents();
    }
    if (providers.status === "fulfilled") {
      K.state.providers = providers.value.all;
      K.state.models = K.extractModels(providers.value.all);
      K.state.connectedProviders = providers.value.connected;
      K.state.providerDefaults = providers.value.defaults;
      K.renderModels();
    } else {
      K.state.providers = [];
      K.state.models = [];
      K.state.connectedProviders = new Set();
      K.state.providerDefaults = {};
      K.renderModels();
    }
    K.renderAccount();
  };

  K.selectedModel = () => {
    const value = K.els.modelSelect.value;
    if (!value) return undefined;
    const [providerID, id, variant] = value.split("::");
    if (!providerID || !id) return undefined;
    return { providerID, id, ...(variant ? { variant } : {}) };
  };

  K.loadSessions = async () => {
    const scope = K.viewScope.captureProject();
    const payload = await K.api.sessionView.list({ limit: 150 });
    if (!K.viewScope.isProjectCurrent(scope)) return K.state.sessions;
    K.state.sessions = Array.isArray(payload) ? payload : [];
    K.renderSessions();
    return K.state.sessions;
  };

  K.loadActiveSessions = async () => {
    const scope = K.viewScope.captureProject();
    try {
      const active = await K.api.sessionView.status();
      if (K.viewScope.isProjectCurrent(scope)) K.state.activeSessions = active;
    } catch {
      if (K.viewScope.isProjectCurrent(scope)) K.state.activeSessions = {};
    }
  };

  K.isSessionRunning = (sessionID?: string) => {
    const status = sessionID ? K.state.activeSessions?.[sessionID] : undefined;
    return status?.active === true;
  };

  K.renderSessions = () => {
    const list = K.els.sessions;
    list.textContent = "";
    if (!K.state.sessions.length) {
      const empty = document.createElement("div");
      empty.className = "sidebar-empty";
      empty.textContent = "No sessions in this project yet.";
      list.appendChild(empty);
      return;
    }
    for (const session of K.state.sessions) {
      const button = document.createElement("button");
      button.className = `session-item${K.state.session?.id === session.id ? " active" : ""}`;
      const title = document.createElement("strong");
      const meta = document.createElement("span");
      title.textContent = session.title || "Untitled session";
      meta.textContent = `${session.agent || "default"} · ${K.relativeTime(session.updatedAt || session.createdAt)}`;
      button.append(title, meta);
      button.addEventListener("click", () => K.selectSession(session));
      list.appendChild(button);
    }
  };

  K.renderSessionHeader = () => {
    const session = K.state.session;
    if (!session) {
      K.els.sessionTitle.textContent = "New session";
      K.els.sessionMeta.textContent = "Local agent workspace";
      return;
    }
    K.els.sessionTitle.textContent = session.title || "Untitled session";
    const model = session.model ? `${session.model.providerID}/${session.model.id || session.model.modelID}` : "default model";
    K.els.sessionMeta.textContent = `${session.agent || "default agent"} · ${model}`;
  };

  K.syncSelectors = () => {
    const session = K.state.session;
    if (!session) return;
    K.els.agentSelect.value = session.agent && [...K.els.agentSelect.options].some((option) => option.value === session.agent)
      ? session.agent
      : "";
    if (session.model) {
      const value = K.modelValue({ ...session.model, id: session.model.id || session.model.modelID });
      K.els.modelSelect.value = [...K.els.modelSelect.querySelectorAll("option")].some((option) => option.value === value) ? value : "";
    }
    if (typeof Event !== "undefined" && typeof window.dispatchEvent === "function") {
      window.dispatchEvent(new Event("tl-studio:agent-run-selection"));
    }
  };
})();
