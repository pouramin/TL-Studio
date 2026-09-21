package main

import (
	"strings"
	"testing"
)

func TestBrowserIDEFoundationIsEmbeddedAndWired(t *testing.T) {
	filesText := readBrowserSource(t, "files.ts")
	for _, expected := range []string{
		`K.state.editorTabs = []`,
		`expectedSha256: tab.sha256`,
		`error.status === 409`,
		`method: "POST"`,
		`method: "PATCH"`,
		`method: "DELETE"`,
		`event.key.toLowerCase() === "s"`,
		`Ask Agent`,
	} {
		if !strings.Contains(filesText, expected) {
			t.Fatalf("files.ts is missing IDE behavior %q", expected)
		}
	}

	hardeningText := readBrowserSource(t, "ide-foundation.ts")
	for _, expected := range []string{
		`refreshOpenTabs`,
		`beforeunload`,
		`externalChanged`,
		`Switch projects and discard all unsaved editor changes?`,
		`K.workspaceFiles`,
		`K.state.local?.platform === "windows"`,
	} {
		if !strings.Contains(hardeningText, expected) {
			t.Fatalf("ide-foundation.ts is missing reconciliation behavior %q", expected)
		}
	}
	if strings.Contains(filesText+hardeningText, "cdn.") || strings.Contains(filesText+hardeningText, "unpkg.com") || strings.Contains(filesText+hardeningText, "jsdelivr.net") {
		t.Fatal("browser IDE must not depend on a runtime CDN")
	}

	entry := readBrowserSource(t, "browser.ts")
	if !strings.Contains(entry, `import "./ide-foundation";`) {
		t.Fatal("Browser module graph does not include the IDE reconciliation module")
	}

	css, err := webFS.ReadFile("web/files.css")
	if err != nil {
		t.Fatalf("read embedded files.css: %v", err)
	}
	styles := string(css)
	for _, expected := range []string{".file-tabs", ".file-editor-input", ".file-tab.external-change"} {
		if !strings.Contains(styles, expected) {
			t.Fatalf("files.css is missing IDE style %q", expected)
		}
	}
}
