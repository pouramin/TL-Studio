package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAvailablePluginCatalogIncludesGraphifyAndLaya(t *testing.T) {
	catalog := availablePluginCatalog()
	if len(catalog) != 2 {
		t.Fatalf("expected two curated plugin presets, got %#v", catalog)
	}
	byID := map[string]pluginCatalogEntry{}
	for _, entry := range catalog {
		byID[entry.ID] = entry
		if entry.Type != pluginTypeMCP || entry.Transport != pluginTransportStdio {
			t.Fatalf("catalog entry must stay on the generic stdio MCP path: %#v", entry)
		}
		if entry.PackageSpec == "" || entry.Executable == "" || entry.Module == "" || entry.Upstream == "" {
			t.Fatalf("catalog entry is missing install/runtime metadata: %#v", entry)
		}
		if entry.Category == "" || entry.Icon == "" {
			t.Fatalf("catalog entry is missing presentation metadata: %#v", entry)
		}
	}

	graphify := byID["graphify"]
	if graphify.Scope != "project" || graphify.PackageSpec != "graphifyy[mcp]" ||
		len(graphify.Arguments) != 1 || graphify.Arguments[0] != "graphify-out/graph.json" ||
		graphify.Metadata["integration"] != "graphify" {
		t.Fatalf("unexpected Graphify preset: %#v", graphify)
	}

	laya := byID["laya"]
	if laya.Scope != "global" || laya.PackageSpec != "laya[mcp]" ||
		laya.Environment["LAYA_PRELOAD"] != "0" {
		t.Fatalf("unexpected Laya preset: %#v", laya)
	}
}

func TestPluginCatalogResponseHidesInstallerCommands(t *testing.T) {
	encoded, err := json.Marshal(availablePluginCatalog())
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"packageSpec", "executable", "module", "arguments", "installHint", "graphify-mcp", "laya-mcp-server"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("catalog response leaked backend install details %q: %s", forbidden, text)
		}
	}
	for _, required := range []string{"category", "icon", "/plugin-graphify.svg", "/plugin-laya.svg"} {
		if !strings.Contains(text, required) {
			t.Fatalf("catalog response missing presentation field %q: %s", required, text)
		}
	}
}
