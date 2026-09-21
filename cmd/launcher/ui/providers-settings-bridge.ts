(() => {
  "use strict";

  const K = window.KLU;
  if (!K || K.__providersSettingsBridgeInstalled) return;
  K.__providersSettingsBridgeInstalled = true;

  const dialog = document.getElementById("settingsDialog");
  const panel = dialog?.querySelector('[data-settings-panel="providers"]');
  if (!dialog || !panel) return;

  // product-ui.js captures the original General/About panel list before the
  // Providers extension is injected. Keep the dynamically added panel in sync
  // when the user navigates back to an original settings section.
  for (const button of dialog.querySelectorAll<HTMLElement>("[data-settings-section]")) {
    if (button.dataset.settingsSection === "providers") continue;
    button.addEventListener("click", () => panel.classList.add("hidden"));
  }

  document.getElementById("settingsButton")?.addEventListener("click", () => {
    panel.classList.add("hidden");
  });

  const style = document.createElement("style");
  style.id = "tl-providers-settings-bridge-style";
  style.textContent = `
    .providers-settings-panel {
      max-height: min(62vh, 560px);
      overflow-y: auto;
      padding-right: 5px;
      scrollbar-gutter: stable;
    }
  `;
  document.head.appendChild(style);
})();
