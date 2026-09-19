(() => {
  "use strict";
  const K = window.KLU;
  if (!K || K.__settingsEnhancementsInstalled) return;
  K.__settingsEnhancementsInstalled = true;

  const panel = document.querySelector('[data-settings-panel="general"]');
  if (!panel || document.getElementById("editorThemeSelect")) return;

  const read = (key, fallback) => {
    try { return localStorage.getItem(key) || fallback; }
    catch { return fallback; }
  };
  const write = (key, value) => {
    try { localStorage.setItem(key, value); } catch {}
  };

  const KEYS = {
    theme: "tl-studio.editor-theme",
    uiFont: "tl-studio.ui-font",
    codeFont: "tl-studio.code-font",
    terminalFont: "tl-studio.terminal-font",
  };

  const fonts = {
    system: 'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
    segoe: '"Segoe UI", Arial, sans-serif',
    arial: 'Arial, Helvetica, sans-serif',
    cascadia: '"Cascadia Code", "Cascadia Mono", Consolas, monospace',
    jetbrains: '"JetBrains Mono", "Cascadia Code", Consolas, monospace',
    fira: '"Fira Code", "Cascadia Code", Consolas, monospace',
    consolas: 'Consolas, "Liberation Mono", monospace',
    mono: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
  };

  const apply = () => {
    const root = document.documentElement;
    const theme = read(KEYS.theme, "midnight");
    root.dataset.editorTheme = ["midnight", "github-dark", "solarized", "mono"].includes(theme) ? theme : "midnight";
    root.style.setProperty("--tl-ui-font", fonts[read(KEYS.uiFont, "system")] || fonts.system);
    root.style.setProperty("--tl-code-font", fonts[read(KEYS.codeFont, "cascadia")] || fonts.cascadia);
    root.style.setProperty("--tl-terminal-font", fonts[read(KEYS.terminalFont, "cascadia")] || fonts.cascadia);
    K.refreshEditorHighlight?.();
  };

  const title = document.createElement("div");
  title.className = "settings-section-title";
  title.textContent = "Editor & Fonts";

  const holder = document.createElement("div");
  holder.innerHTML = `
    <div class="settings-row">
      <div class="settings-copy"><strong>Editor color theme</strong><span>Choose syntax colors independently from the main TL Studio appearance.</span></div>
      <select id="editorThemeSelect" aria-label="Editor color theme">
        <option value="midnight">Midnight</option>
        <option value="github-dark">GitHub Dark</option>
        <option value="solarized">Solarized</option>
        <option value="mono">Monochrome</option>
      </select>
    </div>
    <div class="settings-row settings-row-stack">
      <div class="settings-copy"><strong>UI Font</strong><span>Customise the font used throughout the interface.</span></div>
      <div class="settings-control-stack"><select id="uiFontSelect" class="settings-font-preview" aria-label="UI font">
        <option value="system">System Sans</option><option value="segoe">Segoe UI</option><option value="arial">Arial</option>
      </select><span class="settings-control-hint">Uses fonts installed on this computer.</span></div>
    </div>
    <div class="settings-row settings-row-stack">
      <div class="settings-copy"><strong>Code Font</strong><span>Customise the font used in the workspace editor and code blocks.</span></div>
      <div class="settings-control-stack"><select id="codeFontSelect" class="settings-font-preview" aria-label="Code font">
        <option value="cascadia">Cascadia Code</option><option value="jetbrains">JetBrains Mono</option><option value="fira">Fira Code</option><option value="consolas">Consolas</option><option value="mono">System Mono</option>
      </select><span class="settings-control-hint">Falls back automatically when a font is not installed.</span></div>
    </div>
    <div class="settings-row settings-row-stack">
      <div class="settings-copy"><strong>Terminal Font</strong><span>Customise the font used in the integrated terminal.</span></div>
      <div class="settings-control-stack"><select id="terminalFontSelect" class="settings-font-preview" aria-label="Terminal font">
        <option value="cascadia">Cascadia Mono</option><option value="jetbrains">JetBrains Mono</option><option value="fira">Fira Code</option><option value="consolas">Consolas</option><option value="mono">System Mono</option>
      </select><span class="settings-control-hint">Terminal and editor fonts can be different.</span></div>
    </div>
    <div class="settings-row">
      <div class="settings-copy"><strong>Preview window</strong><span>Reset the floating Preview window to its default size and position.</span></div>
      <button id="resetPreviewWindow" class="ghost small" type="button">Reset position</button>
    </div>`;

  panel.append(title, ...holder.children);

  const controls = {
    theme: document.getElementById("editorThemeSelect"),
    uiFont: document.getElementById("uiFontSelect"),
    codeFont: document.getElementById("codeFontSelect"),
    terminalFont: document.getElementById("terminalFontSelect"),
    resetPreview: document.getElementById("resetPreviewWindow"),
  };

  controls.theme.value = read(KEYS.theme, "midnight");
  controls.uiFont.value = read(KEYS.uiFont, "system");
  controls.codeFont.value = read(KEYS.codeFont, "cascadia");
  controls.terminalFont.value = read(KEYS.terminalFont, "cascadia");

  controls.theme.addEventListener("change", () => { write(KEYS.theme, controls.theme.value); apply(); });
  controls.uiFont.addEventListener("change", () => { write(KEYS.uiFont, controls.uiFont.value); apply(); });
  controls.codeFont.addEventListener("change", () => { write(KEYS.codeFont, controls.codeFont.value); apply(); });
  controls.terminalFont.addEventListener("change", () => { write(KEYS.terminalFont, controls.terminalFont.value); apply(); });
  controls.resetPreview.addEventListener("click", () => K.previewWindow?.reset?.());

  apply();
})();
