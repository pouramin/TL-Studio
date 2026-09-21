import { K } from "./kernel";

(() => {
  "use strict";

  
  const enc = encodeURIComponent;
  const json = (value: any) => JSON.stringify(value);
  const body = (value: any) => ({ body: json(value) });
  const unwrapData = (payload: any) => payload && typeof payload === "object" && "data" in payload ? payload.data : payload;
  const request = (path: string, options: RequestInit = {}) => K.request(`/runtime${path}`, options);
  const wrapData = (data: any) => ({ data });
  const hostedMeta = { providerID: "", preferredModels: [] };
  const applyHostedMeta = (value: any) => {
    if (value?.providerID) hostedMeta.providerID = String(value.providerID);
    if (Array.isArray(value?.preferredModels)) hostedMeta.preferredModels = value.preferredModels.map(String);
  };

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

  const legacyPageQuery = ({ order, limit, cursor }: { order?: string; limit?: number; cursor?: string } = {}) => {
    const query = new URLSearchParams();
    if (cursor) query.set("cursor", cursor);
    else if (order) query.set("order", order);
    if (limit !== undefined) query.set("limit", String(limit));
    return query;
  };

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
    version: "bundled-runtime-adapter-v1",

    health: () => request("/global/health"),
    path: () => request(route("/path")),

    runtime: {
      dispose: async () => unwrapData(await request("/global/dispose", { method: "POST" })),
    },

    agents: async () => {
      const payload = unwrapData(await request(route("/agent")));
      return Array.isArray(payload) ? payload : [];
    },

    providerState: async () => {
      const payload = unwrapData(await request(route("/providers/catalog"))) || {};
      applyHostedMeta(payload.hosted);
      return {
        all: Array.isArray(payload.all) ? payload.all : [],
        connected: new Set<string>(Array.isArray(payload.connected) ? payload.connected.map(String) : []),
        defaults: payload.default && typeof payload.default === "object" ? payload.default : {},
        failed: Array.isArray(payload.failed) ? payload.failed : [],
      };
    },

    providers: {
      config: async () => {
        const payload = unwrapData(await request("/providers/config")) || {};
        return { providers: Array.isArray(payload.providers) ? payload.providers : [] };
      },
      upsert: async (providerID: any, { provider, apiKey }: any = {}) => unwrapData(await request(`/providers/config/${enc(providerID)}`, {
        method: "PUT",
        ...body({ provider, ...(apiKey ? { apiKey } : {}) }),
      })),
      remove: async (providerID: any) => unwrapData(await request(`/providers/config/${enc(providerID)}`, { method: "DELETE" })),
    },

    tools: {
      registry: async () => {
        const payload = await K.request<any>("/local/tools");
        return payload && typeof payload === "object" ? payload : { version: 0, tools: [], unknown: null };
      },
    },

    sessionView: {
      list: ({ limit = 150 } = {}) => K.request(withQuery("/local/sessions", { limit })),
      status: ({ directory = projectDirectory() } = {}) => K.request(withQuery("/local/sessions/status", { directory })),
      get: (sessionID: any, { directory = projectDirectory() } = {}) => K.request(withQuery(`/local/sessions/${enc(sessionID)}`, { directory })),
      messages: (sessionID: any, { limit = 200, directory = projectDirectory() } = {}) => K.request(withQuery(`/local/sessions/${enc(sessionID)}/messages`, { limit, directory })),
      changes: (sessionID: any, { directory = projectDirectory() } = {}) => K.request(withQuery(`/local/sessions/${enc(sessionID)}/changes`, { directory })),
    },

    sessions: {
      create: async (input: any = {}) => {
        const payload: any = {};
        if (input.parentID) payload.parentID = input.parentID;
        if (input.title) payload.title = input.title;
        return wrapData(unwrapData(await request(route("/session"), { method: "POST", ...body(payload) })));
      },
      update: async (sessionID: any, input: any = {}, { directory }: any = {}) => wrapData(unwrapData(await request(route(`/session/${enc(sessionID)}`, {}, directory), {
        method: "PATCH", ...body(input),
      }))),
      remove: async (sessionID: any, { directory }: any = {}) => wrapData(unwrapData(await request(route(`/session/${enc(sessionID)}`, {}, directory), { method: "DELETE" }))),
      promptAsync: (sessionID: any, { text, parts, agent, model, variant, messageID, directory }: any = {}) => {
        const payload = {
          parts: Array.isArray(parts) && parts.length ? parts : [{ type: "text", text: text || "" }],
          ...(messageID ? { messageID } : {}),
          ...(agent ? { agent } : {}),
          ...(model ? { model: wireModel(model) } : {}),
          ...(variant ? { variant } : {}),
        };
        return request(route(`/session/${enc(sessionID)}/prompt_async`, {}, directory), { method: "POST", ...body(payload) });
      },
      abort: (sessionID: any, { scope, directory }: any = {}) => request(route(`/session/${enc(sessionID)}/abort`, { scope }, directory), { method: "POST" }),
    },

    // Read-only compatibility bridge for sessions created during TL Studio's
    // short Protocol v2 alpha window. New sessions and all normal coding stay
    // on the production Session API above.
    legacySessions: {
      list: ({ order = "desc", limit = 100, cursor }: { order?: string; limit?: number; cursor?: string } = {}) => {
        const query = legacyPageQuery({ order, limit, cursor });
        return request(`/api/session${query.size ? `?${query}` : ""}`);
      },
      messages: (sessionID: any, { order = "asc", limit = 500, cursor }: { order?: string; limit?: number; cursor?: string } = {}) => {
        const query = legacyPageQuery({ order, limit, cursor });
        return request(`/api/session/${enc(sessionID)}/message${query.size ? `?${query}` : ""}`);
      },
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
      list: async (sessionID: any) => {
        const payload = unwrapData(await request(route("/question")));
        return (Array.isArray(payload) ? payload : []).filter((item) => !sessionID || item?.sessionID === sessionID);
      },
      reply: (sessionID: any, requestID: any, answers: any) => request(route(`/question/${enc(requestID)}/reply`), {
        method: "POST", ...body({ answers }),
      }),
      reject: (sessionID: any, requestID: any) => request(route(`/question/${enc(requestID)}/reject`), { method: "POST" }),
    },

    hosted: {
      get providerID() { return hostedMeta.providerID; },
      get preferredModels() { return [...hostedMeta.preferredModels]; },
      status: async () => {
        const payload = unwrapData(await request(route("/hosted/status"))) || {};
        applyHostedMeta(payload);
        return {
          authenticated: payload.authenticated === true,
          type: payload.type || "",
          organizationId: payload.organizationId || "",
        };
      },
      authorize: async () => unwrapData(await request(route("/hosted/authorize"), { method: "POST" })),
      callback: async (signal: any) => unwrapData(await request(route("/hosted/callback"), { method: "POST", signal })),
      disconnect: async () => unwrapData(await request("/hosted", { method: "DELETE" })),
    },

    events: {
      subscribe: (options: { onEvent?: (event: TLStudioLiveEvent) => void; onOpen?: (event: Event) => void; onError?: (event: Event) => void } = {}) => openLocalEventSource("/local/events", options),
    },
  });
})();
