package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMonacoEditorIsLocalLazyAndFallbackSafe(t *testing.T) {
	root := releaseRepoRoot(t)

	bridge, err := os.ReadFile(filepath.Join(root, "cmd", "launcher", "ui", "monaco.ts"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(bridge)
	for _, required := range []string{
		"/monaco-editor.js",
		"/monaco-editor.css",
		"/monaco-editor-worker.js",
		"tl-studio:editor-render",
		"lightweight editor fallback",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("Monaco bridge missing %q", required)
		}
	}
	if strings.Contains(source, "cdn.jsdelivr") || strings.Contains(source, "unpkg.com") {
		t.Fatal("Monaco editor must not load from a CDN")
	}

	index, err := os.ReadFile(filepath.Join(root, "cmd", "launcher", "web", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "<script src=\"/monaco.js\" defer></script>") {
		t.Fatal("Monaco bridge is not loaded by the workspace UI")
	}

	files, err := os.ReadFile(filepath.Join(root, "cmd", "launcher", "ui", "files.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(files), "tl-studio:editor-render") {
		t.Fatal("workspace editor does not publish Monaco render state")
	}
}
