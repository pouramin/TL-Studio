import { K } from "./kernel";

(() => {
  "use strict";

  
  const enc = encodeURIComponent;
  const json = (value: any) => JSON.stringify(value);
  const body = (value: any) => ({ body: json(value) });
  const unwrapData = (payload: any) => payload && typeof payload === "object" && "data" in payload ? payload.data : payload;

  const projectDirectory = () => K.state?.local?.project || "";
  const withQuery = (path: string, params: Record<string, unknown> = {}) => {
    const query = new URLSearchParams();
    for (const [key, value] of Object.entries(params)) {
      if (value !== undefined && value !== null && value !== "") query.set(key, String(value));
    }
    return query.size ? `${path}?${query}` : path;
  };
  const route = (path: string, params: Record<string, unknown> = {}, directory = projectDirectory()) => withQuery(path, {
    ...(directory ? { directory } : {}),
    ...params,
  });

  const wireModel = (model: any) => model ? {
    providerID: model.providerID,
    modelID: model.modelID || model.id,
  } : undefined;

  const parseSSE = (handler: (payload: any) => void) => (event: MessageEvent<string>) => {
    if (!event?.data) return;
    try {
      const decoded = JSON.parse(event.data);
      handler(decoded?.payload || decoded);
    } catch (error) {
      console.warn("[TL Studio] Ignoring invalid SSE payload", error);
    }
  };

  const openLocalEventSource = (path: string, { onEvent, onOpen, onError }: { onEvent?: (event: TLStudioLiveEvent) => void; onOpen?: (event: Event) => void; onError?: (event: Event) => void } = {}) => {
    const source = new EventSource(path);
    if (onOpen) source.addEventListener("open", onOpen);
    if (onError) source.addEventListener("error", onError);
    if (onEvent) source.addEventListener("message", parseSSE(onEvent));
    return source;
  };

  K.api = Object.freeze({
    version: "native-product-api-v1",

    health: () => K.request("/local/health"),
    path: () => K.request("/local/path"),


    agents: async () => {
      const payload = await K.request("/local/agents");
      return Array.isArray(payload) ? payload : [];
    },

    providerState: async () => {
      const payload = unwrapData(await K.request(route("/local/providers/catalog"))) || {};
      return {
        all: Array.isArray(payload.all) ? payload.all : [],
        connected: new Set<string>(Array.isArray(payload.connected) ? payload.connected.map(String) : []),
        defaults: payload.default && typeof payload.default === "object" ? payload.default : {},
        failed: Array.isArray(payload.failed) ? payload.failed : [],
      };
    },

    providerAccounts: {
      list: async () => {
        const payload = await K.request(withQuery("/local/provider-accounts", { directory: projectDirectory() }));
        return Array.isArray(payload) ? payload : [];
      },
      status: (providerID: string) => K.request(withQuery(`/local/provider-accounts/${enc(providerID)}`, { directory: projectDirectory() })),
      setup: (providerID: string) => K.request(withQuery(`/local/provider-accounts/${enc(providerID)}/setup`, { directory: projectDirectory() })),
      configureSetup: (providerID: string, values: Record<string, string>) => K.request(withQuery(`/local/provider-accounts/${enc(providerID)}/setup`, { directory: projectDirectory() }), {
        method: "PUT",
        ...body({ values }),
      }),
      beginLogin: (providerID: string) => K.request(withQuery(`/local/provider-accounts/${enc(providerID)}/login`, { directory: projectDirectory() }), { method: "POST" }),
      pollLogin: (providerID: string, loginID: string, signal?: AbortSignal) => K.request(withQuery(`/local/provider-accounts/${enc(providerID)}/login/${enc(loginID)}`, { directory: projectDirectory() }), { signal }),
      cancelLogin: (providerID: string, loginID: string) => K.request(withQuery(`/local/provider-accounts/${enc(providerID)}/login/${enc(loginID)}`, { directory: projectDirectory() }), { method: "DELETE" }),
      refresh: (providerID: string) => K.request(withQuery(`/local/provider-accounts/${enc(providerID)}/refresh`, { directory: projectDirectory() }), { method: "POST" }),
      models: (providerID: string) => K.request(withQuery(`/local/provider-accounts/${enc(providerID)}/models`, { directory: projectDirectory() })),
      disconnect: (providerID: string) => K.request(withQuery(`/local/provider-accounts/${enc(providerID)}`, { directory: projectDirectory() }), { method: "DELETE" }),
    },

    providers: {
      config: async () => {
        const payload = unwrapData(await K.request("/local/providers/config")) || {};
        return { providers: Array.isArray(payload.providers) ? payload.providers : [] };
      },
      upsert: async (providerID: any, { provider, apiKey }: any = {}) => unwrapData(await K.request(`/local/providers/config/${enc(providerID)}`, {
        method: "PUT",
        ...body({ provider, ...(apiKey ? { apiKey } : {}) }),
      })),
      remove: async (providerID: any) => unwrapData(await K.request(`/local/providers/config/${enc(providerID)}`, { method: "DELETE" })),
      disconnectAPI: async (providerID: any) => unwrapData(await K.request(`/local/providers/config/${enc(providerID)}/api-connection`, { method: "DELETE" })),
      discover: async ({ providerID, protocol, baseURL, apiKey }: any = {}) => unwrapData(await K.request("/local/providers/discover", {
        method: "POST",
        ...body({
          ...(providerID ? { providerID } : {}),
          protocol,
          baseURL,
          ...(apiKey ? { apiKey } : {}),
        }),
      })),
    },

    jevRouter: {
      status: () => K.request("/local/jev-router"),
      configure: (enabled: boolean) => K.request("/local/jev-router", {
        method: "PUT",
        ...body({ enabled }),
      }),
    },

    decisionEngine: {
      status: () => K.request("/local/decision-engine"),
      configure: (engine: "off" | "jev") => K.request("/local/decision-engine", {
        method: "PUT",
        ...body({ engine }),
      }),
      evaluate: (input: TLStudioDynamicRecord) => K.request("/local/decision-engine/evaluate", {
        method: "POST",
        ...body(input),
      }),
    },

    tools: {
      registry: async () => {
        const payload = await K.request<any>("/local/tools");
        return payload && typeof payload === "object" ? payload : { version: 0, tools: [], unknown: null };
      },
    },

    plugins: {
      catalog: async () => {
        const payload = await K.request("/local/plugins/catalog");
        return Array.isArray(payload) ? payload : [];
      },
      installCatalog: (pluginID: string) => K.request(`/local/plugin-catalog/${enc(pluginID)}/install`, {
        method: "POST",
        ...body({ confirmed: true }),
      }),
      list: async () => {
        const payload = await K.request("/local/plugins");
        return Array.isArray(payload) ? payload : [];
      },
      saved: async () => {
        const payload = await K.request("/local/plugins/saved");
        return Array.isArray(payload) ? payload : [];
      },
      attach: (pluginID: string, sourceProject: string) => K.request(`/local/plugins/${enc(pluginID)}/attach`, {
        method: "POST",
        ...body({ sourceProject }),
      }),
      create: (plugin: any, environment?: Record<string, string>) => K.request("/local/plugins", {
        method: "POST",
        ...body({ plugin, ...(environment !== undefined ? { environment } : {}) }),
      }),
      update: (pluginID: string, plugin: any, environment?: Record<string, string>) => K.request(`/local/plugins/${enc(pluginID)}`, {
        method: "PUT",
        ...body({ plugin, ...(environment !== undefined ? { environment } : {}) }),
      }),
      remove: (pluginID: string) => K.request(`/local/plugins/${enc(pluginID)}`, { method: "DELETE" }),
      setEnabled: (pluginID: string, enabled: boolean) => K.request(`/local/plugins/${enc(pluginID)}/enabled`, {
        method: "POST",
        ...body({ enabled, ...(enabled ? { confirmed: true } : {}) }),
      }),
      testConfig: (plugin: any, environment?: Record<string, string>) => K.request("/local/plugins/test", {
        method: "POST",
        ...body({ plugin, ...(environment !== undefined ? { environment } : {}) }),
      }),
      test: (pluginID: string) => K.request(`/local/plugins/${enc(pluginID)}/test`, { method: "POST" }),
      action: (pluginID: string, actionID: string, confirmed = false) => K.request(`/local/plugins/${enc(pluginID)}/actions/${enc(actionID)}`, {
        method: "POST",
        ...body({ confirmed }),
      }),
    },

    sessionView: {
      list: ({ limit = 150 } = {}) => K.request(withQuery("/local/sessions", { limit })),
      status: ({ directory = projectDirectory() } = {}) => K.request(withQuery("/local/sessions/status", { directory })),
      get: (sessionID: any, { directory = projectDirectory() } = {}) => K.request(withQuery(`/local/sessions/${enc(sessionID)}`, { directory })),
      messages: (sessionID: any, { limit = 200, directory = projectDirectory() } = {}) => K.request(withQuery(`/local/sessions/${enc(sessionID)}/messages`, { limit, directory })),
      changes: (sessionID: any, { directory = projectDirectory() } = {}) => K.request(withQuery(`/local/sessions/${enc(sessionID)}/changes`, { directory })),
    },

    sessionCommands: {
      create: (input: TLStudioSessionCreateInput = {}, { directory = projectDirectory() } = {}) => K.request(
        withQuery("/local/sessions", { directory }),
        { method: "POST", ...body(input) },
      ),
      update: (sessionID: string, input: TLStudioSessionUpdateInput, { directory = projectDirectory() } = {}) => K.request(
        withQuery(`/local/sessions/${enc(sessionID)}`, { directory }),
        { method: "PATCH", ...body(input) },
      ),
      remove: (sessionID: string, { directory = projectDirectory() } = {}) => K.request(
        withQuery(`/local/sessions/${enc(sessionID)}`, { directory }),
        { method: "DELETE" },
      ),
      run: (sessionID: string, input: TLStudioSessionRunInput = {}, { directory = projectDirectory() } = {}) => K.request(
        withQuery(`/local/sessions/${enc(sessionID)}/runs`, { directory }),
        { method: "POST", ...body(input) },
      ),
      abort: (sessionID: string, { scope, directory = projectDirectory() }: { scope?: string; directory?: string } = {}) => K.request(
        withQuery(`/local/sessions/${enc(sessionID)}/abort`, { directory }),
        { method: "POST", ...body({ ...(scope ? { scope } : {}) }) },
      ),
    },

    permissions: {
      list: async (sessionID: any) => {
        const query = new URLSearchParams();
        if (sessionID) query.set("sessionID", sessionID);
        const payload = await K.request(`/local/permissions${query.size ? `?${query}` : ""}`);
        return Array.isArray(payload) ? payload : [];
      },
      reply: (sessionID: any, requestID: any, reply: any, message: any) => K.request(`/local/permissions/${enc(requestID)}/reply`, {
        method: "POST",
        ...body({ sessionID, reply, ...(message ? { message } : {}) }),
      }),
      rules: async () => {
        const payload = await K.request("/local/permissions/rules");
        return Array.isArray(payload) ? payload : [];
      },
      removeRule: (ruleID: any) => K.request(`/local/permissions/rules/${enc(ruleID)}`, { method: "DELETE" }),
    },

    questions: {
      list: async (sessionID?: string) => {
        const payload = await K.request(withQuery("/local/questions", { sessionID }));
        return Array.isArray(payload) ? payload : [];
      },
      reply: (sessionID: string, requestID: string, answers: string[][]) => K.request(`/local/questions/${enc(requestID)}/reply`, {
        method: "POST",
        ...body({ sessionID, answers }),
      }),
      reject: (sessionID: string, requestID: string) => K.request(`/local/questions/${enc(requestID)}/reject`, {
        method: "POST",
        ...body({ sessionID }),
      }),
    },

    events: {
      subscribe: (options: { onEvent?: (event: TLStudioLiveEvent) => void; onOpen?: (event: Event) => void; onError?: (event: Event) => void } = {}) => openLocalEventSource("/local/events", options),
    },
  });
})();
