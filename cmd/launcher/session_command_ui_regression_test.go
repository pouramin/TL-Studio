package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserSessionMutationsUseTLStudioSemanticContract(t *testing.T) {
	adapter := readBrowserSource(t, "runtime-api.ts")
	for _, required := range []string{
		"sessionCommands: {",
		"withQuery(\"/local/sessions\", { directory })",
		"/local/sessions/${enc(sessionID)}",
		"/local/sessions/${enc(sessionID)}/runs",
		"/local/sessions/${enc(sessionID)}/abort",
	} {
		if !strings.Contains(adapter, required) {
			t.Fatalf("Browser session adapter is missing semantic route %q", required)
		}
	}
	for _, forbidden := range []string{
		"prompt_async",
		"route(\"/session\")",
		"route(`/session/${enc(sessionID)}`",
	} {
		if strings.Contains(adapter, forbidden) {
			t.Fatalf("Browser session adapter leaked implementation-specific mutation route %q", forbidden)
		}
	}

	for _, sourceFile := range []string{
		"chat.ts",
		"attachments.ts",
		"workspace.ts",
		"product-ui.ts",
		"diagnostics-ui.ts",
	} {
		source := readBrowserSource(t, sourceFile)
		if strings.Contains(source, "K.api.sessions") {
			t.Fatalf("%s still uses the legacy raw session mutation adapter", sourceFile)
		}
	}
}

func TestGenericSessionCommandContractContainsNoKiloMutationDetails(t *testing.T) {
	root := releaseRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash("cmd/launcher/session_command_contract.go")))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, forbidden := range []string{
		"x-kilo-directory",
		"prompt_async",
		"KILO_",
		"\"/session/\"",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("generic session command contract leaked engine-specific detail %q", forbidden)
		}
	}
}
