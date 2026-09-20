package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestToolRegistryMapsKnownRuntimeTools(t *testing.T) {
	tests := []struct {
		runtimeID       string
		semanticID      string
		permissionClass string
	}{
		{runtimeID: "write", semanticID: "files.write", permissionClass: "write"},
		{runtimeID: "bash", semanticID: "terminal.command", permissionClass: "execute"},
		{runtimeID: "websearch", semanticID: "network.search", permissionClass: "network"},
		{runtimeID: "semantic_search", semanticID: "search.semantic", permissionClass: "read"},
	}

	for _, tc := range tests {
		descriptor, known := toolDescriptorForRuntimeID(tc.runtimeID)
		if !known {
			t.Fatalf("%s should be a known runtime tool", tc.runtimeID)
		}
		if descriptor.ID != tc.semanticID || descriptor.PermissionClass != tc.permissionClass {
			t.Fatalf("%s mapped to %#v", tc.runtimeID, descriptor)
		}
	}
}

func TestUnknownToolDescriptorIsConservative(t *testing.T) {
	descriptor, known := toolDescriptorForRuntimeID("future_runtime_tool")
	if known {
		t.Fatal("future runtime tool must not be treated as known")
	}
	if descriptor.ID != "runtime.unknown" || descriptor.PermissionClass != "runtime" || descriptor.Category != "runtime" {
		t.Fatalf("unexpected unknown descriptor: %#v", descriptor)
	}
	if descriptor.Capabilities.Read || descriptor.Capabilities.Write || descriptor.Capabilities.Execute || descriptor.Capabilities.Network {
		t.Fatalf("unknown tool must not invent capabilities: %#v", descriptor.Capabilities)
	}
	if len(descriptor.RuntimeIDs) != 1 || descriptor.RuntimeIDs[0] != "future_runtime_tool" {
		t.Fatalf("unknown runtime ID should remain observable: %#v", descriptor.RuntimeIDs)
	}
}

func TestToolRegistryHasUniqueRuntimeMappings(t *testing.T) {
	seen := map[string]string{}
	for _, descriptor := range builtInToolRegistry() {
		if descriptor.ID == "" || descriptor.Name == "" || descriptor.Category == "" || descriptor.PermissionClass == "" {
			t.Fatalf("incomplete descriptor: %#v", descriptor)
		}
		for _, runtimeID := range descriptor.RuntimeIDs {
			if previous := seen[runtimeID]; previous != "" {
				t.Fatalf("runtime tool %q maps to both %q and %q", runtimeID, previous, descriptor.ID)
			}
			seen[runtimeID] = descriptor.ID
		}
	}
}

func TestToolRegistryEndpointIsLauncherOwned(t *testing.T) {
	mux := http.NewServeMux()
	registerToolRegistryRoutes(mux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/local/tools", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	var payload toolRegistryPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Version != toolRegistryVersion || len(payload.Tools) == 0 {
		t.Fatalf("unexpected registry payload: %#v", payload)
	}
	if payload.Unknown.PermissionClass != "runtime" {
		t.Fatalf("unknown fallback must remain runtime-controlled: %#v", payload.Unknown)
	}
}
