package main

import (
	"strings"
	"testing"
)

func TestEmbeddedTerminalUIContract(t *testing.T) {
	text := readBrowserSource(t, "terminal.ts")
	css, err := webFS.ReadFile("web/terminal.css")
	if err != nil { t.Fatal(err) }
	for _, required := range []string{"Terminal", "/local/process", "terminalStop", "historyIndex", "K.terminal", "stoppedByUser", "[stopped]", "snapshot?.cwd", "setCwd"} {
		if !strings.Contains(text, required) { t.Fatalf("terminal.ts missing %q", required) }
	}
	if strings.Contains(text, "cdn.") || strings.Contains(text, "unpkg") || strings.Contains(text, "jsdelivr") {
		t.Fatal("terminal UI must not depend on external CDN assets")
	}
	if !strings.Contains(string(css), ".terminal-panel") { t.Fatal("terminal.css missing terminal panel styles") }
}
