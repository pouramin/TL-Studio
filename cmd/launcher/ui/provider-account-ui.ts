import { K } from "./kernel";

(() => {
  "use strict";

  if (!K || K.__providerAccountsUiInstalled) return;
  K.__providerAccountsUiInstalled = true;

  const clean = (value: any) => String(value ?? "").trim();
  const settingsDialog = document.getElementById("settingsDialog") as HTMLDialogElement | null;
  const panel = settingsDialog?.querySelector<HTMLElement>('[data-settings-panel="providers"]');
  const providerList = document.getElementById("providerList");
  if (!settingsDialog || !panel || !providerList) return;

  const section = document.createElement("section");
  section.className = "provider-account-section";
  section.innerHTML = `
    <div class="provider-subsection-head">
      <div>
        <strong>Providers</strong>
        <span>Sign in where supported, or configure the provider API key.</span>
      </div>
    </div>
    <div id="providerAccountList" class="provider-account-grid"></div>
    <div class="provider-section-divider"><span>Custom providers</span></div>
  `;
  providerList.before(section);

  const setupDialog = document.createElement("dialog");
  setupDialog.id = "providerAccountSetupDialog";
  setupDialog.innerHTML = `
    <form id="providerAccountSetupForm" class="dialog-card provider-account-setup-dialog">
      <div class="provider-dialog-head">
        <div>
          <h2 id="providerAccountSetupTitle">Provider setup</h2>
          <p id="providerAccountSetupDescription"></p>
        </div>
        <button id="providerAccountSetupClose" class="icon-button" type="button" aria-label="Close provider setup">×</button>
      </div>
      <div id="providerAccountSetupNotice" class="provider-notice hidden" role="status"></div>
      <div id="providerAccountSetupFields" class="provider-account-setup-fields"></div>
      <div class="provider-security-note">These fields contain non-secret provider setup only. OAuth tokens and API credentials stay in the TL Studio credential vault.</div>
      <div class="dialog-actions provider-form-actions">
        <button id="providerAccountSetupCancel" class="ghost" type="button">Cancel</button>
        <button id="providerAccountSetupSave" class="primary" type="submit">Save setup</button>
      </div>
    </form>
  `;
  document.body.appendChild(setupDialog);

  const style = document.createElement("style");
  style.id = "tl-provider-accounts-ui-style";
  style.textContent = `
    .provider-account-section{display:grid;gap:12px;margin:0 0 14px}
    .provider-subsection-head{display:flex;align-items:flex-start;justify-content:space-between;gap:12px;padding:0 2px}
    .provider-subsection-head strong,.provider-subsection-head span{display:block}
    .provider-subsection-head strong{font-size:var(--tl-ui-md);font-weight:760}
    .provider-subsection-head span{margin-top:4px;color:var(--muted);font-size:var(--tl-ui-xs);line-height:1.45}
    .provider-account-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px}
    .provider-account-card{position:relative;display:grid;min-width:0;min-height:132px;grid-template-columns:44px minmax(0,1fr);grid-template-areas:"logo name" "logo state" "details details" "actions actions";grid-template-rows:auto auto minmax(12px,1fr) auto;column-gap:10px;row-gap:4px;padding:12px;border:1px solid var(--line);border-radius:13px;background:color-mix(in srgb,var(--panel),var(--panel-2) 18%);text-align:left;transition:border-color .15s ease,background .15s ease,transform .15s ease}
    .provider-account-card:hover{border-color:color-mix(in srgb,var(--muted-2),var(--line) 60%);background:var(--panel-2)}
    .provider-account-card.provider-account-unavailable{opacity:.72}
    .provider-account-card.provider-account-unavailable .provider-account-card-actions{opacity:.72}
    .provider-account-card.provider-account-connected{border-color:color-mix(in srgb,var(--accent) 45%,var(--line));background:color-mix(in srgb,var(--panel),var(--accent) 3%)}
    .provider-account-logo{grid-area:logo;display:grid;width:42px;height:42px;place-items:center;align-self:start;border:1px solid color-mix(in srgb,var(--line) 72%,transparent);border-radius:12px;background:rgba(255,255,255,.035);color:var(--text);overflow:hidden;box-shadow:0 5px 14px rgba(0,0,0,.1)}
    .provider-account-logo svg{display:block;width:27px;height:27px}
    .provider-account-logo[data-provider="chatgpt"]{background:#111827;color:#fff}
    .provider-account-logo[data-provider="claude"],.provider-account-logo[data-provider="claude-web"]{background:#D97757;color:#FFF8F0}
    .provider-account-logo[data-provider="gemini"]{background:linear-gradient(135deg,rgba(66,133,244,.18),rgba(142,117,178,.22) 52%,rgba(217,101,167,.18));color:#fff}
    .provider-account-logo[data-provider="github-copilot"]{background:linear-gradient(135deg,#24292f,#8250df);color:#fff}
    .provider-account-logo[data-provider="huggingface"]{background:#FFD21E;color:#111827}
    .provider-account-logo[data-provider="openrouter"]{background:#94A3B8;color:#111827}
    .provider-account-logo-fallback{font-size:15px;font-weight:800;line-height:1}
    .provider-account-name{grid-area:name;min-width:0;padding-right:24px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:var(--tl-ui-base);font-weight:760;line-height:1.25}
    .provider-account-state{grid-area:state;display:flex;min-width:0;align-items:center;gap:6px;color:var(--muted);font-size:var(--tl-ui-xs);line-height:1.25}
    .provider-account-state>span:last-child{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
    .provider-account-state .provider-status-dot{width:6px;height:6px;flex:none}
    .provider-account-details{grid-area:details;min-width:0;align-self:end;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--muted-2);font-size:var(--tl-ui-xs);line-height:1.35}
    .provider-account-details:empty{display:none}
    .provider-account-card-actions{grid-area:actions;display:grid;width:100%;grid-template-columns:repeat(auto-fit,minmax(64px,1fr));gap:6px;margin-top:5px}
    .provider-account-card-actions .primary,.provider-account-card-actions .ghost{width:100%;min-width:0;padding-left:8px;padding-right:8px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
    .provider-account-mode-active{border-color:color-mix(in srgb,var(--accent) 50%,var(--line))!important;background:color-mix(in srgb,var(--accent),transparent 91%)!important;color:var(--text)!important}
    .provider-account-mode-active:hover{border-color:color-mix(in srgb,var(--danger) 46%,var(--line))!important;color:var(--danger)!important}
    .provider-account-signin{min-width:0}
    .provider-account-setup-button{position:absolute;top:8px;right:8px;width:26px;height:26px;padding:0;border-radius:8px;font-size:12px;line-height:1}
    .provider-account-setup-dialog{width:min(620px,calc(100vw - 36px));padding:20px}
    .provider-account-setup-fields{display:grid;gap:10px}.provider-account-setup-field>span{display:block;margin-bottom:5px;color:var(--muted);font-size:var(--tl-ui-xs);font-weight:650}.provider-account-setup-field input{box-sizing:border-box;width:100%;height:34px}.provider-account-setup-field small{display:block;margin-top:5px;color:var(--muted);font-size:var(--tl-ui-xs);line-height:1.45}
    .provider-section-divider{display:flex;align-items:center;gap:10px;margin:5px 0 2px;color:var(--muted-2);font-size:var(--tl-ui-xs);font-weight:750;letter-spacing:.08em;text-transform:uppercase}
    .provider-section-divider::before,.provider-section-divider::after{content:"";height:1px;background:var(--line);flex:1}
    @media(max-width:980px){.provider-account-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}
    @media(max-width:680px){.provider-account-grid{grid-template-columns:1fr}.provider-account-card{min-height:124px}}
  `;
  document.head.appendChild(style);

  const accountList = document.getElementById("providerAccountList")!;
  let loading = false;

  const accountLoginProviderIDs = new Set(["chatgpt", "claude", "claude-web", "github-copilot"]);
  const accountRuntimeProviderIDs: Record<string, string> = {
    chatgpt: "chatgpt",
    claude: "claude-account",
    "claude-web": "claude-web-account",
    "github-copilot": "github-copilot",
  };

  const CLAUDE_WEB_EXTENSION_IDS = [
    "cpellhbmfdhcgkblnmnppndmeiigmjcg",
    "hklkkfhbcohbfpojbcanhgmfanjhnfna",
  ];
  let claudeWebExtensionID = "";
  const CLAUDE_WEB_BRIDGE_VERSION = "0.6.3-persistent-page";
  let claudeWebRelayController: AbortController | null = null;
  let claudeWebRelayToken = "";
  let claudeWebResumePromise: Promise<void> | null = null;
  const CLAUDE_WEB_MODEL_ID = "claude-sonnet-5-5";
  const providerCardOrder = ["chatgpt", "claude", "gemini", "github-copilot", "huggingface", "openrouter"];
  const apiProviderPresets: Record<string, TLStudioDynamicRecord> = {
    claude: {
      providerID: "claude",
      name: "Claude",
      protocol: "anthropic-messages",
      baseURL: "https://api.anthropic.com/v1",
      toolCall: true,
      reasoning: true,
      credentialLabel: "Anthropic API key",
    },
    gemini: {
      providerID: "gemini",
      name: "Google / Gemini",
      protocol: "openai-compatible",
      baseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
      toolCall: true,
      reasoning: true,
      credentialLabel: "Gemini API key",
    },
    huggingface: {
      providerID: "huggingface",
      name: "Hugging Face",
      protocol: "openai-compatible",
      baseURL: "https://router.huggingface.co/v1",
      toolCall: true,
      reasoning: true,
      credentialLabel: "Hugging Face token",
    },
    openrouter: {
      providerID: "openrouter",
      name: "OpenRouter",
      protocol: "openai-compatible",
      baseURL: "https://openrouter.ai/api/v1",
      toolCall: true,
      reasoning: true,
      credentialLabel: "OpenRouter API key",
    },
  };

  const apiPresetFor = (providerID: string) => apiProviderPresets[clean(providerID)];
  const apiProviderConnected = (preset: TLStudioDynamicRecord) =>
    !!preset?.providerID && K.state.connectedProviders.has(clean(preset.providerID));

  const openAPIProviderPreset = async (preset: TLStudioDynamicRecord) => {
    if (!preset || !K.__providersUi?.openPreset) return;
    K.showError("");
    try {
      await K.__providersUi.openPreset(preset);
    } catch (error) {
      K.showError(error instanceof Error ? error.message : String(error));
    }
  };

  const disconnectAPIProviderPreset = async (account: TLStudioProviderAccount, preset: TLStudioDynamicRecord) => {
    const providerID = clean(preset?.providerID);
    if (!providerID || !apiProviderConnected(preset)) return;
    if (!window.confirm(`Disconnect ${account.name || providerID}? The stored API credential and discovered models for this connection will be removed.`)) return;
    K.showError("");
    try {
      await K.api.providers.disconnectAPI(providerID);
      if (K.state.session?.model?.providerID === providerID) K.state.session.model = undefined;
      if (K.els.modelSelect) K.els.modelSelect.value = "";
      K.state.connectedProviders.delete(providerID);
      await refreshProviderSurfaces();
      window.dispatchEvent(new CustomEvent("tlstudio:providers-changed"));
    } catch (error) {
      K.showError(error instanceof Error ? error.message : String(error));
      try { await refreshProviderSurfaces(); } catch {}
    }
  };

  const setupForm = document.getElementById("providerAccountSetupForm") as HTMLFormElement;
  const setupTitle = document.getElementById("providerAccountSetupTitle") as HTMLElement;
  const setupDescription = document.getElementById("providerAccountSetupDescription") as HTMLElement;
  const setupFields = document.getElementById("providerAccountSetupFields") as HTMLElement;
  const setupNotice = document.getElementById("providerAccountSetupNotice") as HTMLElement;
  const setupClose = document.getElementById("providerAccountSetupClose") as HTMLButtonElement;
  const setupCancel = document.getElementById("providerAccountSetupCancel") as HTMLButtonElement;
  const setupSave = document.getElementById("providerAccountSetupSave") as HTMLButtonElement;
  let setupProviderID = "";
  let setupSaving = false;

  const setSetupNotice = (message = "", error = false) => {
    setupNotice.textContent = message;
    setupNotice.classList.toggle("hidden", !message);
    setupNotice.classList.toggle("error", !!message && error);
  };

  const closeSetup = () => {
    setupProviderID = "";
    setupFields.textContent = "";
    setSetupNotice();
    if (setupDialog.open) setupDialog.close();
  };

  const configureAccount = async (account: TLStudioProviderAccount) => {
    if (!account.setup?.configurable || setupSaving) return;
    setSetupNotice();
    try {
      const setup = await K.api.providerAccounts.setup(account.id);
      setupProviderID = account.id;
      setupTitle.textContent = clean(setup.title) || `Configure ${account.name || account.id}`;
      setupDescription.textContent = clean(setup.description);
      setupFields.textContent = "";
      for (const field of Array.isArray(setup.fields) ? setup.fields : []) {
        const label = document.createElement("label");
        label.className = "provider-account-setup-field";
        const title = document.createElement("span");
        title.textContent = field.label || field.id;
        const input = document.createElement("input");
        input.type = "text";
        input.dataset.providerSetupField = field.id;
        input.value = field.value || "";
        input.placeholder = field.placeholder || "";
        input.required = field.required === true;
        input.readOnly = field.readOnly === true;
        input.autocomplete = "off";
        input.spellcheck = false;
        label.append(title, input);
        if (field.description) {
          const help = document.createElement("small");
          help.textContent = field.description;
          label.appendChild(help);
        }
        setupFields.appendChild(label);
      }
      if (!setupDialog.open) setupDialog.showModal();
      requestAnimationFrame(() => setupFields.querySelector<HTMLInputElement>("input:not([readonly])")?.focus({ preventScroll: true }));
    } catch (error) {
      K.showError(error instanceof Error ? error.message : String(error));
    }
  };

  const providerAccountLogo = (account: TLStudioProviderAccount) => {
    const icons: Record<string, string> = {
      chatgpt: `<svg viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" d="M22.2819 9.8211a5.9847 5.9847 0 0 0-.5157-4.9108 6.0462 6.0462 0 0 0-6.5098-2.9A6.0651 6.0651 0 0 0 4.9807 4.1818a5.9847 5.9847 0 0 0-3.9977 2.9 6.0462 6.0462 0 0 0 .7427 7.0966 5.98 5.98 0 0 0 .511 4.9107 6.051 6.051 0 0 0 6.5146 2.9001A5.9847 5.9847 0 0 0 13.2599 24a6.0557 6.0557 0 0 0 5.7718-4.2058 5.9894 5.9894 0 0 0 3.9977-2.9001 6.0557 6.0557 0 0 0-.7475-7.0729zm-9.022 12.6081a4.4755 4.4755 0 0 1-2.8764-1.0408l.1419-.0804 4.7783-2.7582a.7948.7948 0 0 0 .3927-.6813v-6.7369l2.02 1.1686a.071.071 0 0 1 .038.052v5.5826a4.504 4.504 0 0 1-4.4945 4.4944zm-9.6607-4.1254a4.4708 4.4708 0 0 1-.5346-3.0137l.142.0852 4.783 2.7582a.7712.7712 0 0 0 .7806 0l5.8428-3.3685v2.3324a.0804.0804 0 0 1-.0332.0615L9.74 19.9502a4.4992 4.4992 0 0 1-6.1408-1.6464zM2.3408 7.8956a4.485 4.485 0 0 1 2.3655-1.9728V11.6a.7664.7664 0 0 0 .3879.6765l5.8144 3.3543-2.0201 1.1685a.0757.0757 0 0 1-.071 0l-4.8303-2.7865A4.504 4.504 0 0 1 2.3408 7.872zm16.5963 3.8558L13.1038 8.364 15.1192 7.2a.0757.0757 0 0 1 .071 0l4.8303 2.7913a4.4944 4.4944 0 0 1-.6765 8.1042v-5.6772a.79.79 0 0 0-.407-.667zm2.0107-3.0231l-.142-.0852-4.7735-2.7818a.7759.7759 0 0 0-.7854 0L9.409 9.2297V6.8974a.0662.0662 0 0 1 .0284-.0615l4.8303-2.7866a4.4992 4.4992 0 0 1 6.6802 4.66zM8.3065 12.863l-2.02-1.1638a.0804.0804 0 0 1-.038-.0567V6.0742a4.4992 4.4992 0 0 1 7.3757-3.4537l-.142.0805L8.704 5.459a.7948.7948 0 0 0-.3927.6813zm1.0976-2.3654l2.602-1.4998 2.6069 1.4998v2.9994l-2.5974 1.4997-2.6067-1.4997Z"/></svg>`,
      claude: `<svg viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" d="m4.7144 15.9555 4.7174-2.6471.079-.2307-.079-.1275h-.2307l-.7893-.0486-2.6956-.0729-2.3375-.0971-2.2646-.1214-.5707-.1215-.5343-.7042.0546-.3522.4797-.3218.686.0608 1.5179.1032 2.2767.1578 1.6514.0972 2.4468.255h.3886l.0546-.1579-.1336-.0971-.1032-.0972L6.973 9.8356l-2.55-1.6879-1.3356-.9714-.7225-.4918-.3643-.4614-.1578-1.0078.6557-.7225.8803.0607.2246.0607.8925.686 1.9064 1.4754 2.4893 1.8336.3643.3035.1457-.1032.0182-.0728-.164-.2733-1.3539-2.4467-1.445-2.4893-.6435-1.032-.17-.6194c-.0607-.255-.1032-.4674-.1032-.7285L6.287.1335 6.6997 0l.9957.1336.419.3642.6192 1.4147 1.0018 2.2282 1.5543 3.0296.4553.8985.2429.8318.091.255h.1579v-.1457l.1275-1.706.2368-2.0947.2307-2.6957.0789-.7589.3764-.9107.7468-.4918.5828.2793.4797.686-.0668.4433-.2853 1.8517-.5586 2.9021-.3643 1.9429h.2125l.2429-.2429.9835-1.3053 1.6514-2.0643.7286-.8196.85-.9046.5464-.4311h1.0321l.759 1.1293-.34 1.1657-1.0625 1.3478-.8804 1.1414-1.2628 1.7-.7893 1.36.0729.1093.1882-.0183 2.8535-.607 1.5421-.2794 1.8396-.3157.8318.3886.091.3946-.3278.8075-1.967.4857-2.3072.4614-3.4364.8136-.0425.0304.0486.0607 1.5482.1457.6618.0364h1.621l3.0175.2247.7892.522.4736.6376-.079.4857-1.2142.6193-1.6393-.3886-3.825-.9107-1.3113-.3279h-.1822v.1093l1.0929 1.0686 2.0035 1.8092 2.5075 2.3314.1275.5768-.3218.4554-.34-.0486-2.2039-1.6575-.85-.7468-1.9246-1.621h-.1275v.17l.4432.6496 2.3436 3.5214.1214 1.0807-.17.3521-.6071.2125-.6679-.1214-1.3721-1.9246L14.38 17.959l-1.1414-1.9428-.1397.079-.674 7.2552-.3156.3703-.7286.2793-.6071-.4614-.3218-.7468.3218-1.4753.3886-1.9246.3157-1.53.2853-1.9004.17-.6314-.0121-.0425-.1397.0182-1.4328 1.9672-2.1796 2.9446-1.7243 1.8456-.4128.164-.7164-.3704.0667-.6618.4008-.5889 2.386-3.0357 1.4389-1.882.929-1.0868-.0062-.1579h-.0546l-6.3385 4.1164-1.1293.1457-.4857-.4554.0608-.7467.2307-.2429 1.9064-1.3114Z"/></svg>`,
      gemini: `<svg viewBox="0 0 24 24" aria-hidden="true"><defs><linearGradient id="tlGeminiBrandGradient" x1="2" y1="2" x2="22" y2="22" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#4285F4"/><stop offset=".48" stop-color="#8E75B2"/><stop offset="1" stop-color="#D965A7"/></linearGradient></defs><path fill="url(#tlGeminiBrandGradient)" d="M11.04 19.32Q12 21.51 12 24q0-2.49.93-4.68.96-2.19 2.58-3.81t3.81-2.55Q21.51 12 24 12q-2.49 0-4.68-.93a12.3 12.3 0 0 1-3.81-2.58 12.3 12.3 0 0 1-2.58-3.81Q12 2.49 12 0q0 2.49-.96 4.68-.93 2.19-2.55 3.81a12.3 12.3 0 0 1-3.81 2.58Q2.49 12 0 12q2.49 0 4.68.96 2.19.93 3.81 2.55t2.55 3.81"/></svg>`,
      "github-copilot": `<svg viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" d="M23.922 16.997C23.061 18.492 18.063 22.02 12 22.02 5.937 22.02.939 18.492.078 16.997A.641.641 0 0 1 0 16.741v-2.869a.883.883 0 0 1 .053-.22c.372-.935 1.347-2.292 2.605-2.656.167-.429.414-1.055.644-1.517a10.098 10.098 0 0 1-.052-1.086c0-1.331.282-2.499 1.132-3.368.397-.406.89-.717 1.474-.952C7.255 2.937 9.248 1.98 11.978 1.98c2.731 0 4.767.957 6.166 2.093.584.235 1.077.546 1.474.952.85.869 1.132 2.037 1.132 3.368 0 .368-.014.733-.052 1.086.23.462.477 1.088.644 1.517 1.258.364 2.233 1.721 2.605 2.656a.841.841 0 0 1 .053.22v2.869a.641.641 0 0 1-.078.256Zm-11.75-5.992h-.344a4.359 4.359 0 0 1-.355.508c-.77.947-1.918 1.492-3.508 1.492-1.725 0-2.989-.359-3.782-1.259a2.137 2.137 0 0 1-.085-.104L4 11.746v6.585c1.435.779 4.514 2.179 8 2.179 3.486 0 6.565-1.4 8-2.179v-6.585l-.098-.104s-.033.045-.085.104c-.793.9-2.057 1.259-3.782 1.259-1.59 0-2.738-.545-3.508-1.492a4.359 4.359 0 0 1-.355-.508Zm2.328 3.25c.549 0 1 .451 1 1v2c0 .549-.451 1-1 1-.549 0-1-.451-1-1v-2c0-.549.451-1 1-1Zm-5 0c.549 0 1 .451 1 1v2c0 .549-.451 1-1 1-.549 0-1-.451-1-1v-2c0-.549.451-1 1-1Zm3.313-6.185c.136 1.057.403 1.913.878 2.497.442.544 1.134.938 2.344.938 1.573 0 2.292-.337 2.657-.751.384-.435.558-1.15.558-2.361 0-1.14-.243-1.847-.705-2.319-.477-.488-1.319-.862-2.824-1.025-1.487-.161-2.192.138-2.533.529-.269.307-.437.808-.438 1.578v.021c0 .265.021.562.063.893Zm-1.626 0c.042-.331.063-.628.063-.894v-.02c-.001-.77-.169-1.271-.438-1.578-.341-.391-1.046-.69-2.533-.529-1.505.163-2.347.537-2.824 1.025-.462.472-.705 1.179-.705 2.319 0 1.211.175 1.926.558 2.361.365.414 1.084.751 2.657.751 1.21 0 1.902-.394 2.344-.938.475-.584.742-1.44.878-2.497Z"/></svg>`,
      huggingface: `<svg viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" d="M12.025 1.13c-5.77 0-10.449 4.647-10.449 10.378 0 1.112.178 2.181.503 3.185.064-.222.203-.444.416-.577a.96.96 0 0 1 .524-.15c.293 0 .584.124.84.284.278.173.48.408.71.694.226.282.458.611.684.951v-.014c.017-.324.106-.622.264-.874s.403-.487.762-.543c.3-.047.596.06.787.203s.31.313.4.467c.15.257.212.468.233.542.01.026.653 1.552 1.657 2.54.616.605 1.01 1.223 1.082 1.912.055.537-.096 1.059-.38 1.572.637.121 1.294.187 1.967.187.657 0 1.298-.063 1.921-.178-.287-.517-.44-1.041-.384-1.581.07-.69.465-1.307 1.081-1.913 1.004-.987 1.647-2.513 1.657-2.539.021-.074.083-.285.233-.542.09-.154.208-.323.4-.467a1.08 1.08 0 0 1 .787-.203c.359.056.604.29.762.543s.247.55.265.874v.015c.225-.34.457-.67.683-.952.23-.286.432-.52.71-.694.257-.16.547-.284.84-.285a.97.97 0 0 1 .524.151c.228.143.373.388.43.625l.006.04a10.3 10.3 0 0 0 .534-3.273c0-5.731-4.678-10.378-10.449-10.378M8.327 6.583a1.5 1.5 0 0 1 .713.174 1.487 1.487 0 0 1 .617 2.013c-.183.343-.762-.214-1.102-.094-.38.134-.532.914-.917.71a1.487 1.487 0 0 1 .69-2.803m7.486 0a1.487 1.487 0 0 1 .689 2.803c-.385.204-.536-.576-.916-.71-.34-.12-.92.437-1.103.094a1.487 1.487 0 0 1 .617-2.013 1.5 1.5 0 0 1 .713-.174m-10.68 1.55a.96.96 0 1 1 0 1.921.96.96 0 0 1 0-1.92m13.838 0a.96.96 0 1 1 0 1.92.96.96 0 0 1 0-1.92M8.489 11.458c.588.01 1.965 1.157 3.572 1.164 1.607-.007 2.984-1.155 3.572-1.164.196-.003.305.12.305.454 0 .886-.424 2.328-1.563 3.202-.22-.756-1.396-1.366-1.63-1.32q-.011.001-.02.006l-.044.026-.01.008-.03.024q-.018.017-.035.036l-.032.04a1 1 0 0 0-.058.09l-.014.025q-.049.088-.11.19a1 1 0 0 1-.083.116 1.2 1.2 0 0 1-.173.18q-.035.029-.075.058a1.3 1.3 0 0 1-.251-.243 1 1 0 0 1-.076-.107c-.124-.193-.177-.363-.337-.444-.034-.016-.104-.008-.2.022q-.094.03-.216.087-.06.028-.125.063l-.13.074q-.067.04-.136.086a3 3 0 0 0-.135.096 3 3 0 0 0-.26.219 2 2 0 0 0-.12.121 2 2 0 0 0-.106.128l-.002.002a2 2 0 0 0-.09.132l-.001.001a1.2 1.2 0 0 0-.105.212q-.013.036-.024.073c-1.139-.875-1.563-2.317-1.563-3.203 0-.334.109-.457.305-.454m.836 10.354c.824-1.19.766-2.082-.365-3.194-1.13-1.112-1.789-2.738-1.789-2.738s-.246-.945-.806-.858-.97 1.499.202 2.362c1.173.864-.233 1.45-.685.64-.45-.812-1.683-2.896-2.322-3.295s-1.089-.175-.938.647 2.822 2.813 2.562 3.244-1.176-.506-1.176-.506-2.866-2.567-3.49-1.898.473 1.23 2.037 2.16c1.564.932 1.686 1.178 1.464 1.53s-3.675-2.511-4-1.297c-.323 1.214 3.524 1.567 3.287 2.405-.238.839-2.71-1.587-3.216-.642-.506.946 3.49 2.056 3.522 2.064 1.29.33 4.568 1.028 5.713-.624m5.349 0c-.824-1.19-.766-2.082.365-3.194 1.13-1.112 1.789-2.738 1.789-2.738s.246-.945.806-.858.97 1.499-.202 2.362c-1.173.864.233 1.45.685.64.451-.812 1.683-2.896 2.322-3.295s1.089-.175.938.647-2.822 2.813-2.562 3.244 1.176-.506 1.176-.506 2.866-2.567 3.49-1.898-.473 1.23-2.037 2.16c-1.564.932-1.686 1.178-1.464 1.53s3.675-2.511 4-1.297c.323 1.214-3.524 1.567-3.287 2.405.238.839 2.71-1.587 3.216-.642.506.946-3.49 2.056-3.522 2.064-1.29.33-4.568 1.028-5.713-.624"/></svg>`,
      openrouter: `<svg viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" d="M16.778 1.844v1.919q-.569-.026-1.138-.032-.708-.008-1.415.037c-1.93.126-4.023.728-6.149 2.237-2.911 2.066-2.731 1.95-4.14 2.75-.396.223-1.342.574-2.185.798-.841.225-1.753.333-1.751.333v4.229s.768.108 1.61.333c.842.224 1.789.575 2.185.799 1.41.798 1.228.683 4.14 2.75 2.126 1.509 4.22 2.11 6.148 2.236.88.058 1.716.041 2.555.005v1.918l7.222-4.168-7.222-4.17v2.176c-.86.038-1.611.065-2.278.021-1.364-.09-2.417-.357-3.979-1.465-2.244-1.593-2.866-2.027-3.68-2.508.889-.518 1.449-.906 3.822-2.59 1.56-1.109 2.614-1.377 3.978-1.466.667-.044 1.418-.017 2.278.02v2.176L24 6.014Z"/></svg>`,
    };
    if (account.id === "claude-web") return icons.claude;
    return icons[account.id] || `<span class="provider-account-logo-fallback">${clean(account.name || account.id).slice(0, 1).toUpperCase() || "?"}</span>`;
  };

  const providerAccountDetails = (account: TLStudioProviderAccount) =>
    [clean(account.description), clean(account.billingNote)].filter(Boolean).join(" · ");

  const statusText = (account: TLStudioProviderAccount) => {
    if (!account.available) return "Unavailable";
    if (account.state === "connecting") return "Connecting";
    if (account.state === "expired") return "Expired";
    if (account.state === "needs_reauthentication") return "Needs reauthentication";
    if (account.state === "error") return "Error";
    if (account.connected) return "Connected";
    return "Not connected";
  };

  const syncConnectedProviders = () => {
    for (const account of K.state.providerAccounts) {
      if (!accountLoginProviderIDs.has(account.id)) continue;
      const runtimeProviderID = accountRuntimeProviderIDs[account.id] || account.id;
      if (account.connected) K.state.connectedProviders.add(runtimeProviderID);
      else K.state.connectedProviders.delete(runtimeProviderID);
    }
  };

  const renderAccountButton = () => {
    const connected = K.state.providerAccounts.filter((account) =>
      accountLoginProviderIDs.has(account.id) && account.connected).length;
    K.els.accountButton.textContent = "Account";
    K.els.accountButton.classList.toggle("signed-in", connected > 0);
    K.els.accountButton.title = connected
      ? `${connected} provider account${connected === 1 ? "" : "s"} connected — open Providers`
      : "Open provider account settings";
  };

  const render = () => {
    accountList.textContent = "";
    const accountByID = new Map(K.state.providerAccounts.map((account) => [account.id, account]));
    const cards = providerCardOrder.map((id) => {
      const account = accountByID.get(id);
      if (account) return account;
      const preset = apiPresetFor(id);
      return {
        id,
        name: clean(preset?.name) || id,
        description: "",
        available: false,
        connected: false,
        state: "disconnected",
        authModes: [],
        capabilities: [],
      } as TLStudioProviderAccount;
    });

    for (const account of cards) {
      const apiPreset = apiPresetFor(account.id);
      const isAPIProvider = !!apiPreset;
      const isAccountProvider = accountLoginProviderIDs.has(account.id);
      const isHybridProvider = isAPIProvider && isAccountProvider;
      const isClaudeProvider = account.id === "claude";
      const claudeWebAccount = isClaudeProvider ? accountByID.get("claude-web") : undefined;
      const apiConnected = isAPIProvider && apiProviderConnected(apiPreset);
      const accountConnected = isAccountProvider && account.connected;
      const claudeWebConnected = !!claudeWebAccount?.connected;
      const connected = accountConnected || claudeWebConnected || apiConnected;
      const available = isAPIProvider || account.available || !!claudeWebAccount?.available;

      const card = document.createElement("div");
      card.className = `provider-account-card${available ? "" : " provider-account-unavailable"}${connected ? " provider-account-connected" : ""}`;
      card.dataset.providerAccountId = account.id;

      const details = isClaudeProvider
        ? [
            claudeWebAccount ? providerAccountDetails(claudeWebAccount) : "",
            providerAccountDetails(account),
            `${clean(apiPreset?.credentialLabel) || "API credential"} · ${clean(apiPreset?.baseURL)}`,
          ].filter(Boolean).join(" · ")
        : isHybridProvider
          ? [providerAccountDetails(account), `${clean(apiPreset.credentialLabel) || "API credential"} · ${clean(apiPreset.baseURL)}`].filter(Boolean).join(" · ")
          : isAPIProvider
            ? `${clean(apiPreset.credentialLabel) || "API credential"} · ${clean(apiPreset.baseURL)}`
            : providerAccountDetails(account);
      if (details) card.title = details;

      if (!isAPIProvider && account.setup?.configurable) {
        const setup = document.createElement("button");
        setup.type = "button";
        setup.className = "ghost provider-account-setup-button";
        setup.dataset.providerAccountAction = "setup";
        setup.textContent = "⚙";
        setup.title = account.setup.label || "Configure provider account setup";
        setup.setAttribute("aria-label", setup.title);
        setup.addEventListener("click", () => { void configureAccount(account); });
        card.appendChild(setup);
      }

      const logo = document.createElement("div");
      logo.className = "provider-account-logo";
      logo.dataset.provider = account.id;
      logo.innerHTML = providerAccountLogo(account);
      logo.setAttribute("aria-hidden", "true");

      const name = document.createElement("div");
      name.className = "provider-account-name";
      name.textContent = isClaudeProvider ? "Claude" : (account.name || account.id);

      const state = document.createElement("div");
      state.className = "provider-account-state";
      const dot = document.createElement("span");
      dot.className = `provider-status-dot${connected ? " ok" : ""}`;
      const stateLabel = document.createElement("span");
      if (isClaudeProvider) {
        const modes: string[] = [];
        if (claudeWebConnected) modes.push("Web");
        if (accountConnected) modes.push("Code");
        if (apiConnected) modes.push("API");
        stateLabel.textContent = modes.length
          ? `${modes.join(" + ")} connected`
          : "Web Free/Pro · Code Pro/Max · API";
      } else {
        stateLabel.textContent = isHybridProvider
          ? accountConnected && apiConnected
            ? "Account + API connected"
            : accountConnected
              ? "Account connected"
              : apiConnected
                ? "API connected"
                : account.available
                  ? "Account or API key"
                  : "API key"
          : isAPIProvider
            ? (apiConnected ? "API connected" : "API key")
            : statusText(account);
      }
      state.append(dot, stateLabel);

      const accountDetails = document.createElement("div");
      accountDetails.className = "provider-account-details";
      const detailAccount = claudeWebConnected ? claudeWebAccount : (accountConnected ? account : undefined);
      accountDetails.textContent = detailAccount
        ? [detailAccount.accountLabel, detailAccount.accountType, detailAccount.organizationId].filter(Boolean).join(" · ")
        : "";
      if (!accountDetails.textContent) accountDetails.setAttribute("aria-hidden", "true");

      const actions = document.createElement("div");
      actions.className = "provider-account-card-actions";

      if (isClaudeProvider) {
        const webAction = document.createElement("button");
        webAction.type = "button";
        webAction.dataset.providerAccountAction = claudeWebConnected ? "disconnect-web" : "connect-web";
        webAction.textContent = claudeWebConnected ? "Web ✓" : "Web";
        webAction.className = claudeWebConnected ? "ghost small provider-account-mode-active" : "primary small provider-account-signin";
        webAction.disabled = !claudeWebConnected && !claudeWebAccount?.available;
        webAction.title = claudeWebConnected
          ? "Sign out of the Claude Web browser session"
          : claudeWebAccount?.available
            ? "Use the Claude session already signed in to this Chrome profile through the TL Studio transport extension"
            : clean(claudeWebAccount?.error) || "Claude Web browser sign-in is unavailable.";
        webAction.addEventListener("click", () => {
          if (!claudeWebAccount) return;
          if (claudeWebConnected) void disconnectAccount(claudeWebAccount);
          else void connectAccount(claudeWebAccount);
        });
        actions.appendChild(webAction);

        const codeAction = document.createElement("button");
        codeAction.type = "button";
        codeAction.dataset.providerAccountAction = accountConnected ? "disconnect-code" : "connect-code";
        codeAction.textContent = accountConnected ? "Code ✓" : "Code";
        codeAction.className = accountConnected ? "ghost small provider-account-mode-active" : "ghost small";
        codeAction.disabled = !accountConnected && !account.available;
        codeAction.title = accountConnected
          ? "Sign out of the Claude Code subscription connection"
          : account.available
            ? "Sign in through Claude Code; requires a supported Claude subscription"
            : clean(account.error) || "Claude Code sign-in is unavailable.";
        codeAction.addEventListener("click", () => {
          if (accountConnected) void disconnectAccount(account);
          else void connectAccount(account);
        });
        actions.appendChild(codeAction);

        const configure = document.createElement("button");
        configure.type = "button";
        configure.className = "ghost small";
        configure.dataset.providerAccountAction = "configure-api";
        configure.textContent = "API";
        configure.title = "Configure Anthropic API key";
        configure.addEventListener("click", () => { void openAPIProviderPreset(apiPreset); });
        actions.appendChild(configure);

        if (apiConnected) {
          const disconnectAPI = document.createElement("button");
          disconnectAPI.type = "button";
          disconnectAPI.className = "ghost small provider-delete";
          disconnectAPI.dataset.providerAccountAction = "disconnect-api";
          disconnectAPI.textContent = "Disconnect API";
          disconnectAPI.title = "Disconnect Anthropic API credential";
          disconnectAPI.addEventListener("click", () => { void disconnectAPIProviderPreset(account, apiPreset); });
          actions.appendChild(disconnectAPI);
        }
      } else if (isHybridProvider) {
        const accountAction = document.createElement("button");
        accountAction.type = "button";
        accountAction.dataset.providerAccountAction = accountConnected ? "disconnect" : "connect";
        accountAction.textContent = accountConnected ? "Sign out" : "Sign in";
        accountAction.className = accountConnected ? "ghost small provider-delete" : "primary small provider-account-signin";
        accountAction.disabled = !accountConnected && !account.available;
        accountAction.title = accountConnected
          ? `Sign out of ${account.name || account.id}`
          : account.available
            ? `Sign in with ${account.name || account.id}`
            : clean(account.error) || "Claude account sign-in is unavailable.";
        accountAction.addEventListener("click", () => {
          if (accountConnected) void disconnectAccount(account);
          else void connectAccount(account);
        });
        actions.appendChild(accountAction);

        const configure = document.createElement("button");
        configure.type = "button";
        configure.className = "ghost small";
        configure.dataset.providerAccountAction = "configure-api";
        configure.textContent = "API";
        configure.title = `Configure ${account.name || account.id} API key`;
        configure.addEventListener("click", () => { void openAPIProviderPreset(apiPreset); });
        actions.appendChild(configure);

        if (apiConnected) {
          const disconnectAPI = document.createElement("button");
          disconnectAPI.type = "button";
          disconnectAPI.className = "ghost small provider-delete";
          disconnectAPI.dataset.providerAccountAction = "disconnect-api";
          disconnectAPI.textContent = "Disconnect API";
          disconnectAPI.title = `Disconnect ${account.name || account.id} API credential`;
          disconnectAPI.addEventListener("click", () => { void disconnectAPIProviderPreset(account, apiPreset); });
          actions.appendChild(disconnectAPI);
        }
      } else if (isAPIProvider) {
        const configure = document.createElement("button");
        configure.type = "button";
        configure.className = apiConnected ? "ghost small" : "primary small provider-account-signin";
        configure.dataset.providerAccountAction = "configure-api";
        configure.textContent = "Configure";
        configure.title = `Configure ${account.name || account.id} with an API credential`;
        configure.addEventListener("click", () => { void openAPIProviderPreset(apiPreset); });
        actions.appendChild(configure);

        if (apiConnected) {
          const disconnect = document.createElement("button");
          disconnect.type = "button";
          disconnect.className = "ghost small provider-delete";
          disconnect.dataset.providerAccountAction = "disconnect-api";
          disconnect.textContent = "Disconnect";
          disconnect.title = `Disconnect ${account.name || account.id} API credential`;
          disconnect.addEventListener("click", () => { void disconnectAPIProviderPreset(account, apiPreset); });
          actions.appendChild(disconnect);
        }
      } else if (account.connected) {
        const reconnect = document.createElement("button");
        reconnect.type = "button";
        reconnect.className = "ghost small";
        reconnect.dataset.providerAccountAction = "reconnect";
        reconnect.textContent = "Reconnect";
        reconnect.disabled = !account.available;
        reconnect.addEventListener("click", () => { void connectAccount(account); });

        const disconnect = document.createElement("button");
        disconnect.type = "button";
        disconnect.className = "ghost small provider-delete";
        disconnect.dataset.providerAccountAction = "disconnect";
        disconnect.textContent = "Sign out";
        disconnect.addEventListener("click", () => { void disconnectAccount(account); });
        actions.append(reconnect, disconnect);
      } else {
        const connect = document.createElement("button");
        connect.type = "button";
        connect.className = "primary small provider-account-signin";
        connect.dataset.providerAccountAction = "connect";
        connect.textContent = account.available ? "Sign in" : "Unavailable";
        connect.disabled = !account.available;
        connect.title = account.available
          ? `Sign in with ${account.name || account.id}`
          : clean(account.error) || clean(account.description) || "This account integration is unavailable.";
        connect.addEventListener("click", () => { void connectAccount(account); });
        actions.appendChild(connect);
      }

      card.append(logo, name, state, accountDetails, actions);
      accountList.appendChild(card);
    }
    renderAccountButton();
  };

  const load = async () => {
    if (loading) return K.state.providerAccounts;
    loading = true;
    try {
      const accounts = await K.api.providerAccounts.list();
      K.state.providerAccounts = Array.isArray(accounts) ? accounts : [];
      syncConnectedProviders();
      if (
        K.state.session?.model?.providerID === "claude-web-account" &&
        clean(K.state.session.model.id || K.state.session.model.modelID) === "default"
      ) {
        K.state.session.model = {
          ...K.state.session.model,
          id: CLAUDE_WEB_MODEL_ID,
          modelID: CLAUDE_WEB_MODEL_ID,
        };
      }
      render();
      window.setTimeout(() => { void resumeClaudeWebIfNeeded(); }, 0);
      return K.state.providerAccounts;
    } catch (error) {
      K.state.providerAccounts = [];
      render();
      throw error;
    } finally {
      loading = false;
    }
  };

  const refreshProviderSurfaces = async () => {
    await K.loadCatalog();
    await load();
    K.__providersUi?.reload?.();
    K.renderModels?.();
    K.renderSessionHeader?.();
  };

  const wait = (ms: number, signal: AbortSignal) => new Promise<void>((resolve, reject) => {
    const timer = window.setTimeout(resolve, ms);
    signal.addEventListener("abort", () => {
      window.clearTimeout(timer);
      reject(new DOMException("Provider login cancelled", "AbortError"));
    }, { once: true });
  });

  const sendClaudeWebExtensionMessageTo = (
    extensionID: string,
    message: TLStudioDynamicRecord,
    timeoutMs: number,
  ) => new Promise<TLStudioDynamicRecord>((resolve, reject) => {
    const runtime = (window as any).chrome?.runtime;
    if (!runtime?.sendMessage) {
      reject(new Error("TL Studio Claude Web Bridge is not installed or enabled in this Chrome profile."));
      return;
    }

    let settled = false;
    const finish = (callback: () => void) => {
      if (settled) return;
      settled = true;
      window.clearTimeout(timer);
      callback();
    };
    const timer = window.setTimeout(() => {
      finish(() => reject(new Error(`TL Studio Claude Web Bridge ${extensionID} did not answer in time.`)));
    }, timeoutMs);

    try {
      runtime.sendMessage(
        extensionID,
        message,
        (response: TLStudioDynamicRecord | undefined) => {
          const lastError = runtime.lastError;
          if (lastError?.message) {
            finish(() => reject(new Error(clean(lastError.message))));
            return;
          }
          if (!response) {
            finish(() => reject(new Error(`TL Studio Claude Web Bridge ${extensionID} returned no response.`)));
            return;
          }
          finish(() => resolve(response));
        },
      );
    } catch (error) {
      finish(() => reject(error instanceof Error ? error : new Error(String(error))));
    }
  });

  const sendClaudeWebExtensionMessage = async (
    message: TLStudioDynamicRecord,
    timeoutMs = 8000,
  ) => {
    const ids = claudeWebExtensionID
      ? [claudeWebExtensionID, ...CLAUDE_WEB_EXTENSION_IDS.filter((id) => id !== claudeWebExtensionID)]
      : CLAUDE_WEB_EXTENSION_IDS;
    let lastError: Error | null = null;
    for (const extensionID of ids) {
      try {
        const response = await sendClaudeWebExtensionMessageTo(extensionID, message, timeoutMs);
        claudeWebExtensionID = extensionID;
        return response;
      } catch (error) {
        lastError = error instanceof Error ? error : new Error(String(error));
      }
    }
    throw new Error(
      `TL Studio Claude Web Bridge is unreachable. ${clean(lastError?.message) || "Install or enable the Chrome Web Store extension."}`,
    );
  };

  const stopClaudeWebRelay = () => {
    const token = claudeWebRelayToken;
    claudeWebRelayController?.abort();
    claudeWebRelayController = null;
    claudeWebRelayToken = "";
    if (token) {
      void sendClaudeWebExtensionMessage({
        type: "tlstudio-unpair",
        token,
      }, 4000).catch(() => {});
    }
  };

  const claudeWebRelayFetch = async (path: string, init?: RequestInit) => {
    const response = await fetch(path, {
      cache: "no-store",
      credentials: "same-origin",
      ...init,
    });
    if (!response.ok) {
      let detail = "";
      try {
        const body = await response.json();
        detail = clean(body?.error);
      } catch {}
      throw new Error(detail || `Claude Web relay returned HTTP ${response.status}`);
    }
    return response;
  };

  const startClaudeWebRelay = (token: string) => {
    stopClaudeWebRelay();
    const cleanToken = clean(token);
    if (!cleanToken) throw new Error("Claude Web relay token is missing.");

    const controller = new AbortController();
    claudeWebRelayController = controller;
    claudeWebRelayToken = cleanToken;

    void (async () => {
      while (!controller.signal.aborted && claudeWebRelayToken === cleanToken) {
        try {
          const response = await claudeWebRelayFetch(
            `/local/claude-web-ui/poll?token=${encodeURIComponent(cleanToken)}`,
            { signal: controller.signal },
          );
          const payload = await response.json();
          const command = payload?.command as TLStudioDynamicRecord | undefined;
          if (command?.id) {
            let result: TLStudioDynamicRecord;
            try {
              result = await sendClaudeWebExtensionMessage({
                type: "tlstudio-execute-direct",
                token: cleanToken,
                command,
              }, 310000);
            } catch (error) {
              result = {
                id: clean(command.id),
                ok: false,
                error: error instanceof Error ? error.message : String(error),
              };
            }
            await claudeWebRelayFetch(
              `/local/claude-web-ui/result?token=${encodeURIComponent(cleanToken)}`,
              {
                method: "POST",
                signal: controller.signal,
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify(result),
              },
            );
            continue;
          }
        } catch (error) {
          if (controller.signal.aborted) return;
          console.warn("Claude Web relay:", error);
        }
        await new Promise<void>((resolve) => window.setTimeout(resolve, 180));
      }
    })();
  };

  const establishClaudeWebBridge = async (
    login: TLStudioDynamicRecord,
    signal: AbortSignal,
    onStage?: (message: string) => void,
  ) => {
    const token = clean(login.bridgeToken);
    const origin = clean(login.bridgeOrigin) || window.location.origin;
    if (!token) throw new Error("Claude Web extension pairing token is missing.");
    if (origin !== window.location.origin) throw new Error("Claude Web extension pairing origin mismatch.");

    onStage?.("Step 1/3 · Contacting TL Studio Claude Web Bridge…");
    const ping = await sendClaudeWebExtensionMessage({ type: "tlstudio-ping" });
    if (!ping.ok) {
      throw new Error(clean(ping.error) || "TL Studio Claude Web Bridge did not accept the connection.");
    }
    if (clean(ping.bridgeVersion) !== CLAUDE_WEB_BRIDGE_VERSION) {
      throw new Error(
        `TL Studio Claude Web Bridge is outdated or incompatible. Expected ${CLAUDE_WEB_BRIDGE_VERSION}, received ${clean(ping.bridgeVersion) || "unknown"}.`,
      );
    }

    onStage?.("Step 2/3 · Checking the Claude session in this Chrome profile…");
    const paired = await sendClaudeWebExtensionMessage({
      type: "tlstudio-pair-direct",
      token,
      origin,
    }, 20000);
    if (clean(paired.bridgeVersion) !== CLAUDE_WEB_BRIDGE_VERSION) {
      throw new Error(
        `TL Studio Claude Web Bridge changed during pairing. Expected ${CLAUDE_WEB_BRIDGE_VERSION}, received ${clean(paired.bridgeVersion) || "unknown"}.`,
      );
    }
    if (!paired.ok || !paired.connected) {
      const stage = clean(paired.stage);
      const detail = clean(paired.error) || "Claude is not signed in in this Chrome profile.";
      throw new Error(stage ? `${stage}: ${detail}` : detail);
    }

    onStage?.("Step 3/3 · Claude session found. Connecting it to TL Studio…");
    await claudeWebRelayFetch(
      `/local/claude-web-ui/pair?token=${encodeURIComponent(token)}`,
      { method: "POST", signal },
    );
    startClaudeWebRelay(token);
    return token;
  };

  const resumeClaudeWebIfNeeded = async () => {
    if (claudeWebRelayToken || claudeWebResumePromise) return claudeWebResumePromise;
    const account = K.state.providerAccounts.find((item) => item.id === "claude-web");
    if (!account?.available) return;
    if (account.state !== "needs_reauthentication" && !account.connected) return;

    claudeWebResumePromise = (async () => {
      const controller = new AbortController();
      try {
        const login = await K.api.providerAccounts.beginLogin("claude-web");
        if (clean(login.flow) !== "claude_web_extension") {
          throw new Error("Claude Web returned an unexpected reconnect flow.");
        }
        await establishClaudeWebBridge(login, controller.signal);

        const expiresAt = login.expiresAt ? Date.parse(login.expiresAt) : 0;
        for (;;) {
          if (expiresAt && Date.now() >= expiresAt) throw new Error("Claude Web bridge reconnect expired.");
          const status = await K.api.providerAccounts.pollLogin("claude-web", login.loginId, controller.signal);
          if (status.state === "connected" || status.connected) {
            const accounts = await K.api.providerAccounts.list();
            K.state.providerAccounts = Array.isArray(accounts) ? accounts : [];
            syncConnectedProviders();
            render();
            await K.loadCatalog();
            K.__providersUi?.reload?.();
            K.renderModels?.();
            K.renderSessionHeader?.();
            return;
          }
          if (status.state === "expired" || status.state === "error") {
            throw new Error(status.error || "Claude Web bridge reconnect failed.");
          }
          await wait(250, controller.signal);
        }
      } catch (error) {
        stopClaudeWebRelay();
        console.warn("Claude Web automatic reconnect:", error);
      }
    })().finally(() => {
      claudeWebResumePromise = null;
    });
    return claudeWebResumePromise;
  };

  const connectAccount = async (account: TLStudioProviderAccount) => {
    if (!account.available) return;
    K.showError("");
    K.state.authController?.abort();
    const controller = new AbortController();
    K.state.authController = controller;
    K.state.authURL = "";
    K.state.authProviderID = account.id;
    K.state.authLoginID = "";

    const title = K.els.authDialog.querySelector("h2");
    if (title) title.textContent = `Sign in with ${account.name || account.id}`;
    K.els.authInstructions.textContent = "Starting secure provider authorization…";
    K.els.authCode.textContent = "";
    K.els.authCodeWrap.classList.add("hidden");
    K.els.authOpen.disabled = true;
    K.els.authDialog.showModal();

    try {
      const login = await K.api.providerAccounts.beginLogin(account.id);
      K.state.authLoginID = clean(login.loginId);
      K.state.authURL = clean(login.authorizationUrl || login.verificationUrl);
      K.els.authInstructions.textContent = clean(login.instructions) || "Authorization is ready. Open the sign-in page to continue.";

      if (clean(login.flow) === "claude_web_extension") {
        await establishClaudeWebBridge(
          login,
          controller.signal,
          (message) => { K.els.authInstructions.textContent = message; },
        );
        K.els.authOpen.disabled = true;
      }

      const code = clean(login.userCode);
      if (code) {
        K.els.authCode.textContent = code;
        K.els.authCodeWrap.classList.remove("hidden");
      }
      if (clean(login.flow) !== "claude_web_extension") {
        K.els.authOpen.disabled = !K.state.authURL;
      }

      const expiresAt = login.expiresAt ? Date.parse(login.expiresAt) : 0;
      const interval = Math.max(1, Number(login.pollIntervalSeconds) || 2) * 1000;
      while (!controller.signal.aborted) {
        if (expiresAt && Date.now() >= expiresAt) throw new Error("Provider sign-in expired. Start again.");

        const status = await K.api.providerAccounts.pollLogin(account.id, login.loginId, controller.signal);
        if (status.state === "connected" || status.connected) {
          K.state.authController = null;
          K.state.authProviderID = "";
          K.state.authLoginID = "";
          K.els.authInstructions.textContent = "Signed in successfully.";
          await refreshProviderSurfaces();
          window.setTimeout(() => { if (K.els.authDialog.open) K.els.authDialog.close(); }, 650);
          return;
        }
        if (status.state === "expired") throw new Error("Provider sign-in expired. Start again.");
        if (status.state === "needs_reauthentication") throw new Error("Provider requires authentication again.");
        if (status.state === "error") throw new Error(status.error || "Provider sign-in failed.");
        await wait(interval, controller.signal);
      }
    } catch (error) {
      if (account.id === "claude-web") stopClaudeWebRelay();
      if ((error as any)?.name !== "AbortError") {
        K.els.authInstructions.textContent = `Sign-in failed: ${error instanceof Error ? error.message : String(error)}`;
      }
      try { await load(); } catch {}
    } finally {
      if (K.state.authController === controller) K.state.authController = null;
    }
  };

  setupForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    if (!setupProviderID || setupSaving) return;
    setupSaving = true;
    setupSave.disabled = true;
    setSetupNotice();
    try {
      const values: Record<string, string> = {};
      for (const input of setupFields.querySelectorAll<HTMLInputElement>("[data-provider-setup-field]")) {
        values[clean(input.dataset.providerSetupField)] = clean(input.value);
      }
      await K.api.providerAccounts.configureSetup(setupProviderID, values);
      closeSetup();
      await refreshProviderSurfaces();
    } catch (error) {
      setSetupNotice(error instanceof Error ? error.message : String(error), true);
    } finally {
      setupSaving = false;
      setupSave.disabled = false;
    }
  });
  setupClose.addEventListener("click", closeSetup);
  setupCancel.addEventListener("click", closeSetup);
  setupDialog.addEventListener("close", () => {
    if (!setupSaving) {
      setupProviderID = "";
      setupFields.textContent = "";
      setSetupNotice();
    }
  });

  const disconnectAccount = async (account: TLStudioProviderAccount) => {
    if (!account.connected) return;
    if (!window.confirm(`Sign out of ${account.name || account.id} on this computer?`)) return;
    K.showError("");
    if (account.id === "claude-web") stopClaudeWebRelay();
    try {
      await K.api.providerAccounts.disconnect(account.id);
      const runtimeProviderID = accountRuntimeProviderIDs[account.id] || account.id;
      if (K.state.session?.model?.providerID === runtimeProviderID) K.state.session.model = undefined;
      if (K.els.modelSelect) K.els.modelSelect.value = "";
      await refreshProviderSurfaces();
    } catch (error) {
      K.showError(error instanceof Error ? error.message : String(error));
      try { await load(); } catch {}
    }
  };

  const open = async () => {
    if (typeof K.activateSettingsSection === "function") K.activateSettingsSection("providers");
    if (!settingsDialog.open) settingsDialog.showModal();
    await Promise.allSettled([
      K.__providersUi?.reload?.(),
      load(),
    ]);
  };

  K.__providerAccountsUi = { load, render, open, connectAccount, configureAccount, disconnectAccount, disconnectAPIProviderPreset };
  K.openProviderAccounts = open;

  const previousRenderAccount = K.renderAccount;
  K.renderAccount = () => {
    if (K.state.providerAccounts.length) {
      renderAccountButton();
      return;
    }
    previousRenderAccount?.();
  };

  window.addEventListener("tlstudio:providers-changed", () => { void load(); });
  render();
  void load().catch(() => {});
})();
