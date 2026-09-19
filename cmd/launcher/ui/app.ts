(() => {
  "use strict";
  const K = window.KLU;

  K.refreshAll = async () => {
    K.showError("");
    try {
      await Promise.all([K.checkBackend(), K.loadLocalStatus(), K.loadCatalog(), K.loadSessions(), K.loadActiveSessions()]);
      if (K.state.session) {
        const fresh = K.state.sessions.find((item) => item.id === K.state.session.id);
        if (fresh) K.state.session = fresh;
        await Promise.all([K.loadMessages(), K.loadAttention()]);
        K.renderMessages();
        K.renderSessionHeader();
        K.syncSelectors();
      }
      K.renderSessions();
    } catch (err) { K.showError(err.message || String(err)); }
  };

  const wire = () => {
    const e = K.els;
    e.pickProject.addEventListener("click", K.pickProject);
    e.emptyPickProject.addEventListener("click", K.pickProject);
    e.manualProject.textContent = "Browse…";
    e.manualProject.title = "Choose a project folder";
    e.manualProject.addEventListener("click", K.pickProject);
    e.pathForm.addEventListener("submit", K.setManualProject);
    e.newSession.addEventListener("click", K.newSession);
    e.emptyNewSession.addEventListener("click", K.newSession);
    e.sendButton.addEventListener("click", K.sendPrompt);
    e.refreshButton.addEventListener("click", K.refreshAll);
    e.accountButton.addEventListener("click", K.signInHosted);
    e.authCancel.addEventListener("click", K.cancelAuth);
    e.authOpen.addEventListener("click", () => {
      if (K.state.authURL) window.open(K.state.authURL, "_blank", "noopener,noreferrer");
    });
    e.authCode.addEventListener("click", K.copyAuthCode);
    e.agentSelect.addEventListener("change", K.switchAgent);
    e.modelSelect.addEventListener("change", K.switchModel);
    e.prompt.addEventListener("input", K.resizePrompt);
    e.prompt.addEventListener("keydown", (event) => {
      if (event.key === "Enter" && !event.shiftKey) {
        event.preventDefault();
        K.sendPrompt();
      }
    });
    window.addEventListener("beforeunload", K.stopEvents);
  };

  const init = async () => {
    wire();
    K.resizePrompt();
    try {
      await K.loadLocalStatus();
      if (!await K.checkBackend()) return;
      await Promise.all([K.loadCatalog(), K.loadSessions(), K.loadActiveSessions()]);
      K.renderSessions();
      K.startEvents();
    } catch (err) { K.showError(err.message || String(err)); }
  };

  const loadScript = (src, ready, warning) => new Promise((resolve) => {
    if (ready?.()) return resolve();
    const script = document.createElement("script");
    script.src = src;
    script.async = false;
    script.onload = resolve;
    script.onerror = () => {
      console.warn(warning);
      resolve();
    };
    document.head.appendChild(script);
  });

  const loadExtensions = async () => {
    await loadScript(
      "/ide-foundation.js",
      () => K.__ideFoundationInstalled,
      "[TL Studio] Browser IDE reconciliation extension failed to load",
    );
    await loadScript(
      "/editor-enhancements.js",
      () => K.__editorEnhancementsInstalled,
      "[TL Studio] Editor enhancements failed to load",
    );
    await loadScript(
      "/terminal.js",
      () => K.__terminalInstalled,
      "[TL Studio] Integrated terminal failed to load",
    );
    await loadScript(
      "/preview.js",
      () => K.__previewInstalled,
      "[TL Studio] Live preview failed to load",
    );
    await loadScript(
      "/preview-floating.js",
      () => K.__previewFloatingInstalled,
      "[TL Studio] Floating preview layout failed to load",
    );
    await loadScript(
      "/search.js",
      () => K.__projectSearchInstalled,
      "[TL Studio] Project search UI failed to load",
    );
    await loadScript(
      "/settings-enhancements.js",
      () => K.__settingsEnhancementsInstalled,
      "[TL Studio] Editor and font settings failed to load",
    );
    await loadScript(
      "/attachments.js",
      () => K.__attachmentsInstalled,
      "[TL Studio] Composer attachments failed to load",
    );
    await loadScript(
      "/diagnostics-ui.js",
      () => K.__diagnosticsUiInstalled,
      "[TL Studio] Session diagnostics UI failed to load",
    );
    await loadScript(
      "/provider-recovery-ui.js",
      () => K.__providerRecoveryInstalled,
      "[TL Studio] Provider recovery UI failed to load",
    );
    await loadScript(
      "/providers-ui.js",
      () => K.__providersUiInstalled,
      "[TL Studio] Custom provider settings UI failed to load",
    );
    await loadScript(
      "/providers-settings-bridge.js",
      () => K.__providersSettingsBridgeInstalled,
      "[TL Studio] Custom provider settings bridge failed to load",
    );
    await loadScript(
      "/legacy-sessions.js",
      () => K.__legacySessionsInstalled,
      "[TL Studio] Legacy session recovery failed to load",
    );
  };

  loadExtensions().finally(init);
})();
