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
	css, err := fs.ReadFile(assets, "preview.css")
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
		"tl-studio:project-file-changed",
		"startsWith(\"file.\")",
		"keepalive: true",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("preview.js missing %q", required)
		}
	}
	if strings.Contains(text, "cdn.") || strings.Contains(text, "unpkg") || strings.Contains(text, "jsdelivr") {
		t.Fatal("preview UI must not depend on external CDN assets")
	}
	if !strings.Contains(string(css), ".preview-panel") || !strings.Contains(string(css), ".preview-frame") {
		t.Fatal("preview.css missing preview panel/frame styles")
	}
	if !strings.Contains(string(app), `"/preview.js"`) {
		t.Fatal("app.js does not load preview.js")
	}
}
