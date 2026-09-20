package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserSessionReadsUseTLStudioSemanticContract(t *testing.T) {
	assets, err := fs.Sub(webFS, "web")
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		t.Helper()
		data, err := fs.ReadFile(assets, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return string(data)
	}

	runtimeAPI := read("runtime-api.js")
	for _, expected := range []string{
		"sessionView",
		"/local/sessions",
		"/messages",
		"/changes",
	} {
		if !strings.Contains(runtimeAPI, expected) {
			t.Fatalf("runtime-api.js is missing semantic session read contract %q", expected)
		}
	}
	for _, forbidden := range []string{
		"list: async ({ limit = 50",
		"status: async () =>",
		"get: async (sessionID",
		"diff: async (sessionID",
		"messages: async (sessionID",
	} {
		if strings.Contains(runtimeAPI, forbidden) {
			t.Fatalf("raw runtime session read leaked into browser adapter: %q", forbidden)
		}
	}

	checks := map[string][]string{
		"core.js":         {"K.api.sessionView.list", "K.api.sessionView.status"},
		"chat.js":         {"K.api.sessionView.messages", "K.api.sessionView.get"},
		"workspace.js":    {"K.api.sessionView.changes"},
		"status-ui.js":    {"K.api.sessionView.messages"},
		"presentation.js": {"message?.role", "message?.activities"},
	}
	for name, expected := range checks {
		source := read(name)
		for _, needle := range expected {
			if !strings.Contains(source, needle) {
				t.Fatalf("%s is missing semantic session behavior %q", name, needle)
			}
		}
	}

	for name, forbidden := range map[string][]string{
		"core.js":      {"K.api.sessions.list", "K.api.sessions.status"},
		"chat.js":      {"K.api.sessions.messages", "K.api.sessions.get"},
		"workspace.js": {"K.api.sessions.diff"},
		"status-ui.js": {"K.api.sessions.messages"},
	} {
		source := read(name)
		for _, needle := range forbidden {
			if strings.Contains(source, needle) {
				t.Fatalf("%s still reads raw runtime session data through %q", name, needle)
			}
		}
	}
}

func TestSessionContractKeepsExecutionAndPermissionOutsideReadModel(t *testing.T) {
	assets, err := fs.Sub(webFS, "web")
	if err != nil {
		t.Fatal(err)
	}
	runtimeAPI, err := fs.ReadFile(assets, "runtime-api.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(runtimeAPI)
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
