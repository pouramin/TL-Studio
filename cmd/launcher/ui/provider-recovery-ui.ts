(() => {
  "use strict";

  const K = window.KLU;
  if (!K || K.__providerRecoveryInstalled) return;
  K.__providerRecoveryInstalled = true;

  const baseRenderMessages = K.renderMessages;
  const RESUME_PROMPT = "Continue the current task from the existing workspace state. Inspect what is already complete, do not repeat finished work, and finish the user's latest request.";

  const messageRole = (message) => message?.role || message?.info?.role || message?.type || "";
  const errorOf = (message) => message?.error || message?.info?.error || null;
  const errorData = (error) => error?.data && typeof error.data === "object" ? error.data : (error || {});

  const text = (value) => String(value ?? "").trim();
  const finiteStatus = (value) => {
    const number = Number(value);
    return Number.isFinite(number) && number > 0 ? number : 0;
  };

  const clearlyNonRetryable = (value) => /context (?:window|length)|quota exceeded|insufficient quota|invalid prompt|usage not included|freeusagelimiterror|unauthori[sz]ed|forbidden|authentication|reauthenticate|content filter/i.test(value);
  const transientMessage = (value) => /provider returned error|upstream|temporar(?:y|ily) unavailable|server (?:is )?(?:error|overloaded)|overloaded|rate limit|too many requests|fetch failed|connection (?:reset|closed|dropped)|response stream|timed out|timeout/i.test(value);

  const providerErrorInfo = (message) => {
    if (messageRole(message) !== "assistant") return null;
    const error = errorOf(message);
    if (!error || typeof error !== "object") return null;

    const data = errorData(error);
    const name = text(error.type || error.name);
    const messageText = text(error.message || data.message);
    const responseBody = text(error.responseBody || data.responseBody);
    const statusCode = finiteStatus(error.statusCode || data.statusCode);
    const combined = `${messageText}\n${responseBody}`;

    if (name === "MessageAbortedError" || name === "ProviderAuthError" || name === "ContextOverflowError" || clearlyNonRetryable(combined)) {
      return { retryable: false, name, message: messageText, statusCode, responseBody };
    }

    const apiError = name === "APIError";
    const retryable = error.retryable === true
      || (apiError && data.isRetryable === true)
      || (statusCode >= 500 && statusCode < 600)
      || transientMessage(combined);

    if (!retryable) return { retryable: false, name, message: messageText, statusCode, responseBody };
    return {
      retryable: true,
      name,
      message: messageText || "Provider request failed",
      statusCode,
      responseBody,
    };
  };

  const assistantEntries = () => (Array.isArray(K.state.messages) ? K.state.messages : [])
    .map((message, index) => ({ message, index }))
    .filter(({ message }) => messageRole(message) === "assistant");

  const resumeProviderFailure = async (button) => {
    const session = K.state.session;
    if (!session || K.state.sending || K.isSessionRunning?.(session.id)) return false;
    if (!K.els?.prompt || !K.sendPrompt) return false;

    const previousLabel = button?.textContent || "Resume";
    if (button) {
      button.disabled = true;
      button.textContent = "Resuming…";
    }
    K.showError?.("");

    try {
      K.els.prompt.value = RESUME_PROMPT;
      K.resizePrompt?.();
      await K.sendPrompt();
      return true;
    } catch (error) {
      K.showError?.(`Resume failed: ${error.message || String(error)}`);
      return false;
    } finally {
      if (button?.isConnected) {
        button.disabled = false;
        button.textContent = previousLabel;
      }
    }
  };

  const decorateProviderFailures = () => {
    const view = K.els?.conversation;
    if (!view) return;

    const rows = [...view.querySelectorAll(".message.assistant:not(.working-message)")];
    const entries = assistantEntries();
    rows.forEach((row, rowIndex) => {
      if (!row.classList.contains("error")) return;
      const content = row.querySelector(".message-content");
      if (!content || content.querySelector(".timeout-recovery") || content.querySelector(".provider-recovery")) return;

      const entry = entries[rowIndex];
      const info = entry ? providerErrorInfo(entry.message) : null;
      if (!info?.retryable) return;

      const recovery = document.createElement("div");
      recovery.className = "timeout-recovery provider-recovery";

      const copy = document.createElement("div");
      copy.className = "timeout-recovery-copy";
      const summary = document.createElement("span");
      summary.textContent = "The provider request ended after the runtime retry cycle. Existing file changes are preserved.";
      const meta = document.createElement("span");
      meta.className = "timeout-recovery-meta";
      meta.textContent = [
        "Retryable provider error",
        info.statusCode ? `HTTP ${info.statusCode}` : "",
        info.message && !/^provider returned error$/i.test(info.message) ? info.message : "",
      ].filter(Boolean).join(" · ");
      if (info.responseBody) {
        recovery.title = info.responseBody;
        meta.title = info.responseBody;
      }
      copy.append(summary, meta);

      const button = document.createElement("button");
      button.type = "button";
      button.className = "timeout-resume-button provider-resume-button";
      button.textContent = "Resume";
      button.title = "Continue the task from the preserved workspace state";
      button.addEventListener("click", () => resumeProviderFailure(button));

      recovery.append(copy, button);
      content.appendChild(recovery);
    });
  };

  K.renderMessages = (...args) => {
    const result = baseRenderMessages(...args);
    decorateProviderFailures();
    return result;
  };

  K.__providerRecovery = {
    providerErrorInfo,
    resumeProviderFailure,
  };

  K.renderMessages();
})();
