package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserSessionReadsUseTLStudioSemanticContract(t *testing.T) {
	runtimeAPI := readBrowserSource(t, "runtime-api.ts")
	for _, expected := range []string{
		"sessionView",
		"/local/sessions",
		"/messages",
		"/changes",
	} {
		if !strings.Contains(runtimeAPI, expected) {
			t.Fatalf("runtime-api.ts is missing semantic session read contract %q", expected)
		}
	}
	for _, forbidden := range []string{
		"list: async ({ limit = 50",
		`request(route("/session/status"))`,
		"get: async (sessionID",
		"diff: async (sessionID",
		"messages: async (sessionID",
	} {
		if strings.Contains(runtimeAPI, forbidden) {
			t.Fatalf("raw runtime session read leaked into browser adapter: %q", forbidden)
		}
	}

	checks := map[string][]string{
		"core.ts":         {"K.api.sessionView.list", "K.api.sessionView.status"},
		"chat.ts":         {"K.api.sessionView.messages", "K.api.sessionView.get"},
		"workspace.ts":    {"K.api.sessionView.changes"},
		"status-ui.ts":    {"K.api.sessionView.messages"},
		"presentation.ts": {"message?.role", "message?.activities"},
	}
	for name, expected := range checks {
		source := readBrowserSource(t, name)
		for _, needle := range expected {
			if !strings.Contains(source, needle) {
				t.Fatalf("%s is missing semantic session behavior %q", name, needle)
			}
		}
	}

	for name, forbidden := range map[string][]string{
		"core.ts":      {"K.api.sessions.list", "K.api.sessions.status"},
		"chat.ts":      {"K.api.sessions.messages", "K.api.sessions.get"},
		"workspace.ts": {"K.api.sessions.diff"},
		"status-ui.ts": {"K.api.sessions.messages"},
	} {
		source := readBrowserSource(t, name)
		for _, needle := range forbidden {
			if strings.Contains(source, needle) {
				t.Fatalf("%s still reads raw runtime session data through %q", name, needle)
			}
		}
	}
}

func TestSessionContractKeepsExecutionAndPermissionOutsideReadModel(t *testing.T) {
	source := readBrowserSource(t, "runtime-api.ts")
	for _, expected := range []string{
		"promptAsync:",
		"abort:",
		"/local/permissions",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("execution/permission boundary unexpectedly changed: missing %q", expected)
		}
	}

	root := releaseRepoRoot(t)
	contractSource, err := os.ReadFile(filepath.Join(root, "cmd", "launcher", "session_contract.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(contractSource)
	for _, forbidden := range []string{
		"POST /local/sessions",
		"prompt_async",
		"/permission/",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("semantic session read contract must not execute agents or decide permissions: found %q", forbidden)
		}
	}
}
