import { K } from "./kernel";

(() => {
  "use strict";

  
  if (!K || K.__attachmentsInstalled) return;

  const MAX_FILE_BYTES = 10 * 1024 * 1024;
  const MAX_TOTAL_BYTES = 20 * 1024 * 1024;
  const MAX_FILES = 8;
  const IMAGE_MIMES = new Set(["image/png", "image/jpeg", "image/webp", "image/gif"]);
  const TEXT_MIMES = new Set([
    "application/json",
    "application/ld+json",
    "application/javascript",
    "application/x-javascript",
    "application/xml",
    "application/yaml",
    "application/x-yaml",
    "application/sql",
  ]);
  const TEXT_EXTENSIONS = new Set([
    "txt", "md", "mdx", "json", "jsonl", "yaml", "yml", "toml", "xml", "csv", "tsv",
    "js", "jsx", "mjs", "cjs", "ts", "tsx", "css", "scss", "sass", "less", "html", "htm",
    "py", "pyi", "go", "rs", "java", "kt", "kts", "c", "h", "cc", "cpp", "cxx", "hpp",
    "cs", "php", "rb", "swift", "scala", "sh", "bash", "zsh", "fish", "ps1", "bat", "cmd",
    "sql", "graphql", "gql", "proto", "ini", "conf", "config", "env", "log", "diff", "patch",
    "dockerfile", "gitignore", "editorconfig", "properties", "gradle", "vue", "svelte", "astro",
  ]);

  K.state.attachments = Array.isArray(K.state.attachments) ? K.state.attachments : [];

  const css = document.createElement("link");
  css.rel = "stylesheet";
  css.href = "/attachments.css";
  document.head.appendChild(css);

  const composer = document.querySelector<HTMLElement>(".composer");
  const bottom = composer?.querySelector<HTMLElement>(".composer-bottom");
  const controls = bottom?.querySelector<HTMLElement>(".composer-context-controls");
  if (!composer || !bottom || !controls || !K.els.prompt) return;

  const tray = document.createElement("div");
  tray.id = "composerAttachments";
  tray.className = "composer-attachments hidden";
  tray.setAttribute("aria-live", "polite");
  bottom.before(tray);

  const attachButton = document.createElement("button");
  attachButton.id = "attachButton";
  attachButton.type = "button";
  attachButton.className = "attach-button";
  attachButton.textContent = "+";
  attachButton.title = "Attach files";
  attachButton.setAttribute("aria-label", "Attach files");

  const attachmentInput = document.createElement("input");
  attachmentInput.id = "attachmentInput";
  attachmentInput.type = "file";
  attachmentInput.multiple = true;
  attachmentInput.hidden = true;
  attachmentInput.accept = [
    "image/png", "image/jpeg", "image/webp", "image/gif", "application/pdf", "text/*",
    ".md", ".mdx", ".json", ".jsonl", ".yaml", ".yml", ".toml", ".xml", ".csv",
    ".js", ".jsx", ".ts", ".tsx", ".py", ".go", ".rs", ".java", ".c", ".cpp", ".h",
    ".css", ".html", ".sql", ".sh", ".ps1", ".diff", ".patch", ".env",
  ].join(",");

  controls.prepend(attachmentInput);
  controls.prepend(attachButton);
  K.els.attachButton = attachButton;
  K.els.attachmentInput = attachmentInput;
  K.els.composerAttachments = tray;

  const fileExtension = (name: any) => {
    const clean = String(name || "").toLowerCase();
    if (!clean.includes(".")) return clean;
    return clean.split(".").pop() || "";
  };

  const normalizeMime = (file: any) => {
    const type = String(file?.type || "").toLowerCase();
    const extension = fileExtension(file?.name);
    if (IMAGE_MIMES.has(type)) return type;
    if (type === "application/pdf" || extension === "pdf") return "application/pdf";
    if (type.startsWith("text/") || TEXT_MIMES.has(type) || TEXT_EXTENSIONS.has(extension)) return "text/plain";
    if (extension === "svg") return "text/plain";
    return "";
  };

  const formatBytes = (bytes: any) => {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${Math.ceil(bytes / 1024)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(bytes < 10 * 1024 * 1024 ? 1 : 0)} MB`;
  };

  const kindLabel = (mime: any) => {
    if (mime === "application/pdf") return "PDF";
    if (mime.startsWith("image/")) return "IMG";
    return "TXT";
  };

  const readAsDataURL = (file: any, mime: any) => new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error || new Error(`Could not read ${file.name}`));
    reader.onload = () => {
      const raw = String(reader.result || "");
      const comma = raw.indexOf(",");
      if (comma < 0) return reject(new Error(`Could not encode ${file.name}`));
      resolve(`data:${mime};base64,${raw.slice(comma + 1)}`);
    };
    reader.readAsDataURL(file);
  });

  const attachmentID = () => globalThis.crypto?.randomUUID?.() || `attachment-${Date.now()}-${Math.random().toString(16).slice(2)}`;

  K.renderAttachments = () => {
    tray.textContent = "";
    const attachments = K.state.attachments;
    tray.classList.toggle("hidden", !attachments.length);
    for (const attachment of attachments) {
      const chip = document.createElement("div");
      chip.className = "composer-attachment";

      const icon = document.createElement("span");
      icon.className = "composer-attachment-icon";
      icon.textContent = kindLabel(attachment.mime);

      const copy = document.createElement("span");
      copy.className = "composer-attachment-copy";
      const name = document.createElement("strong");
      const meta = document.createElement("small");
      name.textContent = attachment.name;
      name.title = attachment.name;
      meta.textContent = `${formatBytes(attachment.size)} · ${attachment.mime}`;
      copy.append(name, meta);

      const remove = document.createElement("button");
      remove.type = "button";
      remove.className = "composer-attachment-remove";
      remove.textContent = "×";
      remove.title = `Remove ${attachment.name}`;
      remove.setAttribute("aria-label", `Remove ${attachment.name}`);
      remove.addEventListener("click", () => {
        K.state.attachments = K.state.attachments.filter((item) => item.id !== attachment.id);
        K.renderAttachments();
        K.els.prompt.focus();
      });

      chip.append(icon, copy, remove);
      tray.appendChild(chip);
    }
  };

  K.clearAttachments = () => {
    K.state.attachments = [];
    attachmentInput.value = "";
    K.renderAttachments();
  };

  K.addAttachments = async (files: FileList | File[] | null) => {
    const incoming: File[] = Array.from(files || []);
    if (!incoming.length) return;

    const accepted: TLStudioDynamicRecord[] = [];
    const errors: string[] = [];
    let total = K.state.attachments.reduce((sum, item) => sum + Number(item.size || 0), 0);

    for (const file of incoming) {
      if (K.state.attachments.length + accepted.length >= MAX_FILES) {
        errors.push(`You can attach up to ${MAX_FILES} files at once.`);
        break;
      }
      const mime = normalizeMime(file);
      if (!mime) {
        errors.push(`${file.name}: unsupported file type. Use images, PDF, or text/code files.`);
        continue;
      }
      if (file.size > MAX_FILE_BYTES) {
        errors.push(`${file.name}: larger than the ${formatBytes(MAX_FILE_BYTES)} per-file limit.`);
        continue;
      }
      if (total + file.size > MAX_TOTAL_BYTES) {
        errors.push(`Attachments exceed the ${formatBytes(MAX_TOTAL_BYTES)} total limit.`);
        break;
      }
      try {
        const url = await readAsDataURL(file, mime);
        accepted.push({ id: attachmentID(), name: file.name || "attachment", size: file.size, mime, url });
        total += file.size;
      } catch (error) {
        errors.push(`${file.name}: ${(error as any).message || error}`);
      }
    }

    if (accepted.length) {
      K.state.attachments.push(...accepted);
      K.renderAttachments();
      K.showError("");
    }
    if (errors.length) K.showError(errors[0]);
    attachmentInput.value = "";
  };

  attachButton.addEventListener("click", () => attachmentInput.click());
  attachmentInput.addEventListener("change", () => K.addAttachments(attachmentInput.files));

  let dragDepth = 0;
  composer.addEventListener("dragenter", (event: DragEvent) => {
    if (!event.dataTransfer?.types?.includes("Files")) return;
    event.preventDefault();
    dragDepth += 1;
    composer.classList.add("attachment-dragover");
  });
  composer.addEventListener("dragover", (event: DragEvent) => {
    if (!event.dataTransfer?.types?.includes("Files")) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = "copy";
  });
  composer.addEventListener("dragleave", (event: DragEvent) => {
    if (!event.dataTransfer?.types?.includes("Files")) return;
    dragDepth = Math.max(0, dragDepth - 1);
    if (!dragDepth) composer.classList.remove("attachment-dragover");
  });
  composer.addEventListener("drop", (event: DragEvent) => {
    if (!event.dataTransfer?.files?.length) return;
    event.preventDefault();
    dragDepth = 0;
    composer.classList.remove("attachment-dragover");
    K.addAttachments(event.dataTransfer.files);
  });

  K.els.prompt.addEventListener("paste", (event) => {
    const files = Array.from(event.clipboardData?.files || []);
    if (!files.length) return;
    event.preventDefault();
    K.addAttachments(files);
  });

  const messageAttachments = (message: any) => {
    if (Array.isArray(message?.attachments)) return message.attachments;
    const parts = Array.isArray(message?.parts) ? message.parts : Array.isArray(message?.content) ? message.content : [];
    return parts.filter((part: any) => part?.type === "file").map((part: any) => ({
      name: part.filename || part.name || "Attachment",
      mime: part.mime || "text/plain",
      url: part.url || "",
    }));
  };

  const renderedMessage = (message: any) => {
    if (message?.role) return message.role === "user" || message.role === "assistant";
    if (message?.info && Array.isArray(message.parts)) return message.info.role === "user" || message.info.role === "assistant";
    return ["user", "assistant", "shell", "system", "synthetic"].includes(message?.type);
  };

  const decorateMessageAttachments = () => {
    const rows = Array.from(K.els.conversation.querySelectorAll(":scope > article.message"));
    let rowIndex = 0;
    for (const message of K.state.messages) {
      if (!renderedMessage(message)) continue;
      const row = rows[rowIndex++];
      if (!row) break;
      const files = messageAttachments(message);
      if (!files.length) continue;
      const content = row.querySelector(".message-content");
      if (!content || content.querySelector(".message-attachments")) continue;
      const list = document.createElement("div");
      list.className = "message-attachments";
      for (const file of files) {
        const item = document.createElement("span");
        item.className = "message-attachment";
        const type = document.createElement("span");
        type.textContent = kindLabel(file.mime || "text/plain");
        const name = document.createElement("strong");
        name.textContent = file.name || "Attachment";
        name.title = file.name || "Attachment";
        item.append(type, name);
        list.appendChild(item);
      }
      content.appendChild(list);
    }
  };

  const baseRenderMessages = K.renderMessages;
  K.renderMessages = () => {
    baseRenderMessages();
    decorateMessageAttachments();
  };

  K.sendPrompt = async () => {
    const text = K.els.prompt.value.trim();
    const attachments = K.state.attachments.slice();
    if ((!text && !attachments.length) || K.state.sending) return;
    K.showError("");
    K.state.sending = true;
    K.els.sendButton.disabled = true;
    const startedAt = Date.now();
    try {
      if (!K.state.session) await K.createSession();
      K.ensureSessionSelection();
      const agent = K.els.agentSelect.value || undefined;
      const model = K.selectedModel();
      const parts = [
        { type: "text", text },
        ...attachments.map((item) => ({ type: "file", mime: item.mime, filename: item.name, url: item.url })),
      ];

      K.els.prompt.value = "";
      K.resizePrompt();
      K.state.messages.push({
        role: "user",
        createdAt: startedAt,
        agent: agent || "",
        model: model ? { providerID: model.providerID, id: model.id } : undefined,
        text,
        activities: [],
        attachments: attachments.map((item) => ({ name: item.name, mime: item.mime, url: item.url })),
        usage: { input: 0, output: 0, reasoning: 0, cacheRead: 0, cacheWrite: 0 },
        changes: [],
      });
      K.renderMessages();

      await K.api.sessionCommands.run(K.state.session!.id, { text, parts, agent, model, variant: model?.variant });
      K.clearAttachments();
      await Promise.all([K.loadMessages(), K.loadActiveSessions(), K.loadAttention?.()]);
      K.renderMessages();
      K.startSessionPolling(startedAt);
    } catch (err) {
      K.state.sending = false;
      K.showError((err as any).message || String(err));
      K.renderMessages();
    } finally {
      K.els.sendButton.disabled = false;
      K.els.prompt.focus();
    }
  };

  const baseNewSession = K.newSession;
  K.newSession = () => {
    K.clearAttachments();
    return baseNewSession();
  };

  if (typeof K.afterProjectChange === "function") {
    const baseAfterProjectChange = K.afterProjectChange;
    K.afterProjectChange = async (...args) => {
      K.clearAttachments();
      return baseAfterProjectChange(...args);
    };
  }

  K.__attachmentsInstalled = true;
})();
