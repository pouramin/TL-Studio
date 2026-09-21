import { K } from "./kernel";

(() => {
  "use strict";
  

  const safeJSON = (value: any) => {
    try { return JSON.stringify(value, null, 2); }
    catch { return String(value); }
  };

  const errorText = (error: any) => {
    if (!error) return "";
    if (typeof error === "string") return error;
    return String(error.message || error.data?.message || error.error?.message || safeJSON(error));
  };

  const partsOf = (message: any) => Array.isArray(message?.parts)
    ? message.parts
    : Array.isArray(message?.content) ? message.content : [];
  const activitiesOf = (message: any) => Array.isArray(message?.activities) ? message.activities : partsOf(message);

  const textOf = (message: any) => {
    if (typeof message?.text === "string" && message.text) return message.text;
    return partsOf(message)
      .filter((part: any) => part?.type === "text" && !part.ignored)
      .map((part: any) => part.text || "")
      .filter(Boolean)
      .join("\n")
      .trim();
  };

  const normalizeStatus = (input: any) => {
    const value = String(input || "pending").toLowerCase();
    if (["completed", "success", "done"].includes(value)) return "completed";
    if (["error", "failed", "failure"].includes(value)) return "failed";
    if (["running", "in_progress", "active"].includes(value)) return "running";
    return value;
  };

  const fileHint = (item: any) => {
    if (item?.kind === "tool" && Array.isArray(item.changes) && item.changes[0]?.file) {
      const bits = String(item.changes[0].file).split(/[\\/]/);
      return bits[bits.length - 1] || String(item.changes[0].file);
    }
    const state = item?.state || {};
    const metadata = state.metadata || {};
    const input = state.input || {};
    const diff = metadata.filediff || metadata.fileDiff || {};
    const path = input.filePath || input.path || input.file || metadata.filepath || metadata.path || diff.file;
    if (!path) return "";
    const bits = String(path).split(/[\\/]/);
    return bits[bits.length - 1] || String(path);
  };

  const diffHint = (item: any) => {
    if (item?.kind === "tool" && Array.isArray(item.changes) && item.changes.length) {
      const additions = item.changes.reduce((sum: any, change: any) => sum + (Number(change?.additions) || 0), 0);
      const deletions = item.changes.reduce((sum: any, change: any) => sum + (Number(change?.deletions) || 0), 0);
      return `+${additions} −${deletions}`;
    }
    const state = item?.state || {};
    const metadata = state.metadata || {};
    const diff = metadata.filediff || metadata.fileDiff || state.output?.filediff || {};
    const additions = Number(diff.additions);
    const deletions = Number(diff.deletions);
    if (!Number.isFinite(additions) && !Number.isFinite(deletions)) return "";
    return `+${Number.isFinite(additions) ? additions : 0} −${Number.isFinite(deletions) ? deletions : 0}`;
  };

  const toolDescriptor = (item: any) => K.toolDescriptor?.(item?.tool || item?.name || "") || {
    name: "Runtime tool",
    category: "runtime",
    permissionClass: "runtime",
  };

  const toolDetails = (item: any) => {
    if (item?.kind === "tool") {
      const blocks: Array<[string, string]> = [];
      if (item.input && typeof item.input === "object" && Object.keys(item.input).length) blocks.push(["Input", safeJSON(item.input)]);
      if (item.output !== undefined && item.output !== "") blocks.push(["Output", typeof item.output === "string" ? item.output : safeJSON(item.output)]);
      if (item.error) blocks.push(["Error", errorText(item.error)]);
      if (item.metadata && Object.keys(item.metadata).length) blocks.push(["Metadata", safeJSON(item.metadata)]);
      return blocks;
    }
    const state = item?.state || {};
    const blocks: Array<[string, string]> = [];
    if (state.input && Object.keys(state.input).length) blocks.push(["Input", safeJSON(state.input)]);
    const output = state.output ?? state.result ?? item.output ?? item.result;
    if (output !== undefined && output !== "") blocks.push(["Output", typeof output === "string" ? output : safeJSON(output)]);
    if (state.error) blocks.push(["Error", errorText(state.error)]);
    if (state.metadata && Object.keys(state.metadata).length) blocks.push(["Metadata", safeJSON(state.metadata)]);
    return blocks;
  };

  const safeLink = (href: any) => {
    try {
      const url = new URL(href, window.location.href);
      return ["http:", "https:"].includes(url.protocol) ? url.href : "";
    } catch { return ""; }
  };

  const appendInlineMarkdown = (parent: any, input: any) => {
    let text = String(input || "");
    while (text) {
      const codeAt = text.indexOf("`");
      const boldAt = text.indexOf("**");
      const linkAt = text.indexOf("[");
      const candidates = [codeAt, boldAt, linkAt].filter((value) => value >= 0);
      const next = candidates.length ? Math.min(...candidates) : -1;
      if (next < 0) {
        parent.appendChild(document.createTextNode(text));
        break;
      }
      if (next > 0) {
        parent.appendChild(document.createTextNode(text.slice(0, next)));
        text = text.slice(next);
        continue;
      }

      if (text.startsWith("**")) {
        const end = text.indexOf("**", 2);
        if (end > 2) {
          const strong = document.createElement("strong");
          appendInlineMarkdown(strong, text.slice(2, end));
          parent.appendChild(strong);
          text = text.slice(end + 2);
          continue;
        }
      }

      if (text.startsWith("`")) {
        const end = text.indexOf("`", 1);
        if (end > 1) {
          const code = document.createElement("code");
          code.textContent = text.slice(1, end);
          parent.appendChild(code);
          text = text.slice(end + 1);
          continue;
        }
      }

      if (text.startsWith("[")) {
        const labelEnd = text.indexOf("](", 1);
        const hrefEnd = labelEnd > 0 ? text.indexOf(")", labelEnd + 2) : -1;
        if (labelEnd > 1 && hrefEnd > labelEnd + 2) {
          const label = text.slice(1, labelEnd);
          const href = safeLink(text.slice(labelEnd + 2, hrefEnd));
          if (href) {
            const anchor = document.createElement("a");
            anchor.href = href;
            anchor.target = "_blank";
            anchor.rel = "noreferrer noopener";
            appendInlineMarkdown(anchor, label);
            parent.appendChild(anchor);
            text = text.slice(hrefEnd + 1);
            continue;
          }
        }
      }

      parent.appendChild(document.createTextNode(text[0]));
      text = text.slice(1);
    }
  };

  const renderMarkdown = (container: any, input: any) => {
    const lines = String(input || "").replace(/\r\n?/g, "\n").split("\n");
    let index = 0;
    const paragraph: string[] = [];

    const flushParagraph = () => {
      if (!paragraph.length) return;
      const p = document.createElement("p");
      appendInlineMarkdown(p, paragraph.join(" ").trim());
      container.appendChild(p);
      paragraph.length = 0;
    };

    while (index < lines.length) {
      const line = lines[index];
      if (!line.trim()) {
        flushParagraph();
        index++;
        continue;
      }

      const fence = line.match(/^\s*```([^`]*)$/);
      if (fence) {
        flushParagraph();
        const language = fence[1].trim();
        const body: string[] = [];
        index++;
        while (index < lines.length && !/^\s*```\s*$/.test(lines[index])) body.push(lines[index++]);
        if (index < lines.length) index++;
        const pre = document.createElement("pre");
        pre.className = "markdown-code";
        const code = document.createElement("code");
        if (language) code.dataset.language = language;
        code.textContent = body.join("\n");
        pre.appendChild(code);
        container.appendChild(pre);
        continue;
      }

      const heading = line.match(/^(#{1,4})\s+(.+)$/);
      if (heading) {
        flushParagraph();
        const h = document.createElement(`h${Math.min(4, heading[1].length + 1)}`);
        appendInlineMarkdown(h, heading[2]);
        container.appendChild(h);
        index++;
        continue;
      }

      const unordered = line.match(/^\s*[-*]\s+(.+)$/);
      const ordered = line.match(/^\s*\d+[.)]\s+(.+)$/);
      if (unordered || ordered) {
        flushParagraph();
        const orderedList = !!ordered;
        const list = document.createElement(orderedList ? "ol" : "ul");
        while (index < lines.length) {
          const match = orderedList
            ? lines[index].match(/^\s*\d+[.)]\s+(.+)$/)
            : lines[index].match(/^\s*[-*]\s+(.+)$/);
          if (!match) break;
          const li = document.createElement("li");
          appendInlineMarkdown(li, match[1]);
          list.appendChild(li);
          index++;
        }
        container.appendChild(list);
        continue;
      }

      const quote = line.match(/^\s*>\s?(.*)$/);
      if (quote) {
        flushParagraph();
        const blockquote = document.createElement("blockquote");
        const values: string[] = [];
        while (index < lines.length) {
          const match = lines[index].match(/^\s*>\s?(.*)$/);
          if (!match) break;
          values.push(match[1]);
          index++;
        }
        appendInlineMarkdown(blockquote, values.join(" "));
        container.appendChild(blockquote);
        continue;
      }

      paragraph.push(line.trim());
      index++;
    }
    flushParagraph();
  };

  const messageNode = (kind: any, author: any, text: any, time: any, error: any = "") => {
    const row = document.createElement("article");
    row.className = `message ${kind}${error ? " error" : ""}`;

    const avatar = document.createElement("div");
    avatar.className = "avatar";
    avatar.textContent = kind === "user" ? "YOU" : kind === "system" ? "SYS" : "AI";

    const content = document.createElement("div");
    content.className = "message-content";

    const head = document.createElement("div");
    head.className = "message-head";
    const strong = document.createElement("strong");
    strong.textContent = author;
    const stamp = document.createElement("span");
    stamp.textContent = K.formatTime(time);
    head.append(strong, stamp);
    content.appendChild(head);

    if (text || error) {
      const body = document.createElement("div");
      body.className = "message-text";
      body.textContent = error ? `${text}${text ? "\n\n" : ""}${error}` : text;
      content.appendChild(body);
    }

    row.append(avatar, content);
    return row;
  };

  const activityCard = ({ title, status, meta = "", blocks = [], reasoning = false }: { title: string; status: any; meta?: string; blocks?: Array<[string, string]>; reasoning?: boolean }) => {
    const details = document.createElement("details");
    const normalized = normalizeStatus(status);
    details.className = `activity-card${reasoning ? " reasoning-card" : ""}`;
    details.dataset.status = normalized;
    details.open = normalized === "running" || normalized === "failed";

    const summary = document.createElement("summary");
    const icon = document.createElement("span");
    icon.className = "activity-icon";
    icon.textContent = reasoning ? "◌" : normalized === "completed" ? "✓" : normalized === "failed" ? "!" : "•";

    const name = document.createElement("strong");
    name.textContent = title;
    const descriptor = document.createElement("span");
    descriptor.className = "activity-meta";
    descriptor.textContent = meta;
    const state = document.createElement("span");
    state.className = "activity-status";
    state.textContent = normalized;

    summary.append(icon, name, descriptor, state);
    details.appendChild(summary);

    if (blocks.length) {
      const body = document.createElement("div");
      body.className = "activity-body";
      for (const [label, value] of blocks) {
        if (!value) continue;
        const section = document.createElement("section");
        const heading = document.createElement("div");
        const pre = document.createElement("pre");
        heading.className = "activity-label";
        heading.textContent = label;
        pre.textContent = value;
        section.append(heading, pre);
        body.appendChild(section);
      }
      details.appendChild(body);
    }
    return details;
  };

  const activityNode = (item: any) => {
    if (item?.kind === "reasoning" && item.text) {
      return activityCard({
        title: "Reasoning",
        status: item.status || "completed",
        blocks: [["Thought process", item.text]],
        reasoning: true,
      });
    }

    if (item?.kind === "tool") {
      const meta = [item.category, item.permissionClass, fileHint(item), diffHint(item)].filter(Boolean).join(" · ");
      return activityCard({
        title: item.toolName || "Runtime tool",
        status: item.status || "pending",
        meta,
        blocks: toolDetails(item),
      });
    }

    if (item?.kind === "subtask") {
      return activityCard({
        title: "Subtask",
        status: item.status || "created",
        meta: item.agent || "",
        blocks: [["Task", item.text || ""]],
      });
    }

    if (item?.type === "reasoning" && item.text) {
      return activityCard({
        title: "Reasoning",
        status: item.time?.end || item.time?.completed ? "completed" : item.status || "completed",
        blocks: [["Thought process", item.text]],
        reasoning: true,
      });
    }

    if (item?.type === "tool") {
      const state = item.state || {};
      const descriptor = toolDescriptor(item);
      const meta = [descriptor.category, descriptor.permissionClass, fileHint(item), diffHint(item)].filter(Boolean).join(" · ");
      return activityCard({
        title: descriptor.name,
        status: state.status || item.status || "pending",
        meta,
        blocks: toolDetails(item),
      });
    }

    if (item?.type === "subtask") {
      return activityCard({
        title: "Subtask",
        status: item.status || "created",
        meta: item.agent || "",
        blocks: [["Task", item.description || item.prompt || ""]],
      });
    }
    return null;
  };

  const appendAssistantContent = (node: any, message: any, error = "") => {
    const content = node.querySelector(".message-content");
    const parts = activitiesOf(message);

    // Keep operational activity above the user-facing answer. Some providers
    // append reasoning after text in the raw part array even though it belongs
    // to the work phase; presenting it first produces a stable coding-agent UX.
    for (const item of parts) {
      const activity = activityNode(item);
      if (activity) content.appendChild(activity);
    }

    const text = typeof message?.text === "string"
      ? message.text.trim()
      : partsOf(message)
        .filter((part: any) => part?.type === "text" && !part.ignored && part.text)
        .map((part: any) => part.text)
        .join("\n")
        .trim() || textOf(message);

    if (text) {
      const body = document.createElement("div");
      body.className = "message-text markdown-body";
      renderMarkdown(body, text);
      content.appendChild(body);
    }

    if (error) {
      const body = document.createElement("div");
      body.className = "message-text message-error-text";
      body.textContent = error;
      content.appendChild(body);
    }
  };

  const renderEnvelope = (view: any, message: any) => {
    if (message?.role) {
      const time = message.createdAt ?? message.completedAt;
      if (message.role === "user") {
        view.appendChild(messageNode("user", "You", message.text || "", time));
        return true;
      }
      if (message.role === "assistant") {
        const error = errorText(message.error);
        const node = messageNode("assistant", message.agent || "Agent", "", time, "");
        if (error) node.classList.add("error");
        appendAssistantContent(node, message, error);
        view.appendChild(node);
        return true;
      }
      if (message.role === "system") {
        view.appendChild(messageNode("system", "System", message.text || "", time));
        return true;
      }
      return false;
    }

    if (!message?.info || !Array.isArray(message.parts)) return false;
    const info = message.info;
    const time = info.time?.created ?? info.time?.completed;
    if (info.role === "user") {
      view.appendChild(messageNode("user", "You", textOf(message), time));
      return true;
    }
    if (info.role === "assistant") {
      const error = errorText(info.error);
      const node = messageNode("assistant", info.agent || "Agent", "", time, "");
      if (error) node.classList.add("error");
      appendAssistantContent(node, message, error);
      view.appendChild(node);
      return true;
    }
    return false;
  };

  K.renderMessages = () => {
    const view = K.els.conversation;
    view.textContent = "";

    for (const message of K.state.messages) {
      if (renderEnvelope(view, message)) continue;

      if (message?.type === "user") {
        view.appendChild(messageNode("user", "You", message.text || textOf(message), message.time?.created));
      } else if (message?.type === "assistant") {
        const error = errorText(message.error);
        const node = messageNode("assistant", message.agent || "Agent", "", message.time?.created, "");
        if (error) node.classList.add("error");
        appendAssistantContent(node, message, error);
        view.appendChild(node);
      } else if (message?.type === "shell") {
        const node = messageNode("assistant", "Shell", "", message.time?.created);
        node.querySelector<HTMLElement>(".message-content")!.appendChild(activityCard({
          title: message.command || "Command",
          status: message.time?.completed ? "completed" : "running",
          blocks: [["Output", message.output || ""]],
        }));
        view.appendChild(node);
      } else if (message?.type === "system" || message?.type === "synthetic") {
        view.appendChild(messageNode("system", "System", message.text || "", message.time?.created));
      }
    }

    if (K.state.session && (K.isSessionRunning(K.state.session.id) || K.state.sending)) {
      const row = messageNode("assistant", K.state.session.agent || "Agent", "", Date.now());
      row.classList.add("working-message");
      const working = document.createElement("div");
      working.className = "message-text";
      working.append("Working ");
      const typing = document.createElement("span");
      typing.className = "typing";
      typing.append(document.createElement("i"), document.createElement("i"), document.createElement("i"));
      working.appendChild(typing);
      row.querySelector<HTMLElement>(".message-content")!.appendChild(working);
      view.appendChild(row);
    }

    K.refreshWorkspaceControls?.();
    requestAnimationFrame(() => { view.scrollTop = view.scrollHeight; });
  };

  K.presentation = Object.freeze({ partsOf, textOf, renderMarkdown });
})();
