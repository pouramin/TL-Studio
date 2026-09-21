type TLStudioDynamicRecord = Record<string, any>;
type TLStudioAsyncResult<T = any> = Promise<T>;
type TLStudioTimer = number | null;

interface TLStudioLocalStatus {
  version: string;
  project: string;
  platform: string;
  arch: string;
  frontendURL?: string;
  [key: string]: unknown;
}

interface TLStudioModelRef {
  providerID: string;
  id: string;
  modelID?: string;
  variant?: string;
}

interface TLStudioModelOption extends TLStudioModelRef {
  name: string;
  providerName: string;
}

interface TLStudioAgentOption {
  id: string;
  label: string;
  name?: string;
  displayName?: string;
  description?: string;
  hidden?: boolean;
  mode?: string;
  [key: string]: unknown;
}

interface TLStudioSessionModelRef {
  providerID?: string;
  id?: string;
  modelID?: string;
  variant?: string;
}

interface TLStudioSessionView {
  id: string;
  title: string;
  directory: string;
  parentID?: string;
  agent?: string;
  model?: TLStudioSessionModelRef;
  createdAt?: number;
  updatedAt?: number;
  [key: string]: unknown;
}

interface TLStudioSessionStatus {
  state: "idle" | "running" | "retrying" | "unknown";
  active: boolean;
  attempt?: number;
  nextAt?: number;
  message?: string;
}

interface TLStudioSessionUsage {
  input: number;
  output: number;
  reasoning: number;
  cacheRead: number;
  cacheWrite: number;
}

interface TLStudioSessionActivity {
  kind: "reasoning" | "tool" | "subtask" | "model";
  status?: string;
  text?: string;
  agent?: string;
  title?: string;
  toolID?: string;
  runtimeToolID?: string;
  toolName?: string;
  category?: string;
  permissionClass?: string;
  model?: TLStudioSessionModelRef;
  usage?: TLStudioSessionUsage;
  startAt?: number;
  endAt?: number;
  elapsed?: number;
  [key: string]: any;
}

interface TLStudioSessionMessage {
  id?: string;
  sessionID?: string;
  role: string;
  agent?: string;
  model?: TLStudioSessionModelRef;
  createdAt?: number;
  completedAt?: number;
  text?: string;
  error?: any;
  activities: TLStudioSessionActivity[];
  attachments: Array<{ name: string; mime?: string; url?: string }>;
  usage: TLStudioSessionUsage;
  changes: Array<{ file: string; additions: number; deletions: number; patch?: string }>;
  [key: string]: any;
}

interface TLStudioToolCapabilities {
  read: boolean;
  write: boolean;
  execute: boolean;
  network: boolean;
}

interface TLStudioToolDescriptor {
  id: string;
  runtimeIDs: string[];
  name: string;
  description: string;
  category: string;
  permissionClass: string;
  capabilities: TLStudioToolCapabilities;
  presentation: string;
}

interface TLStudioToolRegistry {
  version: number;
  tools: TLStudioToolDescriptor[];
  unknown: TLStudioToolDescriptor;
}

interface TLStudioPermissionRule {
  id: string;
  project: string;
  permission: string;
  matcher: string;
  decision: "allow";
  createdAt: string;
}

type TLStudioLiveEventType =
  | "stream.ready"
  | "session.changed"
  | "message.changed"
  | "attention.changed"
  | "workspace.changed";

interface TLStudioLiveEvent {
  version: 1;
  type: TLStudioLiveEventType;
  action?: "created" | "removed" | "requested" | "resolved" | "content" | "state" | "changed";
  sessionID?: string;
  messageID?: string;
  attentionKind?: "permission" | "question";
  path?: string;
}

interface TLStudioProviderState {
  all: TLStudioDynamicRecord[];
  connected: Set<string>;
  defaults: Record<string, string>;
  failed: TLStudioDynamicRecord[];
}

interface TLStudioRuntimeContract {
  readonly version: string;
  health(): Promise<any>;
  path(): Promise<any>;
  runtime: { dispose(): Promise<any> };
  agents(): Promise<any[]>;
  providerState(): Promise<TLStudioProviderState>;
  providers: {
    config(): Promise<{ providers: TLStudioDynamicRecord[] }>;
    upsert(providerID: string, input?: TLStudioDynamicRecord): Promise<any>;
    remove(providerID: string): Promise<any>;
  };
  tools: { registry(): Promise<TLStudioToolRegistry> };
  sessionView: {
    list(options?: { limit?: number }): Promise<TLStudioSessionView[]>;
    status(options?: { directory?: string }): Promise<Record<string, TLStudioSessionStatus>>;
    get(sessionID: string, options?: { directory?: string }): Promise<TLStudioSessionView>;
    messages(sessionID: string, options?: { limit?: number; directory?: string }): Promise<TLStudioSessionMessage[]>;
    changes(sessionID: string, options?: { directory?: string }): Promise<Array<{ file: string; additions: number; deletions: number; patch?: string }>>;
  };
  sessions: {
    create(input?: TLStudioDynamicRecord): Promise<any>;
    update(sessionID: string, input?: TLStudioDynamicRecord, options?: { directory?: string }): Promise<any>;
    remove(sessionID: string, options?: { directory?: string }): Promise<any>;
    promptAsync(sessionID: string, input?: TLStudioDynamicRecord): Promise<any>;
    abort(sessionID: string, options?: { scope?: string; directory?: string }): Promise<any>;
  };
  legacySessions: {
    list(options?: TLStudioDynamicRecord): Promise<any>;
    messages(sessionID: string, options?: TLStudioDynamicRecord): Promise<any>;
  };
  permissions: {
    list(sessionID?: string): Promise<any[]>;
    reply(sessionID: string, requestID: string, reply: "once" | "always" | "reject", message?: string): Promise<any>;
    rules(): Promise<TLStudioPermissionRule[]>;
    removeRule(ruleID: string): Promise<any>;
  };
  questions: {
    list(sessionID?: string): Promise<any[]>;
    reply(sessionID: string, requestID: string, answers: string[][]): Promise<any>;
    reject(sessionID: string, requestID: string): Promise<any>;
  };
  hosted: {
    readonly providerID: string;
    readonly preferredModels: readonly string[];
    status(): Promise<{ authenticated: boolean; type: string; organizationId: string }>;
    authorize(): Promise<any>;
    callback(signal?: AbortSignal): Promise<any>;
    disconnect(): Promise<any>;
  };
  events: {
    subscribe(options?: {
      onEvent?: (event: TLStudioLiveEvent) => void;
      onOpen?: (event: Event) => void;
      onError?: (event: Event) => void;
    }): EventSource;
  };
}

interface TLStudioElements {
  projectName: HTMLElement;
  projectPath: HTMLElement;
  pickProject: HTMLButtonElement;
  manualProject: HTMLButtonElement;
  backendStatus: HTMLElement;
  sessions: HTMLElement;
  newSession: HTMLButtonElement;
  emptyNewSession: HTMLButtonElement;
  emptyPickProject: HTMLButtonElement;
  emptyState: HTMLElement;
  conversation: HTMLElement;
  sessionTitle: HTMLElement;
  sessionMeta: HTMLElement;
  agentSelect: HTMLSelectElement;
  modelSelect: HTMLSelectElement;
  refreshButton: HTMLButtonElement;
  accountButton: HTMLButtonElement;
  prompt: HTMLTextAreaElement;
  sendButton: HTMLButtonElement;
  errorBanner: HTMLElement;
  versionLabel: HTMLElement;
  pathDialog: HTMLDialogElement;
  pathForm: HTMLFormElement;
  pathInput: HTMLInputElement;
  authDialog: HTMLDialogElement;
  authInstructions: HTMLElement;
  authCodeWrap: HTMLElement;
  authCode: HTMLElement;
  authCancel: HTMLButtonElement;
  authOpen: HTMLButtonElement;
  attentionDialog: HTMLDialogElement;
  attentionTitle: HTMLElement;
  attentionBody: HTMLElement;
  attentionActions: HTMLElement;
  attachButton: HTMLButtonElement;
  attachmentInput: HTMLInputElement;
  composerAttachments: HTMLElement;
}

interface TLStudioFileTab extends TLStudioDynamicRecord {
  path: string;
  content: string;
  savedContent: string;
  sha256: string;
  modified: string;
  size: number;
  mime: string;
  viewOnly?: boolean;
  externalChanged?: boolean;
  previewKind?: string;
  previewName?: string;
  previewCapabilityID?: string;
}

interface TLStudioState {
  local: TLStudioLocalStatus | null;
  sessions: TLStudioSessionView[];
  session: TLStudioSessionView | null;
  messages: TLStudioSessionMessage[];
  activeSessions: Record<string, TLStudioSessionStatus>;
  agents: TLStudioAgentOption[];
  models: TLStudioModelOption[];
  providers: TLStudioDynamicRecord[];
  providerDefaults: Record<string, string>;
  connectedProviders: Set<string>;
  eventSource: EventSource | null;
  fallbackPolling: TLStudioTimer;
  sessionPolling: TLStudioTimer;
  sending: boolean;
  revision: number;
  authController: AbortController | null;
  authURL: string;
  attentionKey: string;
  attachments: TLStudioDynamicRecord[];
  hostedAuth: TLStudioDynamicRecord | null;
  activeEditorPath: string;
  changes: TLStudioDynamicRecord[];
  editorTabs: TLStudioFileTab[];
  filesEntries: TLStudioDynamicRecord[];
  filesLoading: boolean;
  filesPath: string;
  filesProject: string;
  selectedFileEntry: TLStudioDynamicRecord | null;
  legacySession: boolean;
  preview: TLStudioDynamicRecord;
  terminal: TLStudioDynamicRecord;
  toolRegistry: TLStudioToolRegistry | null;
  changesLoading: boolean;
  sseSettling: boolean;
}

type TLStudioCallable = (...args: any[]) => any;

interface TLStudioKernel {
  els: TLStudioElements;
  state: TLStudioState;
  api: TLStudioRuntimeContract;

  request<T = any>(path: string, options?: RequestInit): Promise<T>;
  basename(path?: string): string;
  formatTime(input?: string | number | Date): string;
  relativeTime(input?: string | number | Date): string;
  showError(message?: string): void;
  normalizeAgents(input: any): TLStudioAgentOption[];
  extractModels(providers: any): TLStudioModelOption[];
  loadLocalStatus(): Promise<void>;
  checkBackend(): Promise<boolean>;
  modelValue(model?: TLStudioModelRef | TLStudioSessionModelRef): string;
  preferredHostedModel(): { providerID: string; id: string } | undefined;
  renderAccount(): void;
  renderAgents(): void;
  renderModels(): void;
  loadCatalog(): Promise<void>;
  selectedModel(): TLStudioModelRef | undefined;
  loadSessions(): Promise<TLStudioSessionView[]>;
  loadActiveSessions(): Promise<void>;
  isSessionRunning(sessionID?: string): boolean;
  renderSessions(): void;
  renderSessionHeader(): void;
  syncSelectors(): void;

  refreshAll: TLStudioCallable;
  pickProject: TLStudioCallable;
  openManualProject: TLStudioCallable;
  setManualProject: TLStudioCallable;
  afterProjectChange: TLStudioCallable;
  newSession: TLStudioCallable;
  createSession: TLStudioCallable;
  selectSession: TLStudioCallable;
  ensureSessionSelection: TLStudioCallable;
  loadMessages: TLStudioCallable;
  renderMessages: TLStudioCallable;
  showConversation: TLStudioCallable;
  sendPrompt: TLStudioCallable;
  resizePrompt: TLStudioCallable;
  switchAgent: TLStudioCallable;
  switchModel: TLStudioCallable;
  stopEvents: TLStudioCallable;
  startEvents: TLStudioCallable;
  handleLiveEvent: TLStudioCallable;
  handleRuntimeEvent: TLStudioCallable;
  handleKiloEvent: TLStudioCallable;
  stopSessionPolling: TLStudioCallable;
  startSessionPolling: TLStudioCallable;
  signInHosted: TLStudioCallable;
  cancelAuth: TLStudioCallable;
  copyAuthCode: TLStudioCallable;
  applyHostedAuthStatus: TLStudioCallable;
  refreshHostedAuthStatus: TLStudioCallable;
  loadAttention: TLStudioCallable;
  refreshPermissionRules: TLStudioCallable;
  loadToolRegistry: TLStudioCallable;
  toolDescriptor: TLStudioCallable;
  addAttachments: TLStudioCallable;
  clearAttachments: TLStudioCallable;
  renderAttachments: TLStudioCallable;
  openWorkspace: TLStudioCallable;
  openWorkspaceFileAt: TLStudioCallable;
  refreshWorkspaceControls: TLStudioCallable;
  refreshEditorHighlight: TLStudioCallable;
  renderChanges: TLStudioCallable;
  loadChanges: TLStudioCallable;
  deleteSessionFromSidebar: TLStudioCallable;
  activateSettingsSection: TLStudioCallable;
  stopCurrentSession: TLStudioCallable;

  presentation?: TLStudioDynamicRecord;
  preview?: TLStudioDynamicRecord;
  previewWindow?: TLStudioDynamicRecord;
  terminal?: TLStudioDynamicRecord;
  workspaceFiles?: TLStudioDynamicRecord;
  legacySessions?: TLStudioDynamicRecord;
  __statusDiagnostics?: TLStudioDynamicRecord;
  __providerRecovery?: TLStudioDynamicRecord;
  __providersUi?: TLStudioDynamicRecord;

  __attachmentsInstalled?: boolean;
  __diagnosticsUiInstalled?: boolean;
  __editorEnhancementsInstalled?: boolean;
  __ideFoundationInstalled?: boolean;
  __legacySessionsInstalled?: boolean;
  __previewFloatingInstalled?: boolean;
  __previewInstalled?: boolean;
  __projectSearchInstalled?: boolean;
  __providerRecoveryInstalled?: boolean;
  __providersSettingsBridgeInstalled?: boolean;
  __providersUiInstalled?: boolean;
  __settingsEnhancementsInstalled?: boolean;
  __terminalInstalled?: boolean;
  __toolRegistryInstalled?: boolean;
}

interface Window {
  KLU: TLStudioKernel;
}
