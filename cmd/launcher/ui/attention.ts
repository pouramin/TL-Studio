(() => {
  "use strict";
  const K = window.KLU;

  const parseDeviceCode = (input: any) => input?.match(/code:\s*([A-Z0-9-]+)/i)?.[1]?.toUpperCase()
    || input?.match(/\b[A-Z0-9]{4,}(?:-[A-Z0-9]{3,})+\b/i)?.[0]?.toUpperCase() || "";

  K.applyHostedAuthStatus = (status) => {
    const normalized = {
      authenticated: status?.authenticated === true,
      type: status?.type || "",
      organizationId: status?.organizationId || "",
    };
    K.state.hostedAuth = normalized;
    if (normalized.authenticated) K.state.connectedProviders.add(K.api.hosted.providerID);
    else K.state.connectedProviders.delete(K.api.hosted.providerID);
    K.renderAccount?.();
    return normalized;
  };

  K.refreshHostedAuthStatus = async () => K.applyHostedAuthStatus(await K.api.hosted.status());

  K.signInHosted = async () => {
    K.showError("");
    let status;
    try {
      status = await K.refreshHostedAuthStatus();
    } catch (err) {
      K.showError(`Unable to verify hosted account state: ${(err as any).message || String(err)}`);
      return;
    }
    if (status.authenticated) return K.showError("The hosted model account is already connected on this computer.");

    K.state.authController?.abort();
    K.state.authController = new AbortController();
    K.state.authURL = "";
    K.els.authOpen.disabled = true;
    K.els.authInstructions.textContent = "Starting secure device authorization…";
    K.els.authCode.textContent = "";
    K.els.authCodeWrap.classList.add("hidden");
    K.els.authDialog.showModal();

    try {
      const info: any = await K.api.hosted.authorize() || {};
      K.state.authURL = info.url || "";
      K.els.authInstructions.textContent = info.instructions || "Authorization is ready. Open the sign-in page to continue.";
      const code = parseDeviceCode(info.instructions);
      if (code) {
        K.els.authCode.textContent = code;
        K.els.authCodeWrap.classList.remove("hidden");
      }
      K.els.authOpen.disabled = !K.state.authURL;

      await K.api.hosted.callback(K.state.authController.signal);
      K.state.authController = null;
      K.els.authInstructions.textContent = "Signed in successfully.";

      await K.api.runtime.dispose();
      await K.loadCatalog();
      await K.refreshHostedAuthStatus();

      window.setTimeout(() => { if (K.els.authDialog.open) K.els.authDialog.close(); }, 650);
    } catch (err) {
      if ((err as any)?.name !== "AbortError") K.els.authInstructions.textContent = `Sign-in failed: ${(err as any).message || String(err)}`;
    }
  };

  K.cancelAuth = () => {
    K.state.authController?.abort();
    K.state.authController = null;
    if (K.els.authDialog.open) K.els.authDialog.close();
  };

  K.copyAuthCode = async () => {
    try { if (K.els.authCode.textContent) await navigator.clipboard.writeText(K.els.authCode.textContent); }
    catch {}
  };

  const actionButton = (label: any, className: any, fn: any) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = className;
    button.textContent = label;
    button.addEventListener("click", fn);
    return button;
  };

  const reset = (title: any) => {
    K.els.attentionTitle.textContent = title;
    K.els.attentionBody.textContent = "";
    K.els.attentionActions.textContent = "";
  };

  K.loadAttention = async () => {
    if (!K.state.session) return;
    const [p, q] = await Promise.allSettled([
      K.api.permissions.list(K.state.session.id),
      K.api.questions.list(K.state.session.id),
    ]);
    const permissions = p.status === "fulfilled" ? p.value : [];
    const questions = q.status === "fulfilled" ? q.value : [];
    if (permissions[0]) return showPermission(permissions[0]);
    if (questions[0]) return showQuestion(questions[0]);
    if (K.els.attentionDialog.open) {
      K.els.attentionDialog.close();
      K.state.attentionKey = "";
    }
  };

  const showPermission = (item: any) => {
    const key = `permission:${item.id}`;
    if (K.state.attentionKey === key && K.els.attentionDialog.open) return;
    K.state.attentionKey = key;
    reset("Permission request");

    const text = document.createElement("div");
    const meta = document.createElement("div");
    text.textContent = `Agent wants permission to ${item.permission || item.action || "perform an action"}.`;
    meta.className = "attention-meta";

    const patterns = Array.isArray(item.patterns) ? item.patterns : Array.isArray(item.resources) ? item.resources : [];
    const alwaysRules = Array.isArray(item.always) ? item.always.filter(Boolean) : [];
    const metadata = item.metadata && typeof item.metadata === "object" ? item.metadata : {};
    const sensitive = metadata.skillShell === true || metadata.sandboxEscalation === true;
    const canAlways = !sensitive && metadata.disableAlways !== true && alwaysRules.length > 0;
    const always = canAlways ? `TL Studio can remember these rules for this project:\n${alwaysRules.join("\n")}` : "";

    meta.textContent = [
      patterns.length ? patterns.join("\n") : "",
      Object.keys(metadata).length ? JSON.stringify(metadata, null, 2) : "",
      always,
    ].filter(Boolean).join("\n\n") || "No additional details.";
    K.els.attentionBody.append(text, meta);

    const actions = [
      actionButton("Reject", "ghost", () => replyPermission(item, "reject")),
      actionButton(sensitive ? "Allow" : "Allow once", "ghost", () => replyPermission(item, "once")),
    ];
    if (canAlways) actions.push(actionButton("Always allow in this project", "primary", () => replyPermission(item, "always")));
    K.els.attentionActions.append(...actions);

    if (!K.els.attentionDialog.open) K.els.attentionDialog.showModal();
  };

  const replyPermission = async (item: any, reply: any) => {
    try {
      await K.api.permissions.reply(K.state.session!.id, item.id, reply);
      if (reply === "always") await K.refreshPermissionRules?.();
      K.state.attentionKey = "";
      if (K.els.attentionDialog.open) K.els.attentionDialog.close();
      await K.loadAttention();
    } catch (err) { K.showError((err as any).message || String(err)); }
  };

  const showQuestion = (item: any) => {
    const key = `question:${item.id}`;
    if (K.state.attentionKey === key && K.els.attentionDialog.open) return;
    K.state.attentionKey = key;
    reset("Agent question");

    const blocks: Array<{ controls: HTMLInputElement[]; custom: HTMLInputElement | null }> = [];
    for (const [index, question] of (item.questions || []).entries()) {
      const block = document.createElement("div");
      const header = document.createElement("div");
      const title = document.createElement("strong");
      block.className = "question-block";
      header.className = "attention-kicker";
      header.textContent = question.header || `Question ${index + 1}`;
      title.textContent = question.question || `Question ${index + 1}`;
      block.append(header, title);

      const controls: HTMLInputElement[] = [];
      const name = `q-${item.id}-${index}`;
      for (const option of question.options || []) {
        const label = document.createElement("label");
        const input = document.createElement("input");
        const span = document.createElement("span");
        label.className = "question-option";
        input.type = question.multiple ? "checkbox" : "radio";
        input.name = name;
        input.value = option.label;
        if (!question.multiple && question.default && option.label === question.default) input.checked = true;
        span.textContent = option.description ? `${option.label} — ${option.description}` : option.label;
        label.append(input, span);
        block.appendChild(label);
        controls.push(input);
      }

      let custom: HTMLInputElement | null = null;
      if (question.custom !== false) {
        custom = document.createElement("input");
        custom.className = "question-custom";
        custom.placeholder = "Or type a custom answer";
        block.appendChild(custom);
      }
      blocks.push({ controls, custom });
      K.els.attentionBody.appendChild(block);
    }

    K.els.attentionActions.append(
      actionButton("Reject", "ghost", () => rejectQuestion(item)),
      actionButton("Answer", "primary", () => answerQuestion(item, blocks)),
    );
    if (!K.els.attentionDialog.open) K.els.attentionDialog.showModal();
  };

  const answerQuestion = async (item: any, blocks: Array<{ controls: HTMLInputElement[]; custom: HTMLInputElement | null }>) => {
    const answers = blocks.map(({ controls, custom }) => {
      const selected = controls.filter((input: any) => input.checked).map((input: HTMLInputElement) => input.value);
      const typed = custom?.value.trim();
      if (typed) selected.push(typed);
      return selected;
    });
    if (answers.some((answer: any) => !answer.length)) return K.showError("Answer every agent question before continuing.");

    try {
      await K.api.questions.reply(K.state.session!.id, item.id, answers);
      K.state.attentionKey = "";
      if (K.els.attentionDialog.open) K.els.attentionDialog.close();
      await K.loadAttention();
    } catch (err) { K.showError((err as any).message || String(err)); }
  };

  const rejectQuestion = async (item: any) => {
    try {
      await K.api.questions.reject(K.state.session!.id, item.id);
      K.state.attentionKey = "";
      if (K.els.attentionDialog.open) K.els.attentionDialog.close();
      await K.loadAttention();
    } catch (err) { K.showError((err as any).message || String(err)); }
  };
})();
