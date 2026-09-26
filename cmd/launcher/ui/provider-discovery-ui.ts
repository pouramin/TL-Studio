import { K } from "./kernel";

(() => {
  "use strict";

  const providersUI = K.__providersUi;
  const form = document.getElementById("providerForm") as HTMLFormElement | null;
  const grid = form?.querySelector<HTMLElement>(".provider-form-grid") || null;
  const providerIDInput = document.getElementById("providerIdInput") as HTMLInputElement | null;
  const protocolInput = document.getElementById("providerProtocolSelect") as HTMLSelectElement | null;
  const baseURLInput = document.getElementById("providerBaseUrlInput") as HTMLInputElement | null;
  const apiKeyInput = document.getElementById("providerApiKeyInput") as HTMLInputElement | null;
  const modelIDInput = document.getElementById("providerModelIdInput") as HTMLInputElement | null;
  const modelNameInput = document.getElementById("providerModelNameInput") as HTMLInputElement | null;
  const contextInput = document.getElementById("providerContextInput") as HTMLInputElement | null;
  const outputInput = document.getElementById("providerOutputInput") as HTMLInputElement | null;
  const toggles = form?.querySelector<HTMLElement>(".provider-toggles") || null;

  if (!providersUI || !form || !grid || !providerIDInput || !protocolInput || !baseURLInput || !apiKeyInput ||
      !modelIDInput || !modelNameInput || !contextInput || !outputInput || !toggles) return;

  const manualFields = [modelIDInput, modelNameInput, contextInput, outputInput]
    .map((input) => input.closest<HTMLElement>("label"))
    .filter((item): item is HTMLElement => !!item);

  const style = document.createElement("style");
  style.id = "tl-provider-discovery-style";
  style.textContent = `
    .provider-discovery-block{grid-column:1/-1;display:grid;gap:9px;padding:11px;border:1px solid var(--line);border-radius:10px;background:var(--panel)}
    .provider-discovery-head{display:flex;align-items:flex-start;justify-content:space-between;gap:10px}.provider-discovery-head strong{font-size:10px}.provider-discovery-head span{display:block;margin-top:2px;color:var(--muted);font-size:9px;line-height:1.45}
    .provider-discovery-actions{display:flex;align-items:center;gap:8px;flex-wrap:wrap}.provider-discovery-status{min-width:0;color:var(--muted);font-size:9px;line-height:1.45}.provider-discovery-status.error{color:var(--danger)}.provider-discovery-status.warn{color:color-mix(in srgb,var(--warning,#d9a441),var(--text) 30%)}
    .provider-discovery-capability-default{display:flex;align-items:flex-start;gap:7px;color:var(--muted);font-size:9px;line-height:1.45}.provider-discovery-capability-default input{margin-top:2px}
    .provider-model-catalog{display:grid;gap:8px}.provider-model-toolbar{display:grid;grid-template-columns:minmax(0,1fr) auto auto;gap:6px}.provider-model-toolbar input{width:100%;height:32px}.provider-model-summary{color:var(--muted);font-size:9px}
    .provider-model-list{display:grid;gap:5px;max-height:290px;overflow:auto;padding-right:2px}.provider-model-row{display:grid;grid-template-columns:auto minmax(0,1fr);gap:8px;align-items:flex-start;padding:8px;border:1px solid var(--line);border-radius:8px;background:var(--panel-2);cursor:pointer}.provider-model-row:hover{border-color:color-mix(in srgb,var(--accent),var(--line) 55%)}.provider-model-row.unavailable{opacity:.72}.provider-model-copy{min-width:0}.provider-model-name{display:flex;align-items:baseline;gap:6px;min-width:0}.provider-model-name strong{font-size:10px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.provider-model-name code{font-size:8px;color:var(--muted);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.provider-model-meta{display:flex;gap:5px;flex-wrap:wrap;margin-top:5px}.provider-model-chip{padding:2px 5px;border:1px solid var(--line);border-radius:999px;color:var(--muted);font-size:8px}.provider-model-chip.negative{color:var(--danger)}
    .provider-manual-toggle-row{display:flex;align-items:center;gap:7px;margin-top:2px;color:var(--muted);font-size:9px}.provider-manual-model-field.hidden,.provider-toggles.provider-manual-hidden{display:none!important}
    @media(max-width:760px){.provider-model-toolbar{grid-template-columns:1fr}.provider-discovery-head{display:block}.provider-discovery-actions{align-items:stretch}.provider-discovery-actions button{width:100%}}
  `;
  document.head.appendChild(style);

  const block = document.createElement("section");
  block.className = "provider-discovery-block";
  block.innerHTML = `
    <div class="provider-discovery-head">
      <div><strong>Models</strong><span>Test the provider connection and discover the model catalog before saving. Manual model IDs remain available as a fallback.</span></div>
    </div>
    <div class="provider-discovery-actions">
      <button id="providerDiscoverModelsButton" type="button" class="ghost small">Test / Discover Models</button>
      <span id="providerDiscoveryStatus" class="provider-discovery-status"></span>
    </div>
    <label class="provider-discovery-capability-default">
      <input id="providerAssumeUnknownTools" type="checkbox" checked />
      <span>Assume tool calling when the provider does not publish tool capability metadata.</span>
    </label>
    <div id="providerModelCatalog" class="provider-model-catalog hidden">
      <div class="provider-model-toolbar">
        <input id="providerModelSearch" type="search" autocomplete="off" placeholder="Search discovered models…" />
        <button id="providerSelectVisibleModels" type="button" class="ghost small">Select visible</button>
        <button id="providerClearModels" type="button" class="ghost small">Clear</button>
      </div>
      <div id="providerModelSummary" class="provider-model-summary"></div>
      <div id="providerModelList" class="provider-model-list"></div>
    </div>
    <div class="provider-manual-toggle-row">
      <button id="providerManualModelToggle" type="button" class="ghost small">Manual model entry</button>
      <span>Use this when the provider has no model-list API or the model is private/unlisted.</span>
    </div>
  `;

  const firstManualField = manualFields[0] || null;
  grid.insertBefore(block, firstManualField);

  const discoverButton = document.getElementById("providerDiscoverModelsButton") as HTMLButtonElement;
  const status = document.getElementById("providerDiscoveryStatus") as HTMLElement;
  const catalogElement = document.getElementById("providerModelCatalog") as HTMLElement;
  const searchInput = document.getElementById("providerModelSearch") as HTMLInputElement;
  const selectVisibleButton = document.getElementById("providerSelectVisibleModels") as HTMLButtonElement;
  const clearButton = document.getElementById("providerClearModels") as HTMLButtonElement;
  const summary = document.getElementById("providerModelSummary") as HTMLElement;
  const list = document.getElementById("providerModelList") as HTMLElement;
  const manualToggle = document.getElementById("providerManualModelToggle") as HTMLButtonElement;
  const assumeUnknownTools = document.getElementById("providerAssumeUnknownTools") as HTMLInputElement;

  let catalog: TLStudioDynamicRecord[] = [];
  let selectedIDs = new Set<string>();
  let manualVisible = false;
  let lastConnectionKey = "";

  const clean = (value: unknown) => String(value ?? "").trim();
  const validProviderID = (value: string) => /^[a-z0-9][a-z0-9-_]*$/.test(value);
  const boolLabel = (value: unknown, yes: string, no: string, unknown: string) =>
    value === true ? yes : value === false ? no : unknown;

  const setStatus = (message = "", kind: "" | "error" | "warn" = "") => {
    status.textContent = message;
    status.className = `provider-discovery-status${kind ? ` ${kind}` : ""}`;
  };

  const setManualVisible = (visible: boolean) => {
    manualVisible = visible;
    for (const field of manualFields) {
      field.classList.add("provider-manual-model-field");
      field.classList.toggle("hidden", !visible);
    }
    toggles.classList.toggle("provider-manual-hidden", !visible);
    manualToggle.textContent = visible ? "Hide manual model entry" : "Manual model entry";
  };

  const formatTokenLimit = (value: unknown, suffix: string) => {
    const number = Number(value || 0);
    if (!Number.isFinite(number) || number <= 0) return "";
    if (number >= 1_000_000) return `${(number / 1_000_000).toFixed(number % 1_000_000 === 0 ? 0 : 1)}M ${suffix}`;
    if (number >= 1_000) return `${Math.round(number / 1_000)}K ${suffix}`;
    return `${number} ${suffix}`;
  };

  const normalizeExistingModel = (model: TLStudioDynamicRecord) => ({
    id: clean(model?.id),
    name: clean(model?.name) || clean(model?.id),
    ...(clean(model?.kind) ? { kind: clean(model.kind) } : {}),
    toolCall: model?.toolCall !== false,
    reasoning: model?.reasoning === true,
    ...(Number(model?.contextLimit) > 0 ? { contextLimit: Number(model.contextLimit) } : {}),
    ...(Number(model?.outputLimit) > 0 ? { outputLimit: Number(model.outputLimit) } : {}),
    configured: true,
    unavailable: true,
  });

  const mergeCatalog = (discovered: TLStudioDynamicRecord[], existingModels: TLStudioDynamicRecord[]) => {
    const byID = new Map<string, TLStudioDynamicRecord>();
    for (const raw of discovered) {
      const id = clean(raw?.id);
      if (!id) continue;
      byID.set(id, { ...raw, id, name: clean(raw?.name) || id, configured: false, unavailable: false });
    }
    for (const raw of existingModels) {
      const existing = normalizeExistingModel(raw);
      if (!existing.id) continue;
      const live = byID.get(existing.id);
      if (live) {
        byID.set(existing.id, {
          ...existing,
          ...live,
          configured: true,
          unavailable: false,
          toolCall: live.toolCall ?? existing.toolCall,
          reasoning: live.reasoning ?? existing.reasoning,
          contextLimit: Number(live.contextLimit || existing.contextLimit || 0) || undefined,
          outputLimit: Number(live.outputLimit || existing.outputLimit || 0) || undefined,
        });
      } else {
        byID.set(existing.id, existing);
      }
    }
    return [...byID.values()].sort((a, b) =>
      String(a.name || a.id).localeCompare(String(b.name || b.id), undefined, { sensitivity: "base" }));
  };

  const visibleModels = () => {
    const query = clean(searchInput.value).toLowerCase();
    return catalog.filter((model) => {
      if (!query) return true;
      return clean(model.name).toLowerCase().includes(query) || clean(model.id).toLowerCase().includes(query);
    });
  };

  const render = () => {
    list.textContent = "";
    const matches = visibleModels();
    const shown = matches.slice(0, 100);
    summary.textContent = `${selectedIDs.size} selected · ${catalog.length} known · showing ${shown.length}${matches.length > shown.length ? ` of ${matches.length} matches` : ""}`;

    if (!shown.length) {
      const empty = document.createElement("div");
      empty.className = "provider-empty";
      empty.textContent = catalog.length ? "No models match this search." : "No models discovered.";
      list.appendChild(empty);
      return;
    }

    for (const model of shown) {
      const row = document.createElement("label");
      row.className = `provider-model-row${model.unavailable ? " unavailable" : ""}`;

      const checkbox = document.createElement("input");
      checkbox.type = "checkbox";
      checkbox.checked = selectedIDs.has(model.id);
      checkbox.dataset.modelId = model.id;
      checkbox.addEventListener("change", () => {
        if (checkbox.checked) selectedIDs.add(model.id);
        else selectedIDs.delete(model.id);
        summary.textContent = `${selectedIDs.size} selected · ${catalog.length} known · showing ${shown.length}${matches.length > shown.length ? ` of ${matches.length} matches` : ""}`;
      });

      const copy = document.createElement("div");
      copy.className = "provider-model-copy";
      const title = document.createElement("div");
      title.className = "provider-model-name";
      const strong = document.createElement("strong");
      strong.textContent = model.name || model.id;
      const code = document.createElement("code");
      code.textContent = model.id;
      title.append(strong, code);

      const meta = document.createElement("div");
      meta.className = "provider-model-meta";
      const chips = [
        model.unavailable ? "Not returned now" : "",
        model.kind === "router" ? "Router" : "",
        formatTokenLimit(model.contextLimit, "ctx"),
        formatTokenLimit(model.outputLimit, "out"),
        boolLabel(model.toolCall, "Tools", "No tools", "Tools ?"),
        model.reasoning === true ? "Reasoning" : model.reasoning === false ? "No reasoning" : "",
        model.vision === true ? "Vision" : model.vision === false ? "No vision" : "",
      ].filter(Boolean);
      for (const label of chips) {
        const chip = document.createElement("span");
        chip.className = `provider-model-chip${label === "Not returned now" || label === "No tools" ? " negative" : ""}`;
        chip.textContent = label;
        meta.appendChild(chip);
      }
      copy.append(title, meta);
      row.append(checkbox, copy);
      list.appendChild(row);
    }
  };

  const currentExistingProvider = async () => {
    const id = clean(providerIDInput.value);
    if (!validProviderID(id)) return null;
    try {
      const config = await K.api.providers.config();
      return Array.isArray(config?.providers)
        ? config.providers.find((provider: TLStudioDynamicRecord) => provider?.id === id) || null
        : null;
    } catch {
      return null;
    }
  };

  const reset = () => {
    catalog = [];
    selectedIDs = new Set();
    searchInput.value = "";
    catalogElement.classList.add("hidden");
    discoverButton.textContent = "Test / Discover Models";
    discoverButton.disabled = false;
    lastConnectionKey = "";
    assumeUnknownTools.checked = true;
    setStatus();
    setManualVisible(false);
  };

  const modelsForSave = () => {
    if (!catalog.length || !selectedIDs.size) return [];
    return catalog
      .filter((model) => selectedIDs.has(model.id))
      .map((model) => ({
        id: model.id,
        name: model.name || model.id,
        ...(clean(model.kind) ? { kind: clean(model.kind) } : {}),
        toolCall: typeof model.toolCall === "boolean" ? model.toolCall : assumeUnknownTools.checked,
        reasoning: model.reasoning === true,
        ...(Number(model.contextLimit) > 0 ? { contextLimit: Number(model.contextLimit) } : {}),
        ...(Number(model.outputLimit) > 0 ? { outputLimit: Number(model.outputLimit) } : {}),
      }));
  };

  providersUI.discoverySelection = {
    modelsForSave,
    reset,
    setAssumeUnknownTools: (value: boolean) => { assumeUnknownTools.checked = value; },
  };

  const discover = async (preferredModelID = "") => {
    const protocol = clean(protocolInput.value);
    const baseURL = clean(baseURLInput.value);
    if (!baseURL) {
      setStatus("Enter a Base URL first.", "error");
      return false;
    }
    try {
      const parsed = new URL(baseURL);
      if (!/^https?:$/.test(parsed.protocol)) throw new Error();
    } catch {
      setStatus("Enter a valid http(s) Base URL first.", "error");
      return false;
    }

    const providerID = clean(providerIDInput.value);
    const requestProviderID = validProviderID(providerID) ? providerID : undefined;
    const key = `${protocol}\u0000${baseURL}`;
    discoverButton.disabled = true;
    discoverButton.textContent = catalog.length ? "Refreshing…" : "Discovering…";
    setStatus("Connecting to the provider and requesting its model catalog…");
    try {
      const [result, existing] = await Promise.all([
        K.api.providers.discover({
          ...(requestProviderID ? { providerID: requestProviderID } : {}),
          protocol,
          baseURL,
          apiKey: clean(apiKeyInput.value),
        }),
        currentExistingProvider(),
      ]);
      const discovered = Array.isArray(result?.models) ? result.models : [];
      const existingModels = Array.isArray(existing?.models) ? existing.models : [];
      catalog = mergeCatalog(discovered, existingModels);
      selectedIDs = new Set(existingModels.map((model: TLStudioDynamicRecord) => clean(model?.id)).filter(Boolean));
      if (preferredModelID && catalog.some((model) => model.id === preferredModelID)) selectedIDs.add(preferredModelID);
      if (!existingModels.length && catalog.length === 1) selectedIDs.add(catalog[0].id);
      searchInput.value = "";
      lastConnectionKey = key;
      catalogElement.classList.remove("hidden");
      render();
      const stale = result?.stale === true || result?.source === "cache";
      const timestamp = clean(result?.fetchedAt);
      const detail = stale
        ? `Showing the last successful catalog${timestamp ? ` from ${timestamp}` : ""}.`
        : `Discovered ${discovered.length} model${discovered.length === 1 ? "" : "s"}.`;
      const preferredFound = !preferredModelID || selectedIDs.has(preferredModelID);
      setStatus(preferredModelID && !preferredFound
        ? `${detail} Requested model ${preferredModelID} was not returned by the provider.`
        : (result?.warning ? `${detail} ${result.warning}` : detail),
        preferredModelID && !preferredFound ? "warn" : (stale ? "warn" : ""));
      discoverButton.textContent = "Refresh Models";
      return preferredFound;
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      setStatus(message || "Model discovery failed.", "error");
      if (!catalog.length) catalogElement.classList.add("hidden");
      discoverButton.textContent = catalog.length ? "Refresh Models" : "Test / Discover Models";
      return false;
    } finally {
      discoverButton.disabled = false;
    }
  };

  providersUI.discoverySelection.discoverModel = (modelID: string) => discover(clean(modelID));
  discoverButton.addEventListener("click", () => { void discover(); });
  searchInput.addEventListener("input", render);
  selectVisibleButton.addEventListener("click", () => {
    for (const model of visibleModels().slice(0, 100)) selectedIDs.add(model.id);
    render();
  });
  clearButton.addEventListener("click", () => {
    selectedIDs.clear();
    render();
  });
  manualToggle.addEventListener("click", () => setManualVisible(!manualVisible));

  const markConnectionChanged = () => {
    if (!catalog.length) return;
    const key = `${clean(protocolInput.value)}\u0000${clean(baseURLInput.value)}`;
    if (lastConnectionKey && key !== lastConnectionKey) {
      setStatus("Connection settings changed. Run model discovery again before saving this catalog.", "warn");
      catalog = [];
      selectedIDs.clear();
      catalogElement.classList.add("hidden");
      discoverButton.textContent = "Test / Discover Models";
    }
  };
  protocolInput.addEventListener("change", markConnectionChanged);
  baseURLInput.addEventListener("input", markConnectionChanged);

  setManualVisible(false);
})();
