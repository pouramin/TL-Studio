package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsExposeGenericPluginsSurface(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(releaseRepoRoot(t), "cmd", "launcher", "web", "index.html"))
	if err != nil { t.Fatal(err) }
	index := string(data)
	source := readBrowserSource(t, "plugins.ts")

	for _, expected := range []string{
		`data-settings-section="plugins"`,
		`data-settings-panel="plugins"`,
		`id="pluginAddButton"`,
		`id="pluginDialog"`,
		`id="pluginDialogTitle"`,
		`id="pluginDialogClose"`,
		`id="pluginCommandInput"`,
		`id="pluginArgsInput"`,
		`placeholder="/c&#10;npx&#10;-y&#10;package-name"`,
		`One argument per line. Enter only arguments here; the executable belongs in Command.`,
		`settings-primary-action`,
		`id="pluginEnvInput"`,
		`id="pluginScopeSelect"`,
		`Test Connection`,
		`id="pluginSaveButton" class="primary small" type="button">Done</button>`,
	} {
		if !strings.Contains(index, expected) {
			t.Fatalf("Plugins settings surface is missing %q", expected)
		}
	}
	for _, expected := range []string{
		"K.api.plugins.testConfig",
		"K.api.plugins.create",
		"K.api.plugins.update",
		"K.api.plugins.setEnabled",
		"K.api.plugins.remove",
		"K.api.plugins.saved()",
		"K.api.plugins.catalog()",
		"K.api.plugins.installCatalog",
		"K.api.plugins.attach",
		"Available integrations",
		"catalog-add",
		"Installing…",
		"Saved for another project",
		"Use in current project",
		`transport: transportSelect.value || "stdio"`,
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("generic Plugins UI is missing %q", expected)
		}
	}

	for _, expected := range []string{
		`pluginDialog.showModal()`,
		`pluginDialog.close()`,
		`pluginDialogTitle.textContent = plugin ? "Configure plugin" : "Add plugin"`,
		`nameInput.focus({ preventScroll: true })`,
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("plugin modal behavior is missing %q", expected)
		}
	}
	settingsClose := strings.Index(index, `id="settingsClose"`)
	pluginDialog := strings.Index(index, `id="pluginDialog"`)
	if settingsClose < 0 || pluginDialog < 0 || pluginDialog < settingsClose {
		t.Fatal("plugin editor must live in a dedicated dialog outside the Settings content")
	}

	cssData, err := os.ReadFile(filepath.Join(releaseRepoRoot(t), "cmd", "launcher", "web", "settings.css"))
	if err != nil { t.Fatal(err) }
	css := string(cssData)
	for _, expected := range []string{
		".plugin-arguments-field {",
		".plugin-arguments-field textarea:focus",
		"border: 1px solid var(--line);",
		"background: var(--panel-2);",
		".plugin-form-grid input::placeholder,",
		"color: #606975;",
		"border-color: #485260;",
		".plugin-dialog-card { width: min(700px, calc(100vw - 36px));",
		".plugin-editor { margin: 0; padding: 0; border: 0; background: transparent;",
		".plugin-empty { display: grid; gap: 3px; padding: 11px 13px;",
	} {
		if !strings.Contains(css, expected) {
			t.Fatalf("Plugins settings styling is missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		"border: 1px solid color-mix(in srgb,var(--accent),var(--line) 78%);",
		"box-shadow: 0 0 0 2px color-mix(in srgb,var(--accent),transparent 82%);",
	} {
		if strings.Contains(css, forbidden) {
			t.Fatalf("Arguments field must not keep the rejected double/accent border treatment: %q", forbidden)
		}
	}
}

func TestPluginCatalogPresentationStaysGeneric(t *testing.T) {
	source := readBrowserSource(t, "plugins.ts")
	for _, required := range []string{
		"Available integrations",
		"install and enable themselves",
		"plugin-catalog-logo",
		"plugin-catalog-category",
		"plugin-catalog-grid",
		"K.api.plugins.installCatalog",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("plugin catalog UI missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"Graphify", "Laya", "graphify-mcp", "laya-mcp-server",
		"Install first if needed:", "preset.command", "preset.arguments", "preset.installHint",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("browser catalog presentation must stay data-driven and hide install commands; found %q", forbidden)
		}
	}

	cssData, err := os.ReadFile(filepath.Join(releaseRepoRoot(t), "cmd", "launcher", "web", "settings.css"))
	if err != nil { t.Fatal(err) }
	css := string(cssData)
	for _, required := range []string{
		".settings-window.settings-window-plugins",
		".plugin-catalog-grid",
		".plugin-catalog-card",
		".plugin-catalog-logo",
		"grid-template-columns: repeat(2, minmax(0, 1fr))",
	} {
		if !strings.Contains(css, required) {
			t.Fatalf("plugin catalog provider-style layout missing %q", required)
		}
	}
}

func TestPluginsUIKeepsIntegrationsGeneric(t *testing.T) {
	source := readBrowserSource(t, "plugins.ts")
	for _, expected := range []string{
		"plugin.integration?.actions",
		"K.api.plugins.action",
		`descriptor.kind === "preview"`,
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("generic integration action handling is missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		"buildGraphify",
		"mcp.graphify.query_graph",
		"mcp.graphify.shortest_path",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("browser must not hardcode Graphify integration internals: found %q", forbidden)
		}
	}
}

func TestPluginPreviewActionsUseExistingPreview(t *testing.T) {
	source := readBrowserSource(t, "plugins.ts")
	for _, expected := range []string{
		"K.preview?.open?.()",
		"K.preview?.selectEntry?.(path)",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("Plugin preview actions must reuse TL Studio Preview: missing %q", expected)
		}
	}
}


func TestPluginEditorResetsAfterCompletion(t *testing.T) {
	source := readBrowserSource(t, "plugins.ts")
	for _, expected := range []string{
		"const resetEditor = () =>",
		"nameInput.value = \"\"",
		"commandInput.value = \"\"",
		"argsInput.value = \"\"",
		"envInput.value = \"\"",
		`pluginDialog.addEventListener("close", resetEditor)`,
		`settingsDialog?.addEventListener("close", () => {`,
		`if (pluginDialog.open) pluginDialog.close()`,
		"closeEditor();",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("Plugin editor reset behavior is missing %q", expected)
		}
	}
}


func TestPluginsUIDistinguishesBundledAndUserAddedWithoutForkingExecution(t *testing.T) {
	source := readBrowserSource(t, "plugins.ts")
	for _, expected := range []string{
		`plugin.origin === "bundled"`,
		`Included with TL Studio`,
		`Added by you`,
		`actionButton(plugin.enabled ? "Disable" : "Enable"`,
		`actionButton("Test Connection", "test", plugin.id)`,
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("bundled/user Plugin UI distinction is missing %q", expected)
		}
	}
	if strings.Contains(source, "bundledPluginExecute") || strings.Contains(source, "executeBundled") {
		t.Fatal("bundled plugins must not gain a browser-side execution path")
	}
}
