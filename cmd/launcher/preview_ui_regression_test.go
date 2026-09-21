package main

import (
	"strings"
	"testing"
)

func TestEmbeddedLivePreviewUIContract(t *testing.T) {
	text := readBrowserSource(t, "preview.ts")
	floatingText := readBrowserSource(t, "preview-floating.ts")

	css, err := webFS.ReadFile("web/preview.css")
	if err != nil {
		t.Fatal(err)
	}
	floatingCSS, err := webFS.ReadFile("web/preview-floating.css")
	if err != nil {
		t.Fatal(err)
	}

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
		"capability?.kind === "html"",
		"tl-studio:project-file-changed",
		"startsWith("file.")",
		"keepalive: true",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("preview.ts missing %q", required)
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
	for _, required := range []string{"resizeDirections", ""n", "s", "e", "w", "ne", "nw", "se", "sw"", "preview-resize-handle", "MIN_WIDTH = 340", "MIN_HEIGHT = 300", "panel.style.width", "panel.style.height"} {
		if !strings.Contains(floatingText, required) {
			t.Fatalf("preview-floating.ts missing width resize behavior %q", required)
		}
	}
	for _, required := range []string{".preview-resize-n", ".preview-resize-s", ".preview-resize-e", ".preview-resize-w", ".preview-resize-ne", ".preview-resize-nw", ".preview-resize-se", ".preview-resize-sw", "cursor: ew-resize", "cursor: ns-resize", "cursor: nwse-resize", "cursor: nesw-resize"} {
		if !strings.Contains(string(floatingCSS), required) {
			t.Fatalf("preview-floating.css missing full resize affordance %q", required)
		}
	}

	entry := readBrowserSource(t, "browser.ts")
	if !strings.Contains(entry, `import "./preview";`) {
		t.Fatal("Browser module graph does not include Preview")
	}
}
