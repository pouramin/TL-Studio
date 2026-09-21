package main

import (
	"strings"
	"testing"
)

func TestBrowserUsesTLStudioToolMetadata(t *testing.T) {
	runtimeAPI := readBrowserSource(t, "runtime-api.ts")
	registryUI := readBrowserSource(t, "tools.ts")
	presentation := readBrowserSource(t, "presentation.ts")
	chat := readBrowserSource(t, "chat.ts")

	if !strings.Contains(runtimeAPI, `/local/tools`) {
		t.Fatal("browser tool adapter must query the TL Studio launcher registry")
	}
	for name, source := range map[string]string{
		"tools.ts":        registryUI,
		"presentation.ts": presentation,
		"chat.ts":         chat,
	} {
		if !strings.Contains(source, "toolDescriptor") {
			t.Fatalf("%s must resolve tool presentation through TL Studio metadata", name)
		}
	}
	if strings.Contains(presentation, `title: item.tool || item.name || "Tool"`) {
		t.Fatal("activity cards must not use raw runtime tool IDs as their primary label")
	}
}

func TestBrowserUnknownToolFallbackIsSafe(t *testing.T) {
	source := readBrowserSource(t, "tools.ts")
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
	registrySource := readBrowserSource(t, "tools.ts")
	attention := readBrowserSource(t, "attention.ts")
	for _, forbidden := range []string{
		"/local/permissions",
		"permissions.reply",
		"Always allow in this project",
	} {
		if strings.Contains(registrySource, forbidden) {
			t.Fatalf("tool registry UI must not make permission decisions: found %q", forbidden)
		}
	}
	if !strings.Contains(attention, "K.api.permissions.reply") {
		t.Fatal("permission replies must remain in the existing permission UI path")
	}
}
