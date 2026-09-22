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
		`id="pluginCommandInput"`,
		`id="pluginArgsInput"`,
		`id="pluginEnvInput"`,
		`id="pluginScopeSelect"`,
		`Test Connection`,
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
		`transport: transportSelect.value || "stdio"`,
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("generic Plugins UI is missing %q", expected)
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
