import { K } from "./kernel";

(() => {
  "use strict";
  

  K.cancelAuth = () => {
    const providerID = K.state.authProviderID;
    const loginID = K.state.authLoginID;
    K.state.authController?.abort();
    K.state.authController = null;
    K.state.authProviderID = "";
    K.state.authLoginID = "";
    if (providerID && loginID) {
      void K.api.providerAccounts.cancelLogin(providerID, loginID).catch(() => {});
    }
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
    const scope = K.viewScope.captureSession();
    const sessionID = K.state.session.id;
    const [p, q] = await Promise.allSettled([
      K.api.permissions.list(sessionID),
      K.api.questions.list(sessionID),
    ]);
    if (!K.viewScope.isSessionCurrent(scope)) return;
    const permissions = p.status === "fulfilled" ? p.value : [];
    const questions = q.status === "fulfilled" ? q.value : [];
    if (permissions[0]) return showPermission(permissions[0], sessionID, scope);
    if (questions[0]) return showQuestion(questions[0], sessionID, scope);
    if (K.els.attentionDialog.open) {
      K.els.attentionDialog.close();
      K.state.attentionKey = "";
    }
  };

  const showPermission = (item: any, sessionID: string, scope: { project: number; session: number }) => {
    const key = `permission:${item.id}`;
    if (K.state.attentionKey === key && K.els.attentionDialog.open) return;
    K.state.attentionKey = key;
    reset("Permission request");

    const action = document.createElement("p");
    action.className = "attention-summary";
    action.textContent = `Agent requests permission: ${item.action || item.permission || "perform an action"}`;

    const patterns = Array.isArray(item.patterns) ? item.patterns : Array.isArray(item.resources) ? item.resources : [];
    const alwaysRules = Array.isArray(item.always) ? item.always.filter(Boolean) : [];
    const metadata = item.metadata && typeof item.metadata === "object" ? item.metadata : {};
    const sensitive = metadata.skillShell === true || metadata.sandboxEscalation === true;
    const canAlways = !sensitive && metadata.disableAlways !== true && alwaysRules.length > 0;
    const taskScope = metadata.taskScope === "project-files" || metadata.taskScope === "project-shell" ? metadata.taskScope : "";

    const details = document.createElement("div");
    details.className = "attention-permission-details";
    const label = document.createElement("strong");
    label.className = "attention-detail-label";
    label.textContent = "Requested operation";
    const command = document.createElement("pre");
    command.className = "attention-command-preview";
    command.textContent = patterns.join("\n") || item.action || "Workspace operation";
    details.append(label, command);
    K.els.attentionBody.append(action, details);

    if (taskScope) {
      const scopeInfo = document.createElement("p");
      scopeInfo.className = taskScope === "project-shell" ? "attention-scope-note attention-scope-warning" : "attention-scope-note";
      scopeInfo.textContent = metadata.taskScopeDescription || "Approval applies to this Agent run only.";
      K.els.attentionBody.appendChild(scopeInfo);
    }

    const actions = [
      actionButton("Reject", "ghost", () => replyPermission(item, "reject", sessionID, scope)),
      actionButton("Allow once", "ghost", () => replyPermission(item, "once", sessionID, scope)),
    ];
    if (taskScope) {
      actions.push(actionButton(taskScope === "project-shell" ? "Allow commands for this task" : "Allow file edits for this task", "primary", () => replyPermission(item, "task", sessionID, scope)));
    } else if (canAlways) {
      actions.push(actionButton("Always allow this operation", "primary", () => replyPermission(item, "always", sessionID, scope)));
    }
    K.els.attentionActions.append(...actions);

    if (!K.els.attentionDialog.open) K.els.attentionDialog.showModal();
  };

  const replyPermission = async (item: any, reply: any, sessionID: string, scope: { project: number; session: number }) => {
    if (!K.viewScope.isSessionCurrent(scope)) return;
    try {
      await K.api.permissions.reply(sessionID, item.id, reply);
      if (!K.viewScope.isSessionCurrent(scope)) return;
      if (reply === "always") await K.refreshPermissionRules?.();
      if (!K.viewScope.isSessionCurrent(scope)) return;
      K.state.attentionKey = "";
      if (K.els.attentionDialog.open) K.els.attentionDialog.close();
      await K.loadAttention();
    } catch (err) {
      if (K.viewScope.isSessionCurrent(scope)) K.showError((err as any).message || String(err));
    }
  };

  const showQuestion = (item: any, sessionID: string, scope: { project: number; session: number }) => {
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
        input.className = "question-choice-input";
        input.name = name;
        input.value = option.label;
        if (!question.multiple && question.default && option.label === question.default) input.checked = true;
        span.className = "question-choice-text";
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
      actionButton("Reject", "ghost", () => rejectQuestion(item, sessionID, scope)),
      actionButton("Answer", "primary", () => answerQuestion(item, blocks, sessionID, scope)),
    );
    if (!K.els.attentionDialog.open) K.els.attentionDialog.showModal();
  };

  const answerQuestion = async (item: any, blocks: Array<{ controls: HTMLInputElement[]; custom: HTMLInputElement | null }>, sessionID: string, scope: { project: number; session: number }) => {
    if (!K.viewScope.isSessionCurrent(scope)) return;
    const answers = blocks.map(({ controls, custom }) => {
      const selected = controls.filter((input: any) => input.checked).map((input: HTMLInputElement) => input.value);
      const typed = custom?.value.trim();
      if (typed) selected.push(typed);
      return selected;
    });
    if (answers.some((answer: any) => !answer.length)) return K.showError("Answer every agent question before continuing.");

    try {
      await K.api.questions.reply(sessionID, item.id, answers);
      if (!K.viewScope.isSessionCurrent(scope)) return;
      K.state.attentionKey = "";
      if (K.els.attentionDialog.open) K.els.attentionDialog.close();
      await K.loadAttention();
    } catch (err) {
      if (K.viewScope.isSessionCurrent(scope)) K.showError((err as any).message || String(err));
    }
  };

  const rejectQuestion = async (item: any, sessionID: string, scope: { project: number; session: number }) => {
    if (!K.viewScope.isSessionCurrent(scope)) return;
    try {
      await K.api.questions.reject(sessionID, item.id);
      if (!K.viewScope.isSessionCurrent(scope)) return;
      K.state.attentionKey = "";
      if (K.els.attentionDialog.open) K.els.attentionDialog.close();
      await K.loadAttention();
    } catch (err) {
      if (K.viewScope.isSessionCurrent(scope)) K.showError((err as any).message || String(err));
    }
  };
})();
