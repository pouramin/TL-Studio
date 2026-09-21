(() => {
  "use strict";
  const K = window.KLU;
  if (!K || K.__projectSearchInstalled) return;
  K.__projectSearchInstalled = true;

  const style = document.createElement("style");
  style.textContent = `
    .project-search-panel{position:fixed;top:68px;right:24px;bottom:96px;width:min(560px,calc(100vw - 48px));z-index:70;display:flex;flex-direction:column;background:var(--panel,#11151c);border:1px solid var(--border,#2a313d);border-radius:14px;box-shadow:0 18px 50px rgba(0,0,0,.28);overflow:hidden}
    .project-search-panel.hidden{display:none}
    .project-search-head{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:14px 16px;border-bottom:1px solid var(--border,#2a313d)}
    .project-search-head-copy{display:flex;flex-direction:column;gap:2px;min-width:0}.project-search-head-copy strong{font-size:14px}.project-search-head-copy span{font-size:11px;color:var(--muted,#8e98a7)}
    .project-search-form{display:grid;gap:10px;padding:14px 16px;border-bottom:1px solid var(--border,#2a313d)}
    .project-search-input{width:100%;box-sizing:border-box;border:1px solid var(--border,#2a313d);background:var(--input,#0d1117);color:inherit;border-radius:9px;padding:10px 11px;font:inherit;outline:none}.project-search-input:focus{border-color:var(--accent,#6aa7ff);box-shadow:0 0 0 2px rgba(106,167,255,.12)}
    .project-search-options{display:grid;grid-template-columns:1fr 1fr;gap:8px}.project-search-options input{min-width:0}
    .project-search-case{display:flex;align-items:center;gap:7px;font-size:12px;color:var(--muted,#8e98a7);user-select:none}.project-search-case input{accent-color:var(--accent,#6aa7ff)}
    .project-search-meta{display:flex;align-items:center;justify-content:space-between;gap:10px;min-height:22px;padding:8px 16px;font-size:11px;color:var(--muted,#8e98a7);border-bottom:1px solid var(--border,#2a313d)}
    .project-search-results{flex:1;overflow:auto;padding:8px}.project-search-empty{padding:24px 16px;text-align:center;color:var(--muted,#8e98a7);font-size:13px}
    .project-search-file{border:1px solid transparent;border-radius:9px;margin-bottom:8px;overflow:hidden}.project-search-file-head{display:flex;justify-content:space-between;gap:12px;padding:8px 10px;background:rgba(127,127,127,.07);font-size:12px}.project-search-file-head strong{font-family:var(--code-font,ui-monospace,SFMono-Regular,Consolas,monospace);font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.project-search-file-head span{color:var(--muted,#8e98a7);white-space:nowrap}
    .project-search-match{width:100%;display:grid;grid-template-columns:54px 1fr;gap:10px;text-align:left;border:0;border-top:1px solid var(--border,#2a313d);background:transparent;color:inherit;padding:8px 10px;cursor:pointer}.project-search-match:hover,.project-search-match:focus-visible{background:rgba(106,167,255,.1);outline:none}.project-search-loc{font-family:var(--code-font,ui-monospace,SFMono-Regular,Consolas,monospace);font-size:11px;color:var(--muted,#8e98a7);padding-top:1px}.project-search-snippet{font-family:var(--code-font,ui-monospace,SFMono-Regular,Consolas,monospace);font-size:12px;line-height:1.45;white-space:pre-wrap;overflow-wrap:anywhere}
    @media(max-width:700px){.project-search-panel{top:58px;right:10px;bottom:84px;width:calc(100vw - 20px)}.project-search-options{grid-template-columns:1fr}}
  `;
  document.head.appendChild(style);

  const toolbar = document.querySelector(".toolbar-controls");
  const mainPane = document.querySelector(".main-pane");
  if (!toolbar || !mainPane) return;

  const button = document.createElement("button");
  button.id = "searchButton";
  button.type = "button";
  button.className = "ghost small";
  button.textContent = "Search";
  button.title = "Search project files (Ctrl/Cmd+Shift+F)";
  toolbar.insertBefore(button, document.getElementById("changesButton") || toolbar.firstChild);

  const panel = document.createElement("aside");
  panel.id = "projectSearchPanel";
  panel.className = "project-search-panel hidden";
  panel.setAttribute("aria-label", "Project search");
  panel.innerHTML = `
    <div class="project-search-head">
      <div class="project-search-head-copy"><strong>Search project</strong><span>Search file contents in the selected project</span></div>
      <button id="closeProjectSearch" class="icon-button" type="button" aria-label="Close search">×</button>
    </div>
    <div class="project-search-form">
      <input id="projectSearchQuery" class="project-search-input" type="search" autocomplete="off" spellcheck="false" placeholder="Search text…" aria-label="Search text" />
      <div class="project-search-options">
        <input id="projectSearchInclude" class="project-search-input" autocomplete="off" spellcheck="false" placeholder="Include: *.js, src/*" aria-label="Include files" />
        <input id="projectSearchExclude" class="project-search-input" autocomplete="off" spellcheck="false" placeholder="Exclude: *.min.js" aria-label="Exclude files" />
      </div>
      <label class="project-search-case"><input id="projectSearchCase" type="checkbox" /> Match case</label>
    </div>
    <div class="project-search-meta"><span id="projectSearchStatus">Type to search the current project</span><span id="projectSearchCount"></span></div>
    <div id="projectSearchResults" class="project-search-results"></div>`;
  mainPane.appendChild(panel);

  const query = document.getElementById("projectSearchQuery") as HTMLInputElement;
  const include = document.getElementById("projectSearchInclude") as HTMLInputElement;
  const exclude = document.getElementById("projectSearchExclude") as HTMLInputElement;
  const caseSensitive = document.getElementById("projectSearchCase") as HTMLInputElement;
  const status = document.getElementById("projectSearchStatus")!;
  const count = document.getElementById("projectSearchCount")!;
  const results = document.getElementById("projectSearchResults")!;
  const close = document.getElementById("closeProjectSearch") as HTMLButtonElement;

  let timer = 0;
  let controller: AbortController | null = null;
  let generation = 0;

  const clearResults = (message = "Type to search the current project") => {
    results.textContent = "";
    const empty = document.createElement("div");
    empty.className = "project-search-empty";
    empty.textContent = message;
    results.appendChild(empty);
    status.textContent = message;
    count.textContent = "";
  };

  const renderResults = (payload: any) => {
    results.textContent = "";
    const files = Array.isArray(payload?.files) ? payload.files : [];
    const matchCount = Number(payload?.matchCount || 0);
    const fileCount = Number(payload?.fileCount || files.length || 0);
    status.textContent = matchCount ? `${matchCount} match${matchCount === 1 ? "" : "es"} in ${fileCount} file${fileCount === 1 ? "" : "s"}` : "No results";
    count.textContent = payload?.truncated ? "Result limit reached" : "";
    if (!files.length) {
      const empty = document.createElement("div");
      empty.className = "project-search-empty";
      empty.textContent = "No matching text found in searchable project files.";
      results.appendChild(empty);
      return;
    }

    for (const file of files) {
      const group = document.createElement("section");
      group.className = "project-search-file";
      const head = document.createElement("div");
      head.className = "project-search-file-head";
      const path = document.createElement("strong");
      path.textContent = file.path || "";
      path.title = file.path || "";
      const total = document.createElement("span");
      total.textContent = `${Array.isArray(file.matches) ? file.matches.length : 0}`;
      head.append(path, total);
      group.appendChild(head);

      for (const match of Array.isArray(file.matches) ? file.matches : []) {
        const row = document.createElement("button");
        row.type = "button";
        row.className = "project-search-match";
        row.title = `Open ${file.path}:${match.line || 1}`;
        const location = document.createElement("span");
        location.className = "project-search-loc";
        location.textContent = `${match.line || 1}:${match.column || 1}`;
        const snippet = document.createElement("span");
        snippet.className = "project-search-snippet";
        snippet.textContent = match.text || match.match || "";
        row.append(location, snippet);
        row.addEventListener("click", async () => {
          panel.classList.add("hidden");
          if (typeof K.openWorkspaceFileAt !== "function") {
            K.showError("Workspace editor navigation is unavailable.");
            return;
          }
          await K.openWorkspaceFileAt({ path: file.path, line: match.line, column: match.column, match: match.match });
        });
        group.appendChild(row);
      }
      results.appendChild(group);
    }
  };

  const runSearch = async () => {
    const text = query.value;
    controller?.abort();
    controller = null;
    const currentGeneration = ++generation;
    if (!text.trim()) {
      clearResults();
      return;
    }

    const params = new URLSearchParams({ q: text });
    if (include.value.trim()) params.set("include", include.value.trim());
    if (exclude.value.trim()) params.set("exclude", exclude.value.trim());
    if (caseSensitive.checked) params.set("caseSensitive", "true");
    controller = new AbortController();
    status.textContent = "Searching…";
    count.textContent = "";
    try {
      const response = await fetch(`/local/search?${params}`, { cache: "no-store", signal: controller.signal });
      const payload = await response.json().catch(() => null);
      if (currentGeneration !== generation) return;
      if (!response.ok) throw new Error(payload?.error || `${response.status} ${response.statusText}`);
      renderResults(payload || {});
    } catch (error) {
      if ((error as any)?.name === "AbortError" || currentGeneration !== generation) return;
      clearResults((error as any)?.message || String(error));
    }
  };

  const scheduleSearch = () => {
    window.clearTimeout(timer);
    timer = window.setTimeout(runSearch, 180);
  };

  const openPanel = () => {
    document.getElementById("filesPanel")?.classList.add("hidden");
    document.getElementById("changesPanel")?.classList.add("hidden");
    panel.classList.remove("hidden");
    window.setTimeout(() => {
      query.focus();
      query.select();
    }, 0);
  };

  const closePanel = () => {
    panel.classList.add("hidden");
    controller?.abort();
  };

  button.addEventListener("click", openPanel);
  close.addEventListener("click", closePanel);
  for (const input of [query, include, exclude]) input.addEventListener("input", scheduleSearch);
  caseSensitive.addEventListener("change", scheduleSearch);
  document.addEventListener("keydown", (event) => {
    if ((event.ctrlKey || event.metaKey) && event.shiftKey && event.key.toLowerCase() === "f") {
      event.preventDefault();
      openPanel();
      return;
    }
    if (event.key === "Escape" && !panel.classList.contains("hidden")) closePanel();
  });

  const previousAfterProjectChange = K.afterProjectChange;
  K.afterProjectChange = async (...args) => {
    controller?.abort();
    query.value = "";
    include.value = "";
    exclude.value = "";
    caseSensitive.checked = false;
    clearResults();
    panel.classList.add("hidden");
    return previousAfterProjectChange(...args);
  };

  clearResults();
})();
