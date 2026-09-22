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

func TestPluginsUIKeepsGraphifyAsConvenienceLayer(t *testing.T) {
	source := readBrowserSource(t, "plugins.ts")
	if !strings.Contains(source, `buildGraphify`) || !strings.Contains(source, `open-graph`) {
		t.Fatal("Graphify convenience actions should remain available through Plugins")
	}
	if strings.Contains(source, `mcp.graphify.query_graph`) || strings.Contains(source, `mcp.graphify.shortest_path`) {
		t.Fatal("Graphify MCP tools must be discovered dynamically, not hardcoded in the browser")
	}
}

func TestPluginsUseExistingPreviewForGraphHTML(t *testing.T) {
	source := readBrowserSource(t, "plugins.ts")
	for _, expected := range []string{
		"K.preview?.open?.()",
		"K.preview?.selectEntry?.(path)",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("Graphify Open Graph must reuse TL Studio Preview: missing %q", expected)
		}
	}
}
