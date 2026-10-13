import { K } from "./kernel";

(() => {
  "use strict";

  if (!K || K.__mainWorkspaceInstalled) return;
  K.__mainWorkspaceInstalled = true;

  const root = document.getElementById("app");
  const legacySidebar = root?.querySelector<HTMLElement>(".sidebar") || null;
  const mainPane = root?.querySelector<HTMLElement>(".main-pane") || null;
  if (!root || !legacySidebar || !mainPane) return;

  if (!document.querySelector('link[href="/workbench.css"]')) {
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = "/workbench.css";
    document.head.appendChild(link);
  }

  const byId = <T extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as T | null;
  const node = <KTag extends keyof HTMLElementTagNameMap>(tag: KTag, className = "", text = "") => {
    const element = document.createElement(tag);
    if (className) element.className = className;
    if (text) element.textContent = text;
    return element;
  };
  const button = (text: string, title: string, className = "") => {
    const element = node("button", className, text);
    element.type = "button";
    element.title = title;
    return element;
  };
  const activityIcon = (name: "explorer" | "search" | "sessions" | "changes" | "terminal" | "settings") => {
    const paths: Record<string, string> = {
      explorer: '<path d="M3.75 6.75h6l1.5 1.5h9v10.5H3.75z"/><path d="M3.75 6.75v-2.5h5.2l1.5 1.5"/>',
      search: '<circle cx="10.75" cy="10.75" r="5.75"/><path d="m15 15 4.75 4.75"/>',
      sessions: '<path d="M4 5.25h16v11H9l-5 3.5z"/>',
      changes: '<circle cx="6" cy="5" r="2"/><circle cx="18" cy="8" r="2"/><circle cx="6" cy="19" r="2"/><path d="M6 7v10M8 5h3.5A6.5 6.5 0 0 1 18 11.5V6"/>',
      terminal: '<path d="m5 7 4 4-4 4"/><path d="M11.5 15H19"/>',
      settings: '<circle cx="12" cy="12" r="3"/><path d="M19 12a7 7 0 0 0-.1-1.2l2-1.55-2-3.45-2.45 1A7.2 7.2 0 0 0 14.4 5.6L14 3h-4l-.4 2.6a7.2 7.2 0 0 0-2.05 1.2l-2.45-1-2 3.45 2 1.55A7 7 0 0 0 5 12c0 .4.03.8.1 1.2l-2 1.55 2 3.45 2.45-1a7.2 7.2 0 0 0 2.05 1.2L10 21h4l.4-2.6a7.2 7.2 0 0 0 2.05-1.2l2.45 1 2-3.45-2-1.55c.07-.4.1-.8.1-1.2Z"/>',
    };
    return `<svg class="workspace-activity-icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">${paths[name]}</svg>`;
  };
  const clean = (value: unknown) => String(value || "").trim();
  const normalizePath = (value: unknown) => clean(value).replace(/\\/g, "/").replace(/^\/+|\/+$/g, "").toLowerCase();

  type AgentMode = "collapsed" | "compact" | "focused";
  type WorkspaceLayout = {
    sidebarWidth: number;
    agentWidth: number;
    terminalHeight: number;
    sidebarCollapsed: boolean;
    agentMode: AgentMode;
  };

  const LAYOUT_KEY = "tl-studio.workspace-layout.v1";
  const ACTIVITY_RAIL_WIDTH = 54;
  const EDITOR_MIN_WIDTH = 320;
  const AGENT_MIN_WIDTH = 280;
  const AGENT_COMPACT_MAX_WIDTH = 420;
  const AGENT_FOCUSED_MIN_WIDTH = 520;
  const defaults: WorkspaceLayout = {
    sidebarWidth: 248,
    agentWidth: 360,
    terminalHeight: 220,
    sidebarCollapsed: false,
    agentMode: "compact",
  };

  const readLayout = (): WorkspaceLayout => {
    try {
      const stored = JSON.parse(localStorage.getItem(LAYOUT_KEY) || "null") || {};
      return {
        sidebarWidth: Number(stored.sidebarWidth) || defaults.sidebarWidth,
        agentWidth: Number(stored.agentWidth) || defaults.agentWidth,
        terminalHeight: Number(stored.terminalHeight) || defaults.terminalHeight,
        sidebarCollapsed: stored.sidebarCollapsed === true,
        agentMode: ["collapsed", "compact", "focused"].includes(stored.agentMode) ? stored.agentMode : defaults.agentMode,
      };
    } catch {
      return { ...defaults };
    }
  };
  const layout = readLayout();
  const persistLayout = () => {
    try { localStorage.setItem(LAYOUT_KEY, JSON.stringify(layout)); }
    catch {}
  };
  const clamp = (value: number, minimum: number, maximum: number) => {
    const safeMax = Math.max(0, maximum);
    const safeMin = Math.min(Math.max(0, minimum), safeMax);
    return Math.min(Math.max(value, safeMin), safeMax);
  };

  root.classList.add("workspace-v2");

  const topbar = node("header", "workspace-topbar");
  const brand = node("div", "workspace-brand");
  const brandImage = document.createElement("img");
  brandImage.src = "/tl-studio-mark.svg";
  brandImage.alt = "";
  const brandCopy = node("div", "workspace-brand-copy");
  brandCopy.append(node("strong", "", "TL Studio"), node("span", "", "Development Workspace"));
  brand.append(brandImage, brandCopy);

  const projectSlot = node("div", "workspace-project-slot");
  const pickProject = byId<HTMLButtonElement>("pickProject");
  if (pickProject) {
    pickProject.classList.add("workspace-project-switcher");
    pickProject.title = "Switch project";
    projectSlot.appendChild(pickProject);
    // Explicit, discoverable entry point: the project name/path alone looked
    // like passive metadata and was easy to miss on first launch.
    const openFolder = button("Open Folder", "Open or switch a local project folder", "workspace-open-folder");
    openFolder.id = "workspaceOpenFolder";
    openFolder.setAttribute("aria-label", "Open or switch a project folder");
    openFolder.addEventListener("click", () => pickProject.click());
    projectSlot.appendChild(openFolder);
  }

  const commandTrigger = button("Search project or run a command", "Command Palette (Ctrl/Cmd+K)", "workspace-command-trigger");
  commandTrigger.id = "workspaceCommandTrigger";
  const commandHint = node("kbd", "", "Ctrl K");
  commandTrigger.appendChild(commandHint);

  const topActions = node("div", "workspace-top-actions");
  const backendStatus = byId("backendStatus");
  if (backendStatus) {
    backendStatus.classList.add("workspace-local-status");
    topActions.appendChild(backendStatus);
  }

  const previewButton = byId<HTMLButtonElement>("previewButton");
  if (previewButton) {
    previewButton.classList.add("workspace-top-action");
    topActions.appendChild(previewButton);
  }

  const themeButton = button("◐", "Toggle light/dark theme", "workspace-icon-button");
  themeButton.id = "workspaceThemeToggle";
  topActions.appendChild(themeButton);

  const refreshButton = byId<HTMLButtonElement>("refreshButton");
  if (refreshButton) {
    refreshButton.textContent = "↻";
    refreshButton.title = "Refresh workspace";
    refreshButton.classList.add("workspace-icon-button");
    topActions.appendChild(refreshButton);
  }

  const accountButton = byId<HTMLButtonElement>("accountButton");
  if (accountButton) {
    accountButton.classList.add("workspace-account-button");
    topActions.appendChild(accountButton);
  }

  topbar.append(brand, projectSlot, commandTrigger, topActions);

  const workbench = node("div", "workspace-workbench");
  const activityRail = node("nav", "workspace-activity-rail");
  activityRail.setAttribute("aria-label", "Workspace activity");
  const activityMain = node("div", "workspace-activity-main");
  const activityBottom = node("div", "workspace-activity-bottom");
  activityRail.append(activityMain, activityBottom);

  const contextSidebar = node("aside", "workspace-context-sidebar");
  contextSidebar.id = "workspaceContextSidebar";
  const contextViews = new Map<string, HTMLElement>();
  const makeContextView = (name: string, label: string) => {
    const section = node("section", "workspace-context-view");
    section.dataset.context = name;
    section.setAttribute("aria-label", label);
    contextSidebar.appendChild(section);
    contextViews.set(name, section);
    return section;
  };

  const explorerView = makeContextView("explorer", "Explorer");
  const searchView = makeContextView("search", "Project search");
  const sessionsView = makeContextView("sessions", "Sessions");
  const changesView = makeContextView("changes", "Changes");

  const filesPanel = byId("filesPanel");
  const filesHead = filesPanel?.querySelector<HTMLElement>(".files-head") || null;
  const filesBrowser = filesPanel?.querySelector<HTMLElement>(".files-browser") || null;
  if (filesPanel) {
    filesPanel.classList.add("workspace-mounted");
    filesPanel.classList.remove("hidden");
  }
  if (filesHead) {
    filesHead.classList.add("workspace-context-head");
    const closeFiles = byId("closeFiles");
    if (closeFiles) closeFiles.classList.add("workspace-hide-control");
    const compactExplorerAction = (id: string, label: string, title: string) => {
      const action = byId<HTMLButtonElement>(id);
      if (!action) return;
      action.textContent = label;
      action.title = title;
      action.setAttribute("aria-label", title);
      action.classList.add("workspace-explorer-action");
    };
    compactExplorerAction("newFile", "+", "New file");
    compactExplorerAction("newFolder", "▣", "New folder");
    compactExplorerAction("showInFolder", "↗", "Show active file in Folder");
    compactExplorerAction("refreshFiles", "↻", "Refresh Explorer");
    explorerView.appendChild(filesHead);
  }
  if (filesBrowser) {
    filesBrowser.classList.add("workspace-context-body");
    explorerView.appendChild(filesBrowser);
  }

  const searchPanel = byId("projectSearchPanel");
  if (searchPanel) {
    searchPanel.classList.add("workspace-context-embedded");
    searchView.appendChild(searchPanel);
  }

  const sessionHead = legacySidebar.querySelector<HTMLElement>(".sidebar-head");
  const sessionList = byId("sessions");
  if (sessionHead) {
    sessionHead.classList.add("workspace-context-head");
    sessionsView.appendChild(sessionHead);
  }
  if (sessionList) {
    sessionList.classList.add("workspace-context-body");
    sessionsView.appendChild(sessionList);
  }

  const changesPanel = byId("changesPanel");
  if (changesPanel) {
    changesPanel.classList.add("workspace-context-embedded");
    changesView.appendChild(changesPanel);
  }

  const sidebarSplitter = node("div", "workspace-splitter workspace-sidebar-splitter");
  sidebarSplitter.setAttribute("role", "separator");
  sidebarSplitter.setAttribute("aria-orientation", "vertical");
  sidebarSplitter.title = "Drag to resize the sidebar";

  const agentSplitter = node("div", "workspace-splitter workspace-agent-splitter");
  agentSplitter.setAttribute("role", "separator");
  agentSplitter.setAttribute("aria-orientation", "vertical");
  agentSplitter.title = "Drag to resize TL Agent";

  const agentPanel = node("aside", "workspace-agent-panel");
  agentPanel.setAttribute("aria-label", "TL Agent");
  const agentRail = node("div", "workspace-agent-collapsed-rail");
  const agentExpand = button("AI", "Expand TL Agent", "workspace-agent-rail-button");
  const agentRailStatus = node("span", "workspace-agent-rail-status", "Ready");
  const agentRailChanges = node("button", "workspace-agent-rail-changes", "");
  agentRailChanges.type = "button";
  agentRailChanges.title = "Show current session changes";
  agentRailChanges.setAttribute("aria-label", "Show current session changes");
  agentRail.append(agentExpand, agentRailStatus, agentRailChanges);

  const agentExpanded = node("div", "workspace-agent-expanded");
  const agentHeader = node("header", "workspace-agent-header");
  const toolbarTitle = mainPane.querySelector<HTMLElement>(".toolbar-title");
  if (toolbarTitle) {
    toolbarTitle.classList.add("workspace-agent-title");
    const agentIdentity = node("span", "workspace-agent-kicker", "TL Agent");
    toolbarTitle.insertBefore(agentIdentity, toolbarTitle.firstChild);
    agentHeader.appendChild(toolbarTitle);
  } else {
    const fallback = node("div", "workspace-agent-title");
    fallback.append(node("strong", "", "TL Agent"), node("span", "muted", "Local Agent"));
    agentHeader.appendChild(fallback);
  }

  const agentHeaderActions = node("div", "workspace-agent-header-actions");
  const sessionMenuButton = byId<HTMLButtonElement>("sessionMenuButton");
  if (sessionMenuButton) {
    sessionMenuButton.classList.add("workspace-agent-header-action");
    agentHeaderActions.appendChild(sessionMenuButton);
  }
  const compactAgent = button("▥", "Compact TL Agent", "workspace-agent-header-action");
  const focusAgent = button("□", "Focus TL Agent", "workspace-agent-header-action");
  const collapseAgent = button("›", "Collapse TL Agent", "workspace-agent-header-action");
  compactAgent.dataset.agentMode = "compact";
  focusAgent.dataset.agentMode = "focused";
  collapseAgent.dataset.agentMode = "collapsed";
  agentHeaderActions.append(compactAgent, focusAgent, collapseAgent);
  agentHeader.appendChild(agentHeaderActions);

  const conversation = byId("conversation");
  const composerWrap = mainPane.querySelector<HTMLElement>(".composer-wrap");
  if (conversation) conversation.classList.add("workspace-agent-thread");
  if (composerWrap) composerWrap.classList.add("workspace-agent-composer-wrap");

  // Per-run Agent budget and per-provider timeout preferences. These are
  // preferences only: the Go session contract validates and caps every value.
  if (composerWrap) {
    const budget = node("details", "workspace-agent-run-controls");
    budget.id = "agentRunControls";
    budget.innerHTML = `<summary title="Adjust tool rounds and provider timeouts">Agent limits</summary>
      <div class="workspace-agent-limits-grid">
        <label>Tool rounds<select id="agentMaxToolRounds" aria-label="Maximum tool rounds per Agent run">
          <option value="24">24 · Standard</option><option value="50">50 · Extended</option>
          <option value="100">100 · Heavy</option></select></label>
        <label>Model request<select id="agentModelTimeoutSeconds" aria-label="Model request timeout for selected provider">
          <option value="0">Default · 4–6 min</option><option value="120">2 min</option>
          <option value="240">4 min</option><option value="360">6 min</option>
          <option value="600">10 min</option></select></label>
        <label>Stream idle<select id="agentStreamIdleSeconds" aria-label="Maximum gap between streaming events for selected provider">
          <option value="0">Off · Default</option><option value="30">30 sec</option>
          <option value="60">60 sec</option><option value="120">120 sec</option></select></label>
        <label>Token budget<select id="agentMaxRunTokens" aria-label="Per-run total model token budget">
          <option value="0">No token cap</option><option value="200000">200K</option>
          <option value="1000000">1M</option><option value="2000000">2M</option></select></label>
      </div>
      <small>Round and token limits apply to each run. Timeout and idle preferences are saved separately per selected provider. Idle applies to streamed HTTP providers.</small>`;
    composerWrap.insertBefore(budget, composerWrap.firstChild);
    const rounds = budget.querySelector<HTMLSelectElement>("#agentMaxToolRounds")!;
    const tokens = budget.querySelector<HTMLSelectElement>("#agentMaxRunTokens")!;
    const timeout = budget.querySelector<HTMLSelectElement>("#agentModelTimeoutSeconds")!;
    const idle = budget.querySelector<HTMLSelectElement>("#agentStreamIdleSeconds")!;
    const safeValue = (select: HTMLSelectElement, value: string) =>
      [...select.options].some((option) => option.value === value) ? value : select.options[0].value;
    const read = (key: string, fallback: string) => {
      try { return localStorage.getItem(key) || fallback; } catch { return fallback; }
    };
    const store = (key: string, value: string) => {
      try { localStorage.setItem(key, value); } catch {}
    };
    const providerKey = () => String(K.selectedModel?.()?.providerID || "default").trim().toLowerCase();
    const loadTimeouts = () => {
      const key = providerKey();
      timeout.value = safeValue(timeout, read("tl-studio.agent.timeout." + key, "0"));
      idle.value = safeValue(idle, read("tl-studio.agent.idle." + key, "0"));
    };
    rounds.value = safeValue(rounds, read("tl-studio.agent.rounds", "24"));
    tokens.value = safeValue(tokens, read("tl-studio.agent.tokens", "0"));
    rounds.addEventListener("change", () => store("tl-studio.agent.rounds", rounds.value));
    tokens.addEventListener("change", () => store("tl-studio.agent.tokens", tokens.value));
    timeout.addEventListener("change", () => store("tl-studio.agent.timeout." + providerKey(), timeout.value));
    idle.addEventListener("change", () => store("tl-studio.agent.idle." + providerKey(), idle.value));
    byId<HTMLSelectElement>("modelSelect")?.addEventListener("change", loadTimeouts);
    loadTimeouts();
  }

  agentExpanded.appendChild(agentHeader);
  if (conversation) agentExpanded.appendChild(conversation);
  if (composerWrap) agentExpanded.appendChild(composerWrap);
  agentPanel.append(agentRail, agentExpanded);

  const toolbar = mainPane.querySelector<HTMLElement>(".toolbar");
  if (toolbar) toolbar.classList.add("workspace-legacy-toolbar");
  const emptyState = byId("emptyState");
  if (emptyState) emptyState.classList.add("workspace-legacy-empty");

  mainPane.classList.add("workspace-editor-column");

  const terminalPanel = byId("terminalPanel");
  const terminalSplitter = node("div", "workspace-splitter workspace-terminal-splitter hidden");
  terminalSplitter.setAttribute("role", "separator");
  terminalSplitter.setAttribute("aria-orientation", "horizontal");
  terminalSplitter.title = "Drag to resize Terminal";

  if (filesPanel) mainPane.insertBefore(filesPanel, mainPane.firstChild);
  if (terminalPanel) {
    terminalPanel.classList.add("workspace-terminal-panel");
    mainPane.append(terminalSplitter, terminalPanel);
  }

  const sidebarToggle = button("‹", "Collapse Context Sidebar", "workspace-rail-button workspace-sidebar-toggle");
  sidebarToggle.id = "workspaceSidebarToggle";
  activityBottom.appendChild(sidebarToggle);

  const activityDefinitions: Array<["explorer" | "search" | "sessions" | "changes", string]> = [
    ["explorer", "Explorer"],
    ["search", "Search"],
    ["sessions", "Sessions"],
    ["changes", "Changes"],
  ];
  const activityButtons = new Map<string, HTMLButtonElement>();
  for (const [name, label] of activityDefinitions) {
    const item = button("", label, "workspace-activity-button");
    item.innerHTML = activityIcon(name);
    item.dataset.workspaceView = name;
    item.setAttribute("aria-label", label);
    const badge = node("span", "workspace-activity-badge");
    badge.dataset.badgeFor = name;
    item.appendChild(badge);
    activityMain.appendChild(item);
    activityButtons.set(name, item);
  }

  const terminalActivity = button("", "Terminal", "workspace-activity-button workspace-terminal-activity");
  terminalActivity.innerHTML = activityIcon("terminal");
  terminalActivity.id = "workspaceTerminalActivity";
  terminalActivity.type = "button";
  terminalActivity.title = "Open Terminal";
  terminalActivity.setAttribute("aria-label", "Terminal");
  terminalActivity.setAttribute("aria-pressed", "false");
  activityMain.appendChild(terminalActivity);

  const settingsButton = byId<HTMLButtonElement>("settingsButton");
  if (settingsButton) {
    settingsButton.innerHTML = activityIcon("settings");
    settingsButton.className = "workspace-activity-button workspace-settings-activity";
    settingsButton.title = "Settings";
    settingsButton.setAttribute("aria-label", "Settings");
    activityBottom.appendChild(settingsButton);
  }

  const statusbar = node("footer", "workspace-statusbar");
  const statusLocal = node("span", "workspace-status-item", "Local");
  const statusChanges = node("button", "workspace-status-button", "Changes 0");
  statusChanges.type = "button";
  const statusAgent = node("span", "workspace-status-item", "Agent ready");
  const statusSpacer = node("span", "workspace-status-spacer");
  const statusModel = node("button", "workspace-status-button", "Backend default");
  statusModel.type = "button";
  statusModel.title = "Change model";
  const statusCursor = node("span", "workspace-status-item", "Ln 1, Col 1");
  const statusTerminal = node("button", "workspace-status-button", "Terminal");
  statusTerminal.type = "button";
  statusbar.append(statusLocal, statusChanges, statusAgent, statusSpacer, statusModel, statusCursor, statusTerminal);

  const firstDialog = root.querySelector(":scope > dialog");
  root.insertBefore(topbar, legacySidebar);
  root.insertBefore(workbench, legacySidebar);
  workbench.append(activityRail, contextSidebar, sidebarSplitter, mainPane, agentSplitter, agentPanel);
  if (firstDialog) root.insertBefore(statusbar, firstDialog);
  else root.appendChild(statusbar);
  legacySidebar.remove();

  let currentView = "explorer";
  const setContext = (name: string, options: { focus?: boolean } = {}) => {
    if (!contextViews.has(name)) name = "explorer";
    currentView = name;
    layout.sidebarCollapsed = false;
    for (const [key, view] of contextViews) {
      const active = key === name;
      view.classList.toggle("active", active);
      view.setAttribute("aria-hidden", active ? "false" : "true");
    }
    for (const [key, item] of activityButtons) {
      const active = key === name;
      item.classList.toggle("active", active);
      item.setAttribute("aria-pressed", active ? "true" : "false");
    }
    if (name === "explorer") byId<HTMLButtonElement>("filesButton")?.click();
    if (name === "search") byId<HTMLButtonElement>("searchButton")?.click();
    if (name === "changes") byId<HTMLButtonElement>("changesButton")?.click();
    applyLayout();
    if (options.focus && name === "search") {
      window.setTimeout(() => byId<HTMLInputElement>("projectSearchQuery")?.focus(), 0);
    }
  };

  for (const [name, item] of activityButtons) {
    item.addEventListener("click", () => setContext(name, { focus: name === "search" }));
  }

  byId<HTMLButtonElement>("closeProjectSearch")?.addEventListener("click", () => setContext("explorer"));
  byId<HTMLButtonElement>("closeChanges")?.addEventListener("click", () => setContext("explorer"));
  searchView.addEventListener("click", (event) => {
    if (!(event.target as Element | null)?.closest(".project-search-match")) return;
    // The legacy Search result handler hides its former overlay after
    // navigation. In the contextual workbench Search remains the selected
    // activity while the real Editor opens the result.
    window.setTimeout(() => searchPanel?.classList.remove("hidden"), 0);
  });

  const workbenchWidth = () => Math.max(0, workbench.clientWidth || window.innerWidth);
  const autoHideContext = () => window.innerWidth <= 1120;
  const contextWidthForLayout = () => (
    layout.sidebarCollapsed || autoHideContext() ? 0 : layout.sidebarWidth
  );
  const maxAgentWidth = () => Math.max(
    48,
    workbenchWidth() - ACTIVITY_RAIL_WIDTH - contextWidthForLayout() - 12 - EDITOR_MIN_WIDTH,
  );

  const effectiveAgentWidth = () => {
    if (layout.agentMode === "collapsed") return 48;
    const max = maxAgentWidth();
    const requested = layout.agentMode === "focused"
      ? Math.max(layout.agentWidth, AGENT_FOCUSED_MIN_WIDTH)
      : Math.min(layout.agentWidth, AGENT_COMPACT_MAX_WIDTH);
    return clamp(requested, Math.min(AGENT_MIN_WIDTH, max), max);
  };

  const applyLayout = () => {
    const benchWidth = workbenchWidth();
    const contextHidden = layout.sidebarCollapsed || autoHideContext();
    const maxSidebar = Math.max(
      0,
      benchWidth - ACTIVITY_RAIL_WIDTH - AGENT_MIN_WIDTH - 12 - EDITOR_MIN_WIDTH,
    );
    if (!contextHidden) {
      layout.sidebarWidth = clamp(layout.sidebarWidth, Math.min(180, maxSidebar), maxSidebar);
    }

    const sidebar = contextHidden ? 0 : layout.sidebarWidth;
    const rawMaxAgent = Math.max(
      48,
      benchWidth - ACTIVITY_RAIL_WIDTH - sidebar - 12 - EDITOR_MIN_WIDTH,
    );
    layout.agentWidth = clamp(
      layout.agentWidth,
      Math.min(AGENT_MIN_WIDTH, rawMaxAgent),
      rawMaxAgent,
    );
    layout.terminalHeight = clamp(layout.terminalHeight, 96, Math.max(96, mainPane.clientHeight * 0.58 || 420));

    root.style.setProperty("--workspace-context-width", (contextHidden ? 0 : layout.sidebarWidth) + "px");
    root.style.setProperty("--workspace-agent-width", effectiveAgentWidth() + "px");
    root.style.setProperty("--workspace-terminal-height", layout.terminalHeight + "px");
    root.classList.toggle("workspace-context-collapsed", contextHidden);
    root.classList.toggle("workspace-agent-collapsed", layout.agentMode === "collapsed");
    root.classList.toggle("workspace-agent-focused", layout.agentMode === "focused");
    sidebarToggle.textContent = layout.sidebarCollapsed ? "›" : "‹";
    sidebarToggle.title = layout.sidebarCollapsed ? "Expand Context Sidebar" : "Collapse Context Sidebar";
    compactAgent.classList.toggle("active", layout.agentMode === "compact");
    focusAgent.classList.toggle("active", layout.agentMode === "focused");
    compactAgent.setAttribute("aria-pressed", layout.agentMode === "compact" ? "true" : "false");
    focusAgent.setAttribute("aria-pressed", layout.agentMode === "focused" ? "true" : "false");
    collapseAgent.setAttribute("aria-pressed", layout.agentMode === "collapsed" ? "true" : "false");
  };

  const setAgentMode = (mode: AgentMode) => {
    if (mode === "compact" && (layout.agentWidth > AGENT_COMPACT_MAX_WIDTH || layout.agentWidth < AGENT_MIN_WIDTH)) {
      layout.agentWidth = defaults.agentWidth;
    }
    layout.agentMode = mode;
    applyLayout();
    persistLayout();
  };
  compactAgent.addEventListener("click", () => setAgentMode("compact"));
  focusAgent.addEventListener("click", () => setAgentMode("focused"));
  collapseAgent.addEventListener("click", () => setAgentMode("collapsed"));
  agentExpand.addEventListener("click", () => setAgentMode("compact"));
  sidebarToggle.addEventListener("click", () => {
    layout.sidebarCollapsed = !layout.sidebarCollapsed;
    applyLayout();
    persistLayout();
  });

  type ResizeState = {
    kind: "sidebar" | "agent" | "terminal";
    pointerId: number;
    startX: number;
    startY: number;
    startSize: number;
  };
  let resizeState: ResizeState | null = null;
  const beginResize = (kind: ResizeState["kind"], event: PointerEvent) => {
    if (event.button !== 0) return;
    resizeState = {
      kind,
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      startSize: kind === "sidebar" ? layout.sidebarWidth : kind === "agent" ? effectiveAgentWidth() : layout.terminalHeight,
    };
    document.documentElement.classList.add("workspace-resizing", "workspace-resize-" + (kind === "terminal" ? "ns" : "ew"));
    event.preventDefault();
  };
  sidebarSplitter.addEventListener("pointerdown", (event) => beginResize("sidebar", event));
  agentSplitter.addEventListener("pointerdown", (event) => beginResize("agent", event));
  terminalSplitter.addEventListener("pointerdown", (event) => beginResize("terminal", event));

  document.addEventListener("pointermove", (event) => {
    if (!resizeState || event.pointerId !== resizeState.pointerId) return;
    if (resizeState.kind === "sidebar") {
      layout.sidebarWidth = resizeState.startSize + (event.clientX - resizeState.startX);
      layout.sidebarCollapsed = false;
    } else if (resizeState.kind === "agent") {
      layout.agentWidth = resizeState.startSize - (event.clientX - resizeState.startX);
      if (layout.agentMode === "collapsed") layout.agentMode = "compact";
    } else {
      layout.terminalHeight = resizeState.startSize - (event.clientY - resizeState.startY);
    }
    applyLayout();
  });
  const endResize = (event: PointerEvent) => {
    if (!resizeState || event.pointerId !== resizeState.pointerId) return;
    resizeState = null;
    document.documentElement.classList.remove("workspace-resizing", "workspace-resize-ew", "workspace-resize-ns");
    persistLayout();
  };
  document.addEventListener("pointerup", endResize);
  document.addEventListener("pointercancel", endResize);

  const syncTerminal = () => {
    const open = !!terminalPanel && !terminalPanel.classList.contains("hidden");
    terminalSplitter.classList.toggle("hidden", !open);
    mainPane.classList.toggle("workspace-terminal-open", open);
    terminalActivity.classList.toggle("active", open);
    terminalActivity.setAttribute("aria-pressed", open ? "true" : "false");
  };
  if (terminalPanel) {
    new MutationObserver(syncTerminal).observe(terminalPanel, { attributes: true, attributeFilter: ["class"] });
    syncTerminal();
  }

  const terminalButton = byId<HTMLButtonElement>("terminalButton");
  terminalActivity.addEventListener("click", () => terminalButton?.click());
  statusTerminal.addEventListener("click", () => terminalButton?.click());
  statusChanges.addEventListener("click", () => setContext("changes"));
  agentRailChanges.addEventListener("click", () => setContext("changes"));
  statusModel.addEventListener("click", () => {
    setAgentMode("compact");
    byId<HTMLSelectElement>("modelSelect")?.focus();
  });

  themeButton.addEventListener("click", () => {
    const select = byId<HTMLSelectElement>("appearanceSelect");
    if (!select) return;
    const resolved = document.documentElement.dataset.resolvedTheme || "dark";
    select.value = resolved === "light" ? "dark" : "light";
    select.dispatchEvent(new Event("change", { bubbles: true }));
  });

  const paletteBackdrop = node("div", "workspace-command-backdrop hidden");
  paletteBackdrop.id = "workspaceCommandPalette";
  const palette = node("div", "workspace-command-palette");
  palette.setAttribute("role", "dialog");
  palette.setAttribute("aria-modal", "true");
  palette.setAttribute("aria-label", "Command Palette");
  const paletteInput = document.createElement("input");
  paletteInput.type = "search";
  paletteInput.placeholder = "Search commands…";
  paletteInput.autocomplete = "off";
  paletteInput.spellcheck = false;
  paletteInput.setAttribute("aria-label", "Search commands");
  const paletteList = node("div", "workspace-command-list");
  palette.append(paletteInput, paletteList);
  paletteBackdrop.appendChild(palette);
  root.appendChild(paletteBackdrop);

  const commands = [
    { label: "Open project", hint: "Choose a local project folder", run: () => pickProject?.click() },
    { label: "Open file", hint: "Show Explorer and browse project files", run: () => setContext("explorer") },
    { label: "Search project", hint: "Search file contents", run: () => setContext("search", { focus: true }) },
    { label: "Switch session", hint: "Show TL Agent sessions", run: () => setContext("sessions") },
    { label: "Focus TL Agent", hint: "Give the Agent more workspace", run: () => setAgentMode("focused") },
    { label: "Compact TL Agent", hint: "Use the normal collaboration layout", run: () => setAgentMode("compact") },
    { label: "Collapse TL Agent", hint: "Return space to the Editor", run: () => setAgentMode("collapsed") },
    { label: "Open Live Preview", hint: "Open the real project Preview", run: () => previewButton?.click() },
    { label: "Toggle Terminal", hint: "Open or close the project command runner", run: () => terminalButton?.click() },
    { label: "Change model", hint: "Focus the real model selector", run: () => statusModel.click() },
    { label: "Open Settings", hint: "Configure TL Studio", run: () => settingsButton?.click() },
  ];

  const closePalette = () => {
    paletteBackdrop.classList.add("hidden");
    paletteInput.value = "";
  };
  let commandIndex = 0;
  const visibleCommandButtons = () => [...paletteList.querySelectorAll<HTMLButtonElement>(".workspace-command-item")];
  const selectCommand = (index: number) => {
    const items = visibleCommandButtons();
    if (!items.length) return;
    commandIndex = (index + items.length) % items.length;
    items.forEach((item, itemIndex) => item.classList.toggle("selected", itemIndex === commandIndex));
    items[commandIndex]?.scrollIntoView({ block: "nearest" });
  };
  const renderCommands = () => {
    const query = paletteInput.value.trim().toLowerCase();
    paletteList.textContent = "";
    commandIndex = 0;
    for (const command of commands.filter((item) => !query || (item.label + " " + item.hint).toLowerCase().includes(query))) {
      const item = button("", command.label, "workspace-command-item");
      const copy = node("span", "workspace-command-copy");
      copy.append(node("strong", "", command.label), node("small", "", command.hint));
      item.appendChild(copy);
      item.addEventListener("click", () => {
        closePalette();
        command.run();
      });
      paletteList.appendChild(item);
    }
    selectCommand(0);
  };
  const openPalette = () => {
    renderCommands();
    paletteBackdrop.classList.remove("hidden");
    window.setTimeout(() => paletteInput.focus(), 0);
  };
  commandTrigger.addEventListener("click", openPalette);
  paletteInput.addEventListener("input", renderCommands);
  paletteInput.addEventListener("keydown", (event) => {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      selectCommand(commandIndex + 1);
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      selectCommand(commandIndex - 1);
    } else if (event.key === "Enter") {
      const current = visibleCommandButtons()[commandIndex];
      if (current) {
        event.preventDefault();
        current.click();
      }
    }
  });
  paletteBackdrop.addEventListener("pointerdown", (event) => {
    if (event.target === paletteBackdrop) closePalette();
  });

  document.addEventListener("keydown", (event) => {
    if ((event.ctrlKey || event.metaKey) && !event.shiftKey && event.key.toLowerCase() === "k") {
      event.preventDefault();
      openPalette();
      return;
    }
    if (event.key === "Escape" && !paletteBackdrop.classList.contains("hidden")) {
      event.preventDefault();
      closePalette();
    }
    if ((event.ctrlKey || event.metaKey) && event.shiftKey && event.key.toLowerCase() === "f") {
      setContext("search", { focus: true });
    }
  });

  const syncAgentFileStates = () => {
    const changed = new Set((Array.isArray(K.state.changes) ? K.state.changes : []).map((item: any) => normalizePath(item?.file)));
    for (const tab of document.querySelectorAll<HTMLElement>(".file-tab")) {
      const open = tab.querySelector<HTMLElement>(".file-tab-open");
      const path = normalizePath(open?.getAttribute("title"));
      const marked = !!path && changed.has(path);
      tab.classList.toggle("agent-modified", marked);
      let marker = tab.querySelector<HTMLElement>(".workspace-agent-file-marker");
      if (marked && !marker) {
        marker = node("span", "workspace-agent-file-marker", "A");
        marker.title = "Changed in the current Agent session";
        open?.appendChild(marker);
      } else if (!marked) {
        marker?.remove();
      }
    }
  };

  const syncStatus = () => {
    const backendText = clean(backendStatus?.textContent) || "Local";
    statusLocal.textContent = backendText;
    const changes = Number(clean(byId("changesCount")?.textContent)) || (Array.isArray(K.state.changes) ? K.state.changes.length : 0);
    statusChanges.textContent = "Changes " + changes;
    agentRailChanges.textContent = changes ? String(changes) : "";
    agentRailChanges.classList.toggle("visible", changes > 0);
    const session = K.state.session;
    const running = !!session && (K.state.sending || K.isSessionRunning?.(session.id));
    statusAgent.textContent = running ? "Agent working" : session ? "Agent ready" : "No session";
    agentRailStatus.textContent = running ? "Working" : "Ready";
    const model = byId<HTMLSelectElement>("modelSelect");
    statusModel.textContent = clean(model?.selectedOptions?.[0]?.textContent) || "Backend default";
    statusCursor.textContent = clean(byId("editorCursor")?.textContent) || "Ln 1, Col 1";

    const sessionBadge = activityRail.querySelector<HTMLElement>('[data-badge-for="sessions"]');
    const changesBadge = activityRail.querySelector<HTMLElement>('[data-badge-for="changes"]');
    const sessionCount = sessionList?.children.length || 0;
    if (sessionBadge) {
      sessionBadge.textContent = sessionCount ? String(sessionCount) : "";
      sessionBadge.classList.toggle("visible", sessionCount > 0);
    }
    if (changesBadge) {
      changesBadge.textContent = changes ? String(changes) : "";
      changesBadge.classList.toggle("visible", changes > 0);
    }
    syncAgentFileStates();
  };

  for (const name of ["renderMessages", "renderSessionHeader", "renderSessions", "syncSelectors", "renderChanges"] as const) {
    const original = K[name];
    if (typeof original !== "function") continue;
    K[name] = (...args: any[]) => {
      const result = original(...args);
      queueMicrotask(syncStatus);
      return result;
    };
  }

  const modelSelect = byId<HTMLSelectElement>("modelSelect");
  modelSelect?.addEventListener("change", syncStatus);
  window.addEventListener("tl-studio:editor-tabs", syncAgentFileStates);

  const observerTargets = [backendStatus, byId("changesCount"), sessionList, byId("editorCursor")].filter(Boolean) as Node[];
  const observer = new MutationObserver(() => syncStatus());
  for (const target of observerTargets) observer.observe(target, { childList: true, subtree: true, characterData: true, attributes: true });

  const baseAfterProjectChange = K.afterProjectChange;
  if (typeof baseAfterProjectChange === "function") {
    K.afterProjectChange = async (...args: any[]) => {
      const change = baseAfterProjectChange(...args);
      // The base handler invalidates old project scopes synchronously.
      const scope = K.viewScope.captureProject();
      const result = await change;
      if (!K.viewScope.isProjectCurrent(scope)) return result;
      filesPanel?.classList.remove("hidden");
      setContext("explorer");
      await K.openWorkspace?.().catch(() => undefined);
      if (K.viewScope.isProjectCurrent(scope)) syncStatus();
      return result;
    };
  }

  const initializeWorkspace = async (attempt = 0): Promise<void> => {
    if (K.state.local?.project) {
      filesPanel?.classList.remove("hidden");
      await K.openWorkspace?.().catch(() => undefined);
      syncStatus();
      return;
    }
    if (attempt >= 40) return;
    window.setTimeout(() => { void initializeWorkspace(attempt + 1); }, 100);
  };

  window.addEventListener("resize", () => {
    applyLayout();
    persistLayout();
  });

  setContext(currentView);
  applyLayout();
  syncStatus();
  void initializeWorkspace();
})();
