package main

import (
	"strings"
	"testing"
)

func TestProjectSearchUIContract(t *testing.T) {
	searchText := readBrowserSource(t, "search.ts")
	for _, required := range []string{
		"/local/search",
		"projectSearchQuery",
		"projectSearchInclude",
		"projectSearchExclude",
		"projectSearchCase",
		"event.ctrlKey || event.metaKey",
		"event.shiftKey",
		"event.key.toLowerCase() === "f"",
		"AbortController",
		"K.openWorkspaceFileAt",
		"match.line",
		"match.column",
	} {
		if !strings.Contains(searchText, required) {
			t.Fatalf("search UI is missing contract marker %q", required)
		}
	}

	filesText := readBrowserSource(t, "files.ts")
	for _, required := range []string{
		"K.openWorkspaceFileAt = openEditorAt",
		"setSelectionRange",
		"openEditor(path)",
	} {
		if !strings.Contains(filesText, required) {
			t.Fatalf("workspace editor is missing search navigation marker %q", required)
		}
	}

	entry := readBrowserSource(t, "browser.ts")
	if !strings.Contains(entry, `import "./search";`) {
		t.Fatal("Browser module graph does not include project search")
	}
}

func TestProjectSearchAddsNoRuntimeCDNDependency(t *testing.T) {
	for _, name := range []string{"search.ts", "files.ts", "app.ts"} {
		text := strings.ToLower(readBrowserSource(t, name))
		for _, forbidden := range []string{"https://cdn.", "https://unpkg.com", "https://esm.sh", "https://jsdelivr.net"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s introduced runtime CDN dependency %q", name, forbidden)
			}
		}
	}
}
