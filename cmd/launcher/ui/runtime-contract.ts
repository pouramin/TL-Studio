type RuntimeProviderID = string;
type RuntimeSessionID = string;

interface RuntimeModelRef {
  providerID: RuntimeProviderID;
  modelID: string;
}

interface RuntimeSessionCreateInput {
  parentID?: RuntimeSessionID;
  title?: string;
}

interface RuntimePromptInput {
  text?: string;
  parts?: unknown[];
  agent?: string;
  model?: RuntimeModelRef;
  variant?: string;
  messageID?: string;
  directory?: string;
}

interface RuntimeProviderModelConfig {
  id: string;
  name: string;
  toolCall: boolean;
  reasoning: boolean;
  contextLimit?: number;
  outputLimit?: number;
}

interface RuntimeProviderDefinition {
  id: RuntimeProviderID;
  name: string;
  protocol: "openai-compatible" | "openai-responses" | "anthropic-messages";
  baseURL: string;
  models: RuntimeProviderModelConfig[];
}

interface RuntimeProviderConfigContract {
  config(): Promise<{ providers: RuntimeProviderDefinition[] }>;
  upsert(providerID: RuntimeProviderID, input: { provider: RuntimeProviderDefinition; apiKey?: string }): Promise<unknown>;
  remove(providerID: RuntimeProviderID): Promise<unknown>;
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

interface TLStudioSessionModelRef {
  providerID?: string;
  id?: string;
}

interface TLStudioSessionView {
  id: RuntimeSessionID;
  title: string;
  directory: string;
  parentID?: RuntimeSessionID;
  agent?: string;
  model?: TLStudioSessionModelRef;
  createdAt?: number;
  updatedAt?: number;
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
  [key: string]: unknown;
}

interface TLStudioSessionMessage {
  id?: string;
  sessionID?: RuntimeSessionID;
  role: string;
  agent?: string;
  model?: TLStudioSessionModelRef;
  createdAt?: number;
  completedAt?: number;
  text?: string;
  error?: unknown;
  activities: TLStudioSessionActivity[];
  attachments: Array<{ name: string; mime?: string; url?: string }>;
  usage: TLStudioSessionUsage;
  changes: Array<{ file: string; additions: number; deletions: number; patch?: string }>;
}

interface TLStudioSessionStatus {
  state: "idle" | "running" | "retrying" | "unknown";
  active: boolean;
  attempt?: number;
  nextAt?: number;
  message?: string;
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
  sessionID?: RuntimeSessionID;
  messageID?: string;
  attentionKind?: "permission" | "question";
  path?: string;
}

interface TLStudioEventSubscribeOptions {
  onEvent?: (event: TLStudioLiveEvent) => void;
  onOpen?: (event: Event) => void;
  onError?: (event: Event) => void;
}

interface RuntimeHostedProviderContract {
  readonly providerID: RuntimeProviderID;
  readonly preferredModels: readonly string[];
  status(): Promise<{ authenticated: boolean; type: string; organizationId: string }>;
  authorize(): Promise<unknown>;
  callback(signal?: AbortSignal): Promise<unknown>;
  disconnect(): Promise<unknown>;
}

interface TLStudioRuntimeContract {
  readonly version: string;
  readonly hosted: RuntimeHostedProviderContract;
  health(): Promise<unknown>;
  agents(): Promise<unknown[]>;
  providerState(): Promise<unknown>;
  readonly providers: RuntimeProviderConfigContract;
  tools: {
    registry(): Promise<TLStudioToolRegistry>;
  };
  sessionView: {
    list(options?: { limit?: number }): Promise<TLStudioSessionView[]>;
    status(options?: { directory?: string }): Promise<Record<RuntimeSessionID, TLStudioSessionStatus>>;
    get(sessionID: RuntimeSessionID, options?: { directory?: string }): Promise<TLStudioSessionView>;
    messages(sessionID: RuntimeSessionID, options?: { limit?: number; directory?: string }): Promise<TLStudioSessionMessage[]>;
    changes(sessionID: RuntimeSessionID, options?: { directory?: string }): Promise<Array<{ file: string; additions: number; deletions: number; patch?: string }>>;
  };
  permissions: {
    list(sessionID?: RuntimeSessionID): Promise<unknown[]>;
    reply(sessionID: RuntimeSessionID, requestID: string, reply: "once" | "always" | "reject", message?: string): Promise<unknown>;
    rules(): Promise<TLStudioPermissionRule[]>;
    removeRule(ruleID: string): Promise<unknown>;
  };
  sessions: {
    create(input?: RuntimeSessionCreateInput): Promise<unknown>;
    update(sessionID: RuntimeSessionID, input?: { title?: string }, options?: { directory?: string }): Promise<unknown>;
    remove(sessionID: RuntimeSessionID, options?: { directory?: string }): Promise<unknown>;
    promptAsync(sessionID: RuntimeSessionID, input?: RuntimePromptInput): Promise<unknown>;
    abort(sessionID: RuntimeSessionID, options?: { scope?: string; directory?: string }): Promise<unknown>;
  };
  events: {
    subscribe(options?: TLStudioEventSubscribeOptions): EventSource;
  };
}

declare global {
  interface Window {
    KLU: {
      api?: TLStudioRuntimeContract;
      [key: string]: unknown;
    };
  }
}

export {};
