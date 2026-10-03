package main

import (
	"strings"
	"testing"
)

func TestPluginSettingsAlwaysShowBundledAndUserSections(t *testing.T) {
	source := readBrowserSource(t, "plugins.ts")
	for _, required := range []string{
		`Included with TL Studio`,
		`No bundled plugins in this build`,
		`Added by you`,
		`No plugins added yet`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("plugins UI must keep section visible when empty; missing %q", required)
		}
	}
	if strings.Contains(source, `if (!plugins.length) return;`) {
		t.Fatal("empty plugin groups must not disappear from Settings")
	}
}

func TestJevSettingsUseCompactControlAndRefreshAfterProviderChanges(t *testing.T) {
	source := readBrowserSource(t, "jev-ui.ts")
	for _, required := range []string{
		`.jev-compact-row`,
		`Active JEV`,
		`Configure JEV`,
		`jevConfigDialog`,
		`tlstudio:providers-changed`,
		`providerHasRouter`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("compact Jev settings contract missing %q", required)
		}
	}
	if strings.Contains(source, `.jev-settings-card`) {
		t.Fatal("legacy full-height Jev card should not remain in Providers settings")
	}
}


func TestJevSetupIsAutomaticAndDoesNotOpenGenericProviderForm(t *testing.T) {
	source := readBrowserSource(t, "jev-ui.ts")
	for _, required := range []string{
		`K.api.providers.discover`,
		`K.api.providers.upsert`,
		`typesafe/jev-router`,
		`No manual model selection was required.`,
		`id="jevOpenRouterKeyInput"`,
		`data-form-type="other"`,
		`data-lpignore="true"`,
		`data-1p-ignore`,
		`-webkit-text-security:disc`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("one-click Jev setup contract missing %q", required)
		}
	}
	for _, forbidden := range []string{
		`providersUI.openProvider`,
		`discoverySelection?.discoverModel`,
		`TypeSafe via OpenRouter`,
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("Jev setup must not require the generic provider/model UI; found %q", forbidden)
		}
	}
}

func TestManagedJevProviderIsHiddenAndRouterIsTopLevel(t *testing.T) {
	providers := readBrowserSource(t, "providers-ui.ts")
	core := readBrowserSource(t, "core.ts")
	jev := readBrowserSource(t, "jev-ui.ts")

	for _, required := range []string{
		`.filter((provider: any) => !clean(provider?.managedBy))`,
		`managedBy: "jev"`,
	} {
		if !strings.Contains(providers+jev, required) {
			t.Fatalf("managed JEV provider UI contract missing %q", required)
		}
	}
	if !strings.Contains(core, `const routers = K.state.models.filter((model) => model.kind === "router")`) {
		t.Fatal("model selector must separate routers from provider-grouped models")
	}
	if !strings.Contains(core, `select.appendChild(option);`) {
		t.Fatal("router models must be appended directly at the top level")
	}
}

func TestJevCompactSwitchActuallyTogglesPersistedEnablement(t *testing.T) {
	source := readBrowserSource(t, "jev-ui.ts")
	for _, required := range []string{
		`K.api.jevRouter.status()`,
		`K.api.jevRouter.configure(false)`,
		`K.api.jevRouter.configure(true)`,
		`const toggleJev = async () =>`,
		`compactControl.addEventListener("click", () => { void toggleJev(); })`,
		`JEV unavailable`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("JEV toggle contract missing %q", required)
		}
	}
	if strings.Contains(source, `compactControl.addEventListener("click", () => { void openDialog(); })`) {
		t.Fatal("compact JEV control must toggle active state instead of always opening configuration")
	}
}

func TestProviderAPIKeyFieldAvoidsPasswordManagerSemantics(t *testing.T) {
	source := readBrowserSource(t, "providers-ui.ts")
	for _, required := range []string{
		`id="providerApiKeyInput"`,
		`type="text"`,
		`autocomplete="off"`,
		`data-form-type="other"`,
		`data-lpignore="true"`,
		`data-1p-ignore`,
		`-webkit-text-security:disc`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("provider API key field must avoid password-manager semantics; missing %q", required)
		}
	}
	if strings.Contains(source, `id="providerApiKeyInput" type="password"`) ||
		strings.Contains(source, `autocomplete="new-password"`) {
		t.Fatal("provider API key field must not be presented to the browser as a login password")
	}
}


func TestProviderSettingsUseDedicatedModalEditor(t *testing.T) {
	source := readBrowserSource(t, "providers-ui.ts")
	for _, required := range []string{
		`providerDialog.id = "providerDialog"`,
		`providerDialog.showModal()`,
		`providerDialog.close()`,
		`providerDialog.addEventListener("close", resetFormFields)`,
		`type="submit">Done</button>`,
		`edit.textContent = "Configure"`,
		`.provider-dialog-card{width:min(760px,calc(100vw - 36px))`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("provider modal contract missing %q", required)
		}
	}
	if strings.Contains(source, `<form id="providerForm" class="provider-form hidden">`) {
		t.Fatal("provider form must not remain as a hidden inline Settings form")
	}
}


func TestCustomProviderSetupStaysSimpleAndAutoDiscoversModels(t *testing.T) {
	providers := readBrowserSource(t, "providers-ui.ts")
	discovery := readBrowserSource(t, "provider-discovery-ui.ts")

	for _, required := range []string{
		`id="providerIdInput" type="hidden"`,
		`OpenAI-compatible`,
		`Anthropic-compatible`,
		`TL Studio will discover the available models automatically.`,
		`discoverySelection?.discoverAll?.()`,
	} {
		if !strings.Contains(providers, required) {
			t.Fatalf("simplified provider setup missing %q", required)
		}
	}
	for _, required := range []string{
		`discoverySelection.discoverAll = () => discover()`,
		`if (!existingModels.length) {`,
		`for (const model of catalog) selectedIDs.add(model.id)`,
		`Manual model entry`,
	} {
		if !strings.Contains(discovery, required) {
			t.Fatalf("automatic provider discovery contract missing %q", required)
		}
	}
	if !strings.Contains(providers, `<option value="openai-responses" hidden>OpenAI Responses</option>`) {
		t.Fatal("existing OpenAI Responses providers must remain editable without exposing the advanced protocol in the new-provider UI")
	}
}


func TestProviderAccountSettingsUseCompactLogoGrid(t *testing.T) {
	source := readBrowserSource(t, "provider-account-ui.ts")
	for _, required := range []string{
		`class="provider-account-grid"`,
		`provider-account-card`,
		`provider-account-logo`,
		`providerAccountLogo`,
		`provider-account-setup-button`,
		`configure.textContent = "Configure"`,
		`accountLoginProviderIDs`,
		`apiProviderPresets`,
		`openAPIProviderPreset`,
		`grid-template-columns:repeat(3,minmax(0,1fr))`,
		`grid-template-areas:"logo name" "logo state" "details details" "actions actions"`,
		`grid-template-columns:repeat(auto-fit,minmax(64px,1fr))`,
		`render();`,
		`void load().catch(() => {});`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("compact provider account card grid missing %q", required)
		}
	}
	if strings.Contains(source, `meta.textContent = [statusText(account), description, billing]`) {
		t.Fatal("provider account cards must not render full provider descriptions inline")
	}
}

func TestProviderAccountCardsUseBundledBrandMarks(t *testing.T) {
	source := readBrowserSource(t, "provider-account-ui.ts")
	for _, required := range []string{
		`logo.dataset.provider = account.id`,
		`data-provider="chatgpt"`,
		`background:#D97757`,
		`background:#FFD21E`,
		`background:#94A3B8`,
		`tlGeminiBrandGradient`,
		`M22.2819 9.8211`,
		`m4.7144 15.9555`,
		`M23.922 16.997`,
		`M12.025 1.13`,
		`M16.778 1.844`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("provider brand mark contract missing %q", required)
		}
	}
	for _, forbidden := range []string{
		`https://cdn.`,
		`<img src=`,
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("provider brand marks must stay bundled/local; found %q", forbidden)
		}
	}
}

func TestProviderAccountBrowserUsesSemanticLoginLifecycle(t *testing.T) {
	source := readBrowserSource(t, "provider-account-ui.ts")
	runtimeAPI := readBrowserSource(t, "runtime-api.ts")
	attention := readBrowserSource(t, "attention.ts")
	for _, required := range []string{
		`beginLogin(account.id)`,
		`pollLogin(account.id, login.loginId`,
		`cancelLogin(providerID, loginID)`,
		`needs_reauthentication`,
	} {
		if !strings.Contains(source+runtimeAPI+attention, required) {
			t.Fatalf("provider account semantic lifecycle missing %q", required)
		}
	}
	for _, forbidden := range []string{
		`.authorize(account.id)`,
		`.callback(account.id`,
		`accessToken`,
		`refreshToken`,
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("browser provider account UI must not handle provider secrets or raw auth payloads; found %q", forbidden)
		}
	}
}


func TestProviderCardsRouteAPIBasedServicesToPresetConfiguration(t *testing.T) {
	accounts := readBrowserSource(t, "provider-account-ui.ts")
	providers := readBrowserSource(t, "providers-ui.ts")

	for _, required := range []string{
		`claude: {`,
		`providerID: "claude"`,
		`protocol: "anthropic-messages"`,
		`baseURL: "https://api.anthropic.com/v1"`,
		`gemini: {`,
		`baseURL: "https://generativelanguage.googleapis.com/v1beta/openai"`,
		`huggingface: {`,
		`baseURL: "https://router.huggingface.co/v1"`,
		`openrouter: {`,
		`baseURL: "https://openrouter.ai/api/v1"`,
		`configure.dataset.providerAccountAction = "configure-api"`,
		`configure.textContent = "Configure"`,
	} {
		if !strings.Contains(accounts, required) {
			t.Fatalf("API provider preset contract missing %q", required)
		}
	}

	for _, required := range []string{
		`const openPreset = async (preset: any) =>`,
		`providerConfig = await K.api.providers.config()`,
		`K.__providersUi.openPreset = openPreset`,
		`requestAnimationFrame(() => els.apiKey?.focus?.({ preventScroll: true }))`,
	} {
		if !strings.Contains(providers, required) {
			t.Fatalf("provider preset editor contract missing %q", required)
		}
	}

	if strings.Contains(accounts, `if (!account.available) return;
    K.showError("");
    K.state.authController`) == false {
		t.Fatal("account login lifecycle must remain intact for account-based providers")
	}
}


func TestBrandedAPIProvidersStayOnCardsInsteadOfCustomList(t *testing.T) {
	accounts := readBrowserSource(t, "provider-account-ui.ts")
	providers := readBrowserSource(t, "providers-ui.ts")
	runtimeAPI := readBrowserSource(t, "runtime-api.ts")

	for _, required := range []string{
		`BRANDED_API_PROVIDER_IDS = new Set(["claude", "gemini", "huggingface", "openrouter"])`,
		`!BRANDED_API_PROVIDER_IDS.has(clean(provider?.id))`,
		`providerConfigEntryByID(providerConfig, providerID)`,
	} {
		if !strings.Contains(providers, required) {
			t.Fatalf("branded API provider list contract missing %q", required)
		}
	}
	for _, required := range []string{
		`disconnectAPIProviderPreset`,
		`disconnect.dataset.providerAccountAction = "disconnect-api"`,
		`disconnect.textContent = "Disconnect"`,
		`K.api.providers.disconnectAPI(providerID)`,
	} {
		if !strings.Contains(accounts, required) {
			t.Fatalf("branded API provider card disconnect contract missing %q", required)
		}
	}
	if !strings.Contains(runtimeAPI, `/api-connection`) {
		t.Fatal("runtime API must expose the API-only disconnect route")
	}
}


func TestActionButtonsShareOneVisualGeometryContract(t *testing.T) {
	styles := readBrowserSource(t, "../web/styles.css")
	settings := readBrowserSource(t, "../web/settings.css")
	polish := readBrowserSource(t, "../web/polish.css")
	status := readBrowserSource(t, "../web/status-ui.css")
	files := readBrowserSource(t, "../web/files.css")
	jev := readBrowserSource(t, "jev-ui.ts")
	accounts := readBrowserSource(t, "provider-account-ui.ts")

	for _, required := range []string{
		`--tl-button-height: 34px`,
		`--tl-button-height-small: 28px`,
		`--tl-button-radius: 8px`,
		`--tl-button-radius-small: 7px`,
		`height:var(--tl-button-height)`,
		`height:var(--tl-button-height-small)`,
		`white-space:nowrap`,
	} {
		if !strings.Contains(styles+polish, required) {
			t.Fatalf("shared button geometry contract missing %q", required)
		}
	}
	if !strings.Contains(settings, `height: var(--tl-button-height);`) {
		t.Fatal("settings primary actions must use the shared action-button height")
	}
	if !strings.Contains(status, `height: var(--tl-button-height-small);`) {
		t.Fatal("recovery actions must use the shared small-button height")
	}
	if strings.Contains(files, `.file-editor-actions .primary.small { min-height: 27px; }`) {
		t.Fatal("file editor must not override the shared small-button height")
	}
	if !strings.Contains(jev, `.jev-config-row>button{flex:none}`) {
		t.Fatal("JEV row actions must not shrink and wrap inside the settings dialog")
	}
	if strings.Contains(accounts, `.provider-account-card-actions .primary,.provider-account-card-actions .ghost{min-height:28px`) {
		t.Fatal("provider account card actions must inherit the shared small-button geometry")
	}
}


func TestClaudeProviderCardMakesWebAndCodeLoginUnambiguous(t *testing.T) {
	source := readBrowserSource(t, "provider-account-ui.ts")
	for _, required := range []string{
		`accountLoginProviderIDs = new Set(["chatgpt", "claude", "claude-web", "github-copilot"])`,
		`"claude-web": "claude-web-account"`,
		`providerCardOrder = ["chatgpt", "claude", "gemini"`,
		`const claudeWebAccount = isClaudeProvider ? accountByID.get("claude-web") : undefined`,
		`webAction.textContent = claudeWebConnected ? "Web ✓" : "Web"`,
		`codeAction.textContent = accountConnected ? "Code ✓" : "Code"`,
		`else void connectAccount(claudeWebAccount)`,
		`else void connectAccount(account)`,
		`configure.textContent = "API"`,
		`const runtimeProviderID = accountRuntimeProviderIDs[account.id] || account.id`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("Claude combined provider card contract missing %q", required)
		}
	}
	if strings.Contains(source, `providerCardOrder = ["chatgpt", "claude", "claude-web"`) {
		t.Fatal("Claude Web must not render as a second ambiguous standalone card")
	}
	for _, required := range []string{
		`provider-account-mode-active`,
		`grid-template-columns:repeat(auto-fit,minmax(64px,1fr))`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("Claude provider actions must use compact responsive controls; missing %q", required)
		}
	}
}


func TestProviderSettingsWindowExpandsForProviderGrid(t *testing.T) {
	bridge := readBrowserSource(t, "providers-settings-bridge.ts")
	for _, required := range []string{
		`settings-window-providers`,
		`width: min(980px, calc(100vw - 36px))`,
		`max-height: min(72vh, 690px)`,
		`new MutationObserver(syncProviderWindowMode)`,
	} {
		if !strings.Contains(bridge, required) {
			t.Fatalf("provider settings layout contract missing %q", required)
		}
	}
}

func TestClaudeWebUsesInferenceOnlyExtensionRelay(t *testing.T) {
	accounts := readBrowserSource(t, "provider-account-ui.ts")
	for _, required := range []string{
		`CLAUDE_WEB_MODEL_ID = "claude-sonnet-5-5"`,
		`CLAUDE_WEB_EXTENSION_IDS = [`,
		`"cpellhbmfdhcgkblnmnppndmeiigmjcg"`,
		`"hklkkfhbcohbfpojbcanhgmfanjhnfna"`,
		`CLAUDE_WEB_BRIDGE_VERSION = "0.6.3-persistent-page"`,
		`clean(K.state.session.model.id || K.state.session.model.modelID) === "default"`,
		`tlstudio-pair-direct`,
		`tlstudio-execute-direct`,
		`token: cleanToken`,
		`claude-web-ui/pair`,
		`claude-web-ui/poll`,
		`claude-web-ui/result`,
		`resumeClaudeWebIfNeeded`,
		`establishClaudeWebBridge`,
		`beginLogin(account.id)`,
		`pollLogin(account.id, login.loginId`,
	} {
		if !strings.Contains(accounts, required) {
			t.Fatalf("Claude Web extension relay UI contract missing %q", required)
		}
	}
	for _, forbidden := range []string{
		`claude_web_native_chrome`,
		`dedicated browser profile`,
		`--remote-debugging-port`,
		`fpphidfmpfiibpbloeecegdlecfbhcla`,
	} {
		if strings.Contains(accounts, forbidden) {
			t.Fatalf("Claude Web UI must not fall back to the removed cloned/CDP transport; found %q", forbidden)
		}
	}
}
