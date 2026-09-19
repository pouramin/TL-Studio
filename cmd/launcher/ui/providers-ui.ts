(() => {
  "use strict";

  const K = window.KLU;
  if (!K || K.__providersUiInstalled) return;
  K.__providersUiInstalled = true;

  const PROTOCOLS = new Set(["openai-compatible", "openai-responses", "anthropic-messages"]);
  const PROVIDER_ID = /^[a-z0-9][a-z0-9-_]*$/;

  const clean = (value) => String(value ?? "").trim();
  const positiveInt = (value) => {
    const raw = clean(value);
    if (!raw) return undefined;
    const number = Number(raw);
    return Number.isInteger(number) && number > 0 ? number : NaN;
  };
  const safeURL = (value) => {
    try {
      const url = new URL(clean(value));
      if (!/^https?:$/.test(url.protocol)) return "";
      return url.toString().replace(/\/$/, "");
    } catch { return ""; }
  };

  const validateDraft = (draft) => {
    const id = clean(draft.providerID);
    if (!PROVIDER_ID.test(id)) return "Provider ID must use lowercase letters, numbers, dashes, or underscores.";
    if (!clean(draft.name)) return "Display name is required.";
    if (!PROTOCOLS.has(draft.protocol)) return "Choose a supported provider API.";
    if (!safeURL(draft.baseURL)) return "Enter a valid http(s) Base URL.";
    if (!clean(draft.modelID)) return "Model ID is required.";
    const context = positiveInt(draft.contextLimit);
    const output = positiveInt(draft.outputLimit);
    if (Number.isNaN(context)) return "Context limit must be a positive whole number.";
    if (Number.isNaN(output)) return "Max output must be a positive whole number.";
    return "";
  };

  const buildProviderDefinition = (draft, existing = {}) => {
    const modelID = clean(draft.modelID);
    const context = positiveInt(draft.contextLimit);
    const output = positiveInt(draft.outputLimit);
    const previousModels = Array.isArray(existing?.models) ? existing.models.filter((model) => model?.id && model.id !== modelID) : [];
    const model = {
      id: modelID,
      name: clean(draft.modelName) || modelID,
      toolCall: draft.toolCall !== false,
      reasoning: draft.reasoning === true,
      ...(context ? { contextLimit: context } : {}),
      ...(output ? { outputLimit: output } : {}),
    };
    return {
      id: clean(draft.providerID),
      name: clean(draft.name),
      protocol: draft.protocol,
      baseURL: safeURL(draft.baseURL),
      models: [...previousModels, model].sort((a, b) => String(a.id).localeCompare(String(b.id))),
    };
  };

  const customProviderEntries = (config) => Array.isArray(config?.providers)
    ? [...config.providers].sort((a, b) => String(a?.name || a?.id || "").localeCompare(String(b?.name || b?.id || "")))
    : [];

  const withoutProvider = (config, providerID) => ({
    ...(config && typeof config === "object" ? config : {}),
    providers: Array.isArray(config?.providers)
      ? config.providers.filter((provider) => provider?.id !== providerID)
      : [],
  });

  K.__providersUi = {
    PROTOCOLS,
    validateDraft,
    buildProviderDefinition,
    customProviderEntries,
    withoutProvider,
  };

  const settingsDialog = document.getElementById("settingsDialog");
  const settingsNav = settingsDialog?.querySelector(".settings-nav");
  const settingsContent = settingsDialog?.querySelector(".settings-content");
  if (!settingsDialog || !settingsNav || !settingsContent) return;

  const navButton = document.createElement("button");
  navButton.className = "settings-nav-item";
  navButton.type = "button";
  navButton.dataset.settingsSection = "providers";
  navButton.innerHTML = '<span class="settings-nav-icon" aria-hidden="true">◇</span><span>Providers</span>';
  const aboutButton = settingsNav.querySelector('[data-settings-section="about"]');
  settingsNav.insertBefore(navButton, aboutButton || null);

  const panel = document.createElement("section");
  panel.className = "settings-panel hidden providers-settings-panel";
  panel.dataset.settingsPanel = "providers";
  panel.innerHTML = `
    <div class="settings-panel-head providers-panel-head">
      <div>
        <h3>Providers</h3>
        <p>Add OpenAI-compatible, OpenAI Responses, or Anthropic-compatible endpoints to TL Studio. Models saved here appear in TL Studio's model selector.</p>
      </div>
      <button id="providerAddButton" class="primary provider-add-button" type="button">Add provider</button>
    </div>
    <div id="providerNotice" class="provider-notice hidden" role="status"></div>
    <div id="providerList" class="provider-list"></div>
    <form id="providerForm" class="provider-form hidden">
      <div class="provider-form-head">
        <div><strong id="providerFormTitle">Add provider</strong><span>Configuration is saved globally by TL Studio and is available across projects.</span></div>
        <button id="providerFormCancelTop" class="icon-button" type="button" aria-label="Close provider form">×</button>
      </div>
      <div class="provider-form-grid">
        <label><span>Provider ID</span><input id="providerIdInput" autocomplete="off" spellcheck="false" placeholder="my-provider" /></label>
        <label><span>Display name</span><input id="providerNameInput" autocomplete="off" placeholder="My Provider" /></label>
        <label><span>Provider API</span><select id="providerProtocolSelect"><option value="openai-compatible">OpenAI Compatible</option><option value="openai-responses">OpenAI Responses</option><option value="anthropic-messages">Anthropic Messages</option></select></label>
        <label class="provider-field-wide"><span>Base URL</span><input id="providerBaseUrlInput" autocomplete="off" spellcheck="false" placeholder="https://api.example.com/v1" /></label>
        <label class="provider-field-wide"><span>API key</span><input id="providerApiKeyInput" type="password" autocomplete="new-password" spellcheck="false" placeholder="Leave blank to keep an existing key" /></label>
        <label><span>Model ID</span><input id="providerModelIdInput" autocomplete="off" spellcheck="false" placeholder="model-id" /></label>
        <label><span>Model name</span><input id="providerModelNameInput" autocomplete="off" placeholder="Model name" /></label>
        <label><span>Context limit</span><input id="providerContextInput" inputmode="numeric" autocomplete="off" placeholder="Optional" /></label>
        <label><span>Max output</span><input id="providerOutputInput" inputmode="numeric" autocomplete="off" placeholder="Optional" /></label>
      </div>
      <div class="provider-toggles">
        <label><input id="providerToolCallInput" type="checkbox" checked /> <span>Tool calling</span></label>
        <label><input id="providerReasoningInput" type="checkbox" /> <span>Reasoning</span></label>
      </div>
      <div class="provider-security-note">TL Studio keeps API keys out of its provider config. Credentials are currently delegated to the local runtime credential store and are never saved in browser storage.</div>
      <div class="provider-limit-note">For custom models, set context/output limits when you know them. Automatic context compaction may be unavailable when a model has no known context limit.</div>
      <div class="dialog-actions provider-form-actions"><button id="providerFormCancel" class="ghost" type="button">Cancel</button><button id="providerFormSave" class="primary" type="submit">Save provider</button></div>
    </form>
  `;
  const aboutPanel = settingsContent.querySelector('[data-settings-panel="about"]');
  settingsContent.insertBefore(panel, aboutPanel || null);

  const style = document.createElement("style");
  style.id = "tl-providers-ui-style";
  style.textContent = `
    .providers-panel-head{display:flex;align-items:flex-start;justify-content:space-between;gap:16px}.provider-add-button{flex:none}
    .provider-notice{margin:-8px 0 14px;padding:9px 10px;border:1px solid var(--line);border-radius:8px;background:var(--panel-2);font-size:10px;line-height:1.45}.provider-notice.error{border-color:color-mix(in srgb,var(--danger),var(--line) 55%);color:var(--danger)}
    .provider-list{display:grid;gap:8px}.provider-empty{padding:24px 12px;border:1px dashed var(--line);border-radius:10px;color:var(--muted);font-size:10px;text-align:center}.provider-item{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:12px;align-items:center;padding:11px 12px;border:1px solid var(--line);border-radius:10px;background:var(--panel)}.provider-item-title{display:flex;align-items:center;gap:7px}.provider-item-title strong{font-size:11px}.provider-status-dot{width:7px;height:7px;border-radius:50%;background:var(--muted-2)}.provider-status-dot.ok{background:var(--accent)}.provider-item-meta{margin-top:4px;color:var(--muted);font-size:9px;line-height:1.45}.provider-item-actions{display:flex;gap:6px}.provider-delete{color:var(--danger)}
    .provider-form{margin-top:14px;padding:14px;border:1px solid var(--line);border-radius:11px;background:var(--panel-2)}.provider-form-head{display:flex;align-items:flex-start;justify-content:space-between;gap:12px;margin-bottom:13px}.provider-form-head strong,.provider-form-head span{display:block}.provider-form-head strong{font-size:12px}.provider-form-head span{margin-top:3px;color:var(--muted);font-size:9px}.provider-form-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px}.provider-form-grid label>span{display:block;margin:0 0 5px;color:var(--muted);font-size:9px;font-weight:650}.provider-form-grid input,.provider-form-grid select{box-sizing:border-box;width:100%;height:34px}.provider-field-wide{grid-column:1/-1}.provider-toggles{display:flex;gap:18px;margin-top:12px;color:var(--text);font-size:10px}.provider-toggles label{display:flex;align-items:center;gap:5px}.provider-security-note,.provider-limit-note{margin-top:11px;color:var(--muted);font-size:9px;line-height:1.5}.provider-security-note{color:color-mix(in srgb,var(--accent),var(--text) 45%)}.provider-form-actions{padding:13px 0 0}.providers-settings-panel.busy{opacity:.72;pointer-events:none}
    @media(max-width:760px){.providers-panel-head{display:block}.provider-add-button{margin-top:10px}.provider-form-grid{grid-template-columns:1fr}.provider-field-wide{grid-column:auto}.provider-item{grid-template-columns:1fr}.provider-item-actions{justify-content:flex-end}}
  `;
  document.head.appendChild(style);

  const $ = (id) => document.getElementById(id);
  const els = {
    add: $("providerAddButton"), notice: $("providerNotice"), list: $("providerList"), form: $("providerForm"),
    title: $("providerFormTitle"), cancel: $("providerFormCancel"), cancelTop: $("providerFormCancelTop"), save: $("providerFormSave"),
    id: $("providerIdInput"), name: $("providerNameInput"), protocol: $("providerProtocolSelect"), baseURL: $("providerBaseUrlInput"), apiKey: $("providerApiKeyInput"),
    modelID: $("providerModelIdInput"), modelName: $("providerModelNameInput"), context: $("providerContextInput"), output: $("providerOutputInput"),
    toolCall: $("providerToolCallInput"), reasoning: $("providerReasoningInput"),
  };

  let providerConfig = { providers: [] };
  let editingID = "";
  let saving = false;

  const setBusy = (value) => {
    saving = value;
    panel.classList.toggle("busy", value);
    if (els.save) els.save.disabled = value;
  };
  const notice = (message = "", error = false) => {
    els.notice.textContent = message;
    els.notice.classList.toggle("hidden", !message);
    els.notice.classList.toggle("error", !!message && error);
  };
  const activate = () => {
    if (typeof K.activateSettingsSection === "function") {
      K.activateSettingsSection("providers");
      return;
    }
    for (const button of settingsDialog.querySelectorAll("[data-settings-section]")) {
      const active = button.dataset.settingsSection === "providers";
      button.classList.toggle("active", active);
      if (active) button.setAttribute("aria-current", "page"); else button.removeAttribute("aria-current");
    }
    for (const item of settingsDialog.querySelectorAll("[data-settings-panel]")) item.classList.toggle("hidden", item.dataset.settingsPanel !== "providers");
  };

  const draft = () => ({
    providerID: els.id.value,
    name: els.name.value,
    protocol: els.protocol.value,
    baseURL: els.baseURL.value,
    apiKey: els.apiKey.value,
    modelID: els.modelID.value,
    modelName: els.modelName.value,
    contextLimit: els.context.value,
    outputLimit: els.output.value,
    toolCall: els.toolCall.checked,
    reasoning: els.reasoning.checked,
  });

  const clearForm = () => {
    editingID = "";
    els.form.reset();
    els.protocol.value = "openai-compatible";
    els.toolCall.checked = true;
    els.reasoning.checked = false;
    els.id.disabled = false;
    els.title.textContent = "Add provider";
    els.form.classList.add("hidden");
  };

  // Provider editing is scoped to the Providers settings section. Leaving that
  // section must discard the transient add/edit form so returning always opens
  // a clean provider list rather than restoring stale edit state.
  const resetTransientForm = () => {
    notice("");
    clearForm();
  };
  K.__providersUi.resetTransientForm = resetTransientForm;

  const fillForm = (value, existingID = "") => {
    editingID = existingID;
    els.id.value = value.providerID || "";
    els.name.value = value.name || "";
    els.protocol.value = value.protocol || "openai-compatible";
    els.baseURL.value = value.baseURL || "";
    els.apiKey.value = "";
    els.modelID.value = value.modelID || "";
    els.modelName.value = value.modelName || "";
    els.context.value = value.contextLimit || "";
    els.output.value = value.outputLimit || "";
    els.toolCall.checked = value.toolCall !== false;
    els.reasoning.checked = value.reasoning === true;
    els.id.disabled = !!existingID;
    els.title.textContent = existingID ? `Edit ${value.name || existingID}` : "Add provider";
    els.form.classList.remove("hidden");
    els.form.scrollIntoView?.({ block: "nearest" });
  };

  const modelCount = (provider) => Array.isArray(provider?.models) ? provider.models.length : 0;
  const renderList = () => {
    els.list.textContent = "";
    const entries = customProviderEntries(providerConfig);
    if (!entries.length) {
      const empty = document.createElement("div");
      empty.className = "provider-empty";
      empty.textContent = "No custom providers yet. Add any compatible endpoint to get started.";
      els.list.appendChild(empty);
      return;
    }
    for (const entry of entries) {
      const row = document.createElement("div");
      row.className = "provider-item";
      const copy = document.createElement("div");
      const title = document.createElement("div");
      title.className = "provider-item-title";
      const dot = document.createElement("span");
      const loaded = K.state.providers.some((provider) => provider?.id === entry.id);
      dot.className = `provider-status-dot${loaded ? " ok" : ""}`;
      const strong = document.createElement("strong");
      strong.textContent = entry.name || entry.id;
      title.append(dot, strong);
      const meta = document.createElement("div");
      meta.className = "provider-item-meta";
      const protocol = entry.protocol;
      const keyed = K.state.connectedProviders.has(entry.id);
      meta.textContent = `${entry.id} · ${protocol} · ${modelCount(entry)} model${modelCount(entry) === 1 ? "" : "s"} · ${keyed ? "credentials connected" : "no stored key"}`;
      copy.append(title, meta);

      const actions = document.createElement("div");
      actions.className = "provider-item-actions";
      const edit = document.createElement("button");
      edit.type = "button";
      edit.className = "ghost small";
      edit.textContent = "Edit";
      edit.addEventListener("click", () => editEntry(entry));
      const remove = document.createElement("button");
      remove.type = "button";
      remove.className = "ghost small provider-delete";
      remove.textContent = "Delete";
      remove.addEventListener("click", () => deleteEntry(entry));
      actions.append(edit, remove);
      row.append(copy, actions);
      els.list.appendChild(row);
    }
  };

  const load = async () => {
    notice("");
    try {
      providerConfig = await K.api.providers.config();
      renderList();
    } catch (error) {
      notice(`Could not load provider settings: ${error.message || String(error)}`, true);
    }
  };

  const editEntry = (entry) => {
    const first = Array.isArray(entry?.models) && entry.models.length ? entry.models[0] : {};
    fillForm({
      providerID: entry.id,
      name: entry.name || entry.id,
      protocol: entry.protocol || "openai-compatible",
      baseURL: entry.baseURL || "",
      modelID: first.id || "",
      modelName: first.name || first.id || "",
      contextLimit: first.contextLimit || "",
      outputLimit: first.outputLimit || "",
      toolCall: first.toolCall !== false,
      reasoning: first.reasoning === true,
    }, entry.id);
  };

  const save = async (event) => {
    event?.preventDefault();
    if (saving) return;
    const value = draft();
    const error = validateDraft(value);
    if (error) return notice(error, true);

    setBusy(true);
    notice("");
    try {
      const id = clean(value.providerID);
      const existing = customProviderEntries(providerConfig).find((provider) => provider.id === (editingID || id)) || {};
      const provider = buildProviderDefinition(value, existing);
      await K.api.providers.upsert(id, {
        provider,
        ...(clean(value.apiKey) ? { apiKey: clean(value.apiKey) } : {}),
      });
      await K.loadCatalog();
      providerConfig = await K.api.providers.config();
      renderList();
      clearForm();
      const loaded = K.state.models.some((model) => model.providerID === id && model.id === clean(value.modelID));
      notice(loaded
        ? `${value.name} saved. ${clean(value.modelID)} is now available in the model selector.`
        : `${value.name} was saved by TL Studio, but the active runtime did not load ${clean(value.modelID)}. Check the endpoint, protocol, and model ID.`, !loaded);
    } catch (err) {
      notice(`Could not save provider: ${err.message || String(err)}`, true);
      try { providerConfig = await K.api.providers.config(); renderList(); } catch {}
    } finally {
      setBusy(false);
    }
  };

  const deleteEntry = async (entry) => {
    if (saving) return;
    if (!window.confirm(`Delete provider “${entry.name || entry.id}”? Stored credentials for this provider will also be removed.`)) return;
    setBusy(true);
    notice("");
    try {
      await K.api.providers.remove(entry.id);

      // The delete has already succeeded in TL Studio's provider registry.
      // Update the visible state immediately instead of waiting on a runtime
      // catalog refresh, which can briefly fail while the runtime reloads.
      providerConfig = withoutProvider(providerConfig, entry.id);
      K.state.providers = Array.isArray(K.state.providers)
        ? K.state.providers.filter((provider) => provider?.id !== entry.id)
        : [];
      K.state.models = Array.isArray(K.state.models)
        ? K.state.models.filter((model) => model?.providerID !== entry.id)
        : [];
      K.state.connectedProviders?.delete?.(entry.id);
      if (K.state.providerDefaults && typeof K.state.providerDefaults === "object") {
        delete K.state.providerDefaults[entry.id];
      }
      renderList();
      K.renderModels?.();
      clearForm();
      notice(`${entry.name || entry.id} removed.`);

      try {
        providerConfig = await K.api.providers.config();
        renderList();
      } catch (error) {
        console.warn("[TL Studio] Provider registry refresh after delete failed", error);
      }
      try {
        await K.loadCatalog();
      } catch (error) {
        console.warn("[TL Studio] Runtime catalog refresh after provider delete failed", error);
      }
    } catch (error) {
      notice(`Could not delete provider: ${error.message || String(error)}`, true);
    } finally {
      setBusy(false);
    }
  };

  navButton.addEventListener("click", () => {
    resetTransientForm();
    activate();
    load();
  });
  for (const button of settingsDialog.querySelectorAll("[data-settings-section]")) {
    if (button.dataset.settingsSection === "providers") continue;
    button.addEventListener("click", resetTransientForm);
  }
  settingsDialog.addEventListener("close", resetTransientForm);

  els.add.addEventListener("click", () => { notice(""); fillForm({ toolCall: true }); });
  els.cancel.addEventListener("click", clearForm);
  els.cancelTop.addEventListener("click", clearForm);
  els.form.addEventListener("submit", save);
})();