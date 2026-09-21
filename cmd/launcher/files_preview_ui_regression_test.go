package main

import (
	"io/fs"
	"strings"
	"testing"
)

func TestWorkspacePreviewOnlyBinaryTabContract(t *testing.T) {
	assets, err := fs.Sub(webFS, "web")
	if err != nil {
		t.Fatal(err)
	}
	filesJS, err := fs.ReadFile(assets, "files.js")
	if err != nil {
		t.Fatal(err)
	}
	monacoJS, err := fs.ReadFile(assets, "monaco.js")
	if err != nil {
		t.Fatal(err)
	}

	files := string(filesJS)
	for _, required := range []string{
		"filePreviewOnly",
		"Preview-only file",
		"preview?.viewOnly",
		"preview?.previewable",
		"tab.viewOnly",
		"previewKind",
		"previewCapabilityID",
		"Binary files without a TL Studio preview cannot be opened yet",
		"Binary editing is intentionally disabled",
		"viewOnly: !!tab?.viewOnly",
		"Show in Folder",
		"/local/reveal",
		"showActiveInFolder",
		"showInFolder",
	} {
		if !strings.Contains(files, required) {
			t.Fatalf("files.js missing preview-only tab behavior %q", required)
		}
	}
	if strings.Contains(files, "Binary files cannot be edited yet") {
		t.Fatal("Workspace still blocks all binary files before Preview can activate")
	}

	monaco := string(monacoJS)
	for _, required := range []string{"detail?.viewOnly", "state.editor.setModel(null)"} {
		if !strings.Contains(monaco, required) {
			t.Fatalf("monaco.js missing preview-only protection %q", required)
		}
	}
}
