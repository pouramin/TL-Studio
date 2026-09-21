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
		"state.editor?.layout?.()",
		"detail?.viewOnly",
		"state.editor.setModel(null)",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("Monaco bridge missing %q", required)
		}
	}
	if strings.Contains(source, "cdn.jsdelivr") || strings.Contains(source, "unpkg.com") {
		t.Fatal("Monaco editor must not load from a CDN")
	}

	entry, err := os.ReadFile(filepath.Join(root, "cmd", "launcher", "ui", "browser.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(entry), `import "./monaco";`) {
		t.Fatal("Monaco bridge is not part of the Browser module graph")
	}

	index, err := os.ReadFile(filepath.Join(root, "cmd", "launcher", "web", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), `<script type="module" src="/browser.js"></script>`) {
		t.Fatal("workspace UI is not loading the bundled Browser module")
	}

	filesCSS, err := os.ReadFile(filepath.Join(root, "cmd", "launcher", "web", "files.css"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(filesCSS), ".file-editor-surface { position: absolute; inset: 0;") {
		t.Fatal("workspace editor surface must fill the editor body")
	}

	files, err := os.ReadFile(filepath.Join(root, "cmd", "launcher", "ui", "files.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(files), "tl-studio:editor-render") {
		t.Fatal("workspace editor does not publish Monaco render state")
	}
}
