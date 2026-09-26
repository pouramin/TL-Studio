import { K } from "./kernel";

(() => {
  "use strict";

  if (!K || K.__jevUiInstalled) return;
  K.__jevUiInstalled = true;

  const JEV_ROUTER_MODEL = "typesafe/jev-router";
  const OPENROUTER_BASE_URL = "https://openrouter.ai/api/v1";
  const panel = document.querySelector<HTMLElement>('[data-settings-panel="providers"]');
  const providerList = document.getElementById("providerList");
  if (!panel || !providerList || !K.__providersUi) return;

  const clean = (value: unknown) => String(value ?? "").trim();
  const isOpenRouter = (value: unknown) => {
    try {
      const url = new URL(clean(value));
      return url.protocol === "https:"
        && url.hostname.toLowerCase() === "openrouter.ai"
        && url.pathname.replace(/\/+$/, "") === "/api/v1";
    } catch {
      return false;
    }
  };

  const card = document.createElement("section");
  card.className = "jev-settings-card";
  card.innerHTML = `
    <div class="jev-settings-head">
      <div>
        <strong>TypeSafe Jev</strong>
        <span>Jev Router is a normal generative router through OpenRouter. Direct Jev decisions are a separate, optional paid capability.</span>
      </div>
      <button id="jevRouterSetupButton" type="button" class="ghost small">Set up Jev Router</button>
    </div>
    <div id="jevRouterStatus" class="jev-settings-status"></div>
    <div class="jev-decision-row">
      <label for="jevDecisionEngineSelect">
        <strong>Decision Engine</strong>
        <span>Off by default. Enabling Jev only configures the Decisions API; TL Studio does not call it automatically.</span>
      </label>
      <select id="jevDecisionEngineSelect">
        <option value="off">Off</option>
        <option value="jev">Jev via OpenRouter (paid)</option>
      </select>
    </div>
    <div id="jevDecisionStatus" class="jev-settings-status"></div>
  `;
  providerList.insertAdjacentElement("afterend", card);

  const style = document.createElement("style");
  style.id = "tl-jev-ui-style";
  style.textContent = `
    .jev-settings-card{display:grid;gap:10px;margin-top:12px;padding:12px;border:1px solid var(--line);border-radius:10px;background:var(--panel-2)}
    .jev-settings-head{display:flex;align-items:flex-start;justify-content:space-between;gap:12px}.jev-settings-head strong,.jev-settings-head span,.jev-decision-row strong,.jev-decision-row span{display:block}.jev-settings-head strong,.jev-decision-row strong{font-size:10px}.jev-settings-head span,.jev-decision-row span{margin-top:3px;color:var(--muted);font-size:9px;line-height:1.45}
    .jev-settings-status{min-height:12px;color:var(--muted);font-size:9px;line-height:1.45}.jev-settings-status.error{color:var(--danger)}.jev-settings-status.ok{color:color-mix(in srgb,var(--accent),var(--text) 35%)}
    .jev-decision-row{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:12px;padding-top:9px;border-top:1px solid var(--line)}.jev-decision-row select{min-width:190px;height:32px}
    @media(max-width:760px){.jev-settings-head,.jev-decision-row{grid-template-columns:1fr;display:grid}.jev-settings-head button,.jev-decision-row select{width:100%}}
  `;
  document.head.appendChild(style);

  const setupButton = document.getElementById("jevRouterSetupButton") as HTMLButtonElement;
  const routerStatus = document.getElementById("jevRouterStatus") as HTMLElement;
  const decisionSelect = document.getElementById("jevDecisionEngineSelect") as HTMLSelectElement;
  const decisionStatus = document.getElementById("jevDecisionStatus") as HTMLElement;
  const providersUI = K.__providersUi;

  const setText = (element: HTMLElement, message = "", kind = "") => {
    element.textContent = message;
    element.className = `jev-settings-status${kind ? ` ${kind}` : ""}`;
  };

  const nextProviderID = (providers: TLStudioDynamicRecord[]) => {
    const used = new Set(providers.map((provider) => clean(provider?.id)));
    if (!used.has("openrouter")) return "openrouter";
    if (!used.has("typesafe-openrouter")) return "typesafe-openrouter";
    let suffix = 2;
    while (used.has(`typesafe-openrouter-${suffix}`)) suffix++;
    return `typesafe-openrouter-${suffix}`;
  };

  const openExistingOpenRouter = async (provider: TLStudioDynamicRecord) => {
    providersUI.discoverySelection?.reset?.();
    const first = Array.isArray(provider?.models) && provider.models.length ? provider.models[0] : {};
    providersUI.openProvider?.({
      providerID: provider.id,
      name: provider.name || provider.id,
      protocol: provider.protocol || "openai-compatible",
      baseURL: provider.baseURL || OPENROUTER_BASE_URL,
      modelID: first.id || "",
      modelName: first.name || first.id || "",
      contextLimit: first.contextLimit || "",
      outputLimit: first.outputLimit || "",
      toolCall: first.toolCall !== false,
      reasoning: first.reasoning === true,
    }, provider.id);
    setText(routerStatus, `Reusing existing OpenRouter provider “${provider.name || provider.id}” and its stored credential. Discovering Jev Router…`);
    try {
      const found = await providersUI.discoverySelection?.discoverModel?.(JEV_ROUTER_MODEL);
      setText(routerStatus, found
        ? "Jev Router was found in the OpenRouter catalog and selected. Review the live capability metadata and Save provider."
        : "OpenRouter responded, but Jev Router was not available in the returned catalog. Nothing paid was substituted automatically.",
        found ? "ok" : "error");
    } catch (error) {
      setText(routerStatus, `OpenRouter is configured, but discovery failed: ${error instanceof Error ? error.message : String(error)}`, "error");
    }
  };

  const openNewOpenRouter = (providers: TLStudioDynamicRecord[]) => {
    providersUI.discoverySelection?.reset?.();
    providersUI.openProvider?.({
      providerID: nextProviderID(providers),
      name: "TypeSafe via OpenRouter",
      protocol: "openai-compatible",
      baseURL: OPENROUTER_BASE_URL,
      modelID: "",
      modelName: "",
      contextLimit: "",
      outputLimit: "",
      toolCall: true,
      reasoning: false,
    });
    setText(routerStatus, "OpenRouter is not configured yet. Enter your OpenRouter API key, click Test / Discover Models, select Jev Router, then Save provider.");
    (document.getElementById("providerApiKeyInput") as HTMLInputElement | null)?.focus();
  };

  const setupJevRouter = async () => {
    setupButton.disabled = true;
    setText(routerStatus, "Checking existing provider configuration…");
    try {
      const config = await K.api.providers.config();
      const providers = Array.isArray(config?.providers) ? config.providers : [];
      const existing = providers.find((provider: TLStudioDynamicRecord) => isOpenRouter(provider?.baseURL));
      if (existing) await openExistingOpenRouter(existing);
      else openNewOpenRouter(providers);
    } catch (error) {
      setText(routerStatus, `Could not prepare Jev Router: ${error instanceof Error ? error.message : String(error)}`, "error");
    } finally {
      setupButton.disabled = false;
    }
  };

  const loadDecisionStatus = async () => {
    decisionSelect.disabled = true;
    try {
      const status = await K.api.decisionEngine.status();
      decisionSelect.value = status?.engine === "jev" ? "jev" : "off";
      if (status?.engine === "jev") {
        const provider = clean(status?.providerName || status?.providerID);
        setText(decisionStatus, `Jev Decision Engine is enabled${provider ? ` using ${provider}` : ""}. No automatic Agent or permission decisions are active.`, "ok");
      } else if (status?.providerConfigured) {
        setText(decisionStatus, "Decision Engine is off. The existing OpenRouter credential can be reused if you enable Jev.");
      } else {
        setText(decisionStatus, "Decision Engine is off. Configure OpenRouter above before enabling paid Jev decisions.");
      }
    } catch (error) {
      setText(decisionStatus, `Could not read Decision Engine settings: ${error instanceof Error ? error.message : String(error)}`, "error");
    } finally {
      decisionSelect.disabled = false;
    }
  };

  const changeDecisionEngine = async () => {
    const next = decisionSelect.value === "jev" ? "jev" : "off";
    if (next === "jev") {
      const approved = window.confirm(
        "Enable Jev Decision Engine? Direct Jev Decision API calls are paid through your OpenRouter account. "
        + "TL Studio will reuse your existing OpenRouter credential. This setting does not enable any automatic routing, tool, permission, or verification calls."
      );
      if (!approved) {
        await loadDecisionStatus();
        return;
      }
    }
    decisionSelect.disabled = true;
    try {
      const status = await K.api.decisionEngine.configure(next);
      decisionSelect.value = status?.engine === "jev" ? "jev" : "off";
      setText(decisionStatus, next === "jev"
        ? "Jev Decision Engine configured. It remains idle until a future or explicit Decision Engine call uses it."
        : "Decision Engine is off.", "ok");
    } catch (error) {
      setText(decisionStatus, `Could not update Decision Engine: ${error instanceof Error ? error.message : String(error)}`, "error");
      await loadDecisionStatus();
    } finally {
      decisionSelect.disabled = false;
    }
  };

  setupButton.addEventListener("click", () => { void setupJevRouter(); });
  decisionSelect.addEventListener("change", () => { void changeDecisionEngine(); });

  const providersNav = document.querySelector<HTMLElement>('[data-settings-section="providers"]');
  providersNav?.addEventListener("click", () => { void loadDecisionStatus(); });
  void loadDecisionStatus();
})();
