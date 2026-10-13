const $ = (id: string): HTMLElement => document.getElementById(id)!;

// Lifecycle contract: the presentation module owns renderMessages. Other
// modules subscribe to post-render effects without monkey-patching that method.
// Subscriptions run in registration order; a failed decoration cannot prevent
// the remaining independent decorations from updating the UI.
const messageRenderEffects: Array<() => void> = [];

const onMessagesRendered = (effect: () => void): (() => void) => {
  if (typeof effect !== "function") throw new TypeError("Message render effect must be a function");
  messageRenderEffects.push(effect);
  return () => {
    const index = messageRenderEffects.indexOf(effect);
    if (index !== -1) messageRenderEffects.splice(index, 1);
  };
};

const emitMessagesRendered = (): void => {
  for (const effect of [...messageRenderEffects]) {
    try {
      effect();
    } catch (error) {
      console.error("[TL Studio] message render effect failed", error);
    }
  }
};

export const K = {
  onMessagesRendered,
  emitMessagesRendered,
  els: {
    projectName: $("projectName"), projectPath: $("projectPath"), pickProject: $("pickProject"), manualProject: $("manualProject"),
    backendStatus: $("backendStatus"), sessions: $("sessions"), newSession: $("newSession"), emptyNewSession: $("emptyNewSession"),
    emptyPickProject: $("emptyPickProject"), emptyState: $("emptyState"), conversation: $("conversation"), sessionTitle: $("sessionTitle"),
    sessionMeta: $("sessionMeta"), agentSelect: $("agentSelect"), modelSelect: $("modelSelect"), refreshButton: $("refreshButton"),
    accountButton: $("accountButton"), prompt: $("prompt"), sendButton: $("sendButton"), errorBanner: $("errorBanner"), versionLabel: $("versionLabel"),
    pathDialog: $("pathDialog"), pathForm: $("pathForm"), pathInput: $("pathInput"),
    authDialog: $("authDialog"), authInstructions: $("authInstructions"), authCodeWrap: $("authCodeWrap"), authCode: $("authCode"),
    authCancel: $("authCancel"), authOpen: $("authOpen"), attentionDialog: $("attentionDialog"), attentionTitle: $("attentionTitle"),
    attentionBody: $("attentionBody"), attentionActions: $("attentionActions"),
  } as TLStudioElements,
  state: {
    local: null,
    sessions: [],
    session: null,
    messages: [],
    activeSessions: {},
    agents: [],
    models: [],
    providers: [],
    providerDefaults: {},
    connectedProviders: new Set(),
    providerAccounts: [],
    eventSource: null,
    fallbackPolling: null,
    sessionPolling: null,
    sending: false,
    revision: 0,
    authController: null,
    authURL: "",
    authProviderID: "",
    authLoginID: "",
    attentionKey: "",
    attachments: [],
    activeEditorPath: "",
    changes: [],
    editorTabs: [],
    filesEntries: [],
    filesLoading: false,
    filesPath: "",
    filesProject: "",
    selectedFileEntry: null,
    legacySession: false,
    preview: {},
    terminal: {},
    toolRegistry: null,
    plugins: [],
    changesLoading: false,
    sseSettling: false,
  } as TLStudioState,
} as TLStudioKernel;
