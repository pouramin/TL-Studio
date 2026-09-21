package main

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedLivePreviewUIContract(t *testing.T) {
	assets, err := fs.Sub(webFS, "web")
	if err != nil {
		t.Fatal(err)
	}
	js, err := fs.ReadFile(assets, "preview.js")
	if err != nil {
		t.Fatal(err)
	}
	floatingJS, err := fs.ReadFile(assets, "preview-floating.js")
	if err != nil {
		t.Fatal(err)
	}
	css, err := fs.ReadFile(assets, "preview.css")
	if err != nil {
		t.Fatal(err)
	}
	floatingCSS, err := fs.ReadFile(assets, "preview-floating.css")
	if err != nil {
		t.Fatal(err)
	}
	app, err := fs.ReadFile(assets, "app.js")
	if err != nil {
		t.Fatal(err)
	}

	text := string(js)
	for _, required := range []string{
		"Live Preview",
		"/local/preview",
		"previewFrame",
		"previewReload",
		"previewExternal",
		"previewEntry",
		"K.state.activeEditorPath",
		"snapshot?.entries",
		"new URLSearchParams({ entry: value })",
		"Ready to preview",
		"/local/preview/capabilities",
		"previewCapabilityForPath",
		"activePreviewEntry",
		"switchPreviewEntry",
		"await switchPreviewEntry(value)",
		"tl-studio:editor-render",
		"followActivePreviewEntry",
		"entrySwitchGeneration",
		"activeEntry !== entry",
		"Preview file",
		"capability?.kind === \"html\"",
		"tl-studio:project-file-changed",
		"startsWith(\"file.\")",
		"keepalive: true",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("preview.js missing %q", required)
		}
	}
	if strings.Contains(text, "HTML file") || strings.Contains(text, "followActiveHTMLEntry") {
		t.Fatal("preview UI still contains HTML-only preview behavior")
	}
	if strings.Contains(text, "cdn.") || strings.Contains(text, "unpkg") || strings.Contains(text, "jsdelivr") {
		t.Fatal("preview UI must not depend on external CDN assets")
	}
	if !strings.Contains(string(css), ".preview-panel") || !strings.Contains(string(css), ".preview-frame") || !strings.Contains(string(css), ".preview-entry-row") {
		t.Fatal("preview.css missing preview panel/frame/entry selector styles")
	}
	floatingText := string(floatingJS)
	for _, required := range []string{"resizeDirections", "\"n\", \"s\", \"e\", \"w\", \"ne\", \"nw\", \"se\", \"sw\"", "preview-resize-handle", "MIN_WIDTH = 340", "MIN_HEIGHT = 300", "panel.style.width", "panel.style.height"} {
		if !strings.Contains(floatingText, required) {
			t.Fatalf("preview-floating.js missing width resize behavior %q", required)
		}
	}
	for _, required := range []string{".preview-resize-n", ".preview-resize-s", ".preview-resize-e", ".preview-resize-w", ".preview-resize-ne", ".preview-resize-nw", ".preview-resize-se", ".preview-resize-sw", "cursor: ew-resize", "cursor: ns-resize", "cursor: nwse-resize", "cursor: nesw-resize"} {
		if !strings.Contains(string(floatingCSS), required) {
			t.Fatalf("preview-floating.css missing full resize affordance %q", required)
		}
	}
	if !strings.Contains(string(app), `"/preview.js"`) {
		t.Fatal("app.js does not load preview.js")
	}
}
