package main

import (
	"io/fs"
	"strings"
	"testing"
)

func TestBrowserUsesTLStudioToolMetadata(t *testing.T) {
	assets, err := fs.Sub(webFS, "web")
	if err != nil {
		t.Fatal(err)
	}
	runtimeAPI, err := fs.ReadFile(assets, "runtime-api.js")
	if err != nil {
		t.Fatal(err)
	}
	registryUI, err := fs.ReadFile(assets, "tools.js")
	if err != nil {
		t.Fatal(err)
	}
	presentation, err := fs.ReadFile(assets, "presentation.js")
	if err != nil {
		t.Fatal(err)
	}
	chat, err := fs.ReadFile(assets, "chat.js")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(runtimeAPI), `/local/tools`) {
		t.Fatal("browser tool adapter must query the TL Studio launcher registry")
	}
	for name, source := range map[string]string{
		"tools.js":        string(registryUI),
		"presentation.js": string(presentation),
		"chat.js":         string(chat),
	} {
		if !strings.Contains(source, "toolDescriptor") {
			t.Fatalf("%s must resolve tool presentation through TL Studio metadata", name)
		}
	}
	if strings.Contains(string(presentation), `title: item.tool || item.name || "Tool"`) {
		t.Fatal("activity cards must not use raw runtime tool IDs as their primary label")
	}
}

func TestBrowserUnknownToolFallbackIsSafe(t *testing.T) {
	assets, err := fs.Sub(webFS, "web")
	if err != nil {
		t.Fatal(err)
	}
	registryUI, err := fs.ReadFile(assets, "tools.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(registryUI)
	for _, expected := range []string{
		`name: "Runtime tool"`,
		`category: "runtime"`,
		`permissionClass: "runtime"`,
		`unknown: true`,
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("unknown tool fallback is missing %q", expected)
		}
	}
}

func TestToolRegistryCannotReplyToPermissions(t *testing.T) {
	assets, err := fs.Sub(webFS, "web")
	if err != nil {
		t.Fatal(err)
	}
	registryUI, err := fs.ReadFile(assets, "tools.js")
	if err != nil {
		t.Fatal(err)
	}
	attention, err := fs.ReadFile(assets, "attention.js")
	if err != nil {
		t.Fatal(err)
	}
	registrySource := string(registryUI)
	for _, forbidden := range []string{
		"/local/permissions",
		"permissions.reply",
		"Always allow in this project",
	} {
		if strings.Contains(registrySource, forbidden) {
			t.Fatalf("tool registry UI must not make permission decisions: found %q", forbidden)
		}
	}
	if !strings.Contains(string(attention), "K.api.permissions.reply") {
		t.Fatal("permission replies must remain in the existing permission UI path")
	}
}
