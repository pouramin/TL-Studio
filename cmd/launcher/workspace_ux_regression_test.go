package main

import (
	"io/fs"
	"strings"
	"testing"
)

func TestWorkspaceUXEnhancementsContract(t *testing.T) {
	assets, err := fs.Sub(webFS, "web")
	if err != nil { t.Fatal(err) }

	read := func(name string) string {
		data, err := fs.ReadFile(assets, name)
		if err != nil { t.Fatalf("read %s: %v", name, err) }
		return string(data)
	}

	floating := read("preview-floating.js")
	for _, required := range []string{"tl-studio.preview-window", "pointerdown", "ResizeObserver", "previewWindow", "localStorage"} {
		if !strings.Contains(floating, required) { t.Fatalf("preview-floating.js missing %q", required) }
	}

	editor := read("editor-enhancements.js")
	for _, required := range []string{"file-editor-highlight", "tok-keyword", "tok-string", "MutationObserver", "refreshEditorHighlight"} {
		if !strings.Contains(editor, required) { t.Fatalf("editor-enhancements.js missing %q", required) }
	}

	settings := read("settings-enhancements.js")
	for _, required := range []string{"Editor color theme", "UI Font", "Code Font", "Terminal Font", "resetPreviewWindow", "tl-studio.editor-theme"} {
		if !strings.Contains(settings, required) { t.Fatalf("settings-enhancements.js missing %q", required) }
	}

	app := read("app.js")
	for _, required := range []string{`"/editor-enhancements.js"`, `"/preview-floating.js"`, `"/settings-enhancements.js"`} {
		if !strings.Contains(app, required) { t.Fatalf("app.js missing loader %s", required) }
	}

	combined := floating + editor + settings
	for _, forbidden := range []string{"unpkg", "jsdelivr", "cdn.jsdelivr", "cdnjs"} {
		if strings.Contains(strings.ToLower(combined), forbidden) { t.Fatalf("workspace UX must not depend on external CDN: %s", forbidden) }
	}
}
