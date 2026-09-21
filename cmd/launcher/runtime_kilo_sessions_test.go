package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKiloSessionCommandAdapterTranslatesSemanticRun(t *testing.T) {
	project := t.TempDir()
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/session/s1/prompt_async" {
			t.Fatalf("unexpected runtime request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("directory"); got != project {
			t.Fatalf("directory query=%q want %q", got, project)
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "runtime" || pass != "secret" {
			t.Fatalf("runtime auth mismatch: %q %q %v", user, pass, ok)
		}
		header, err := url.QueryUnescape(r.Header.Get("x-kilo-directory"))
		if err != nil {
			t.Fatal(err)
		}
		if header != project {
			t.Fatalf("runtime project header=%q want %q", header, project)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["agent"] != "code" || payload["variant"] != "fast" {
			t.Fatalf("agent/variant mapping mismatch: %#v", payload)
		}
		model, _ := payload["model"].(map[string]any)
		if model["providerID"] != "test" || model["modelID"] != "model-1" {
			t.Fatalf("model mapping mismatch: %#v", model)
		}
		parts, _ := payload["parts"].([]any)
		if len(parts) != 1 {
			t.Fatalf("parts mapping mismatch: %#v", payload["parts"])
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer runtimeServer.Close()

	state := &appState{project: project}
	engine := kiloRuntimeEngine{}
	backend, err := newRuntimeBackend(
		state,
		runtimeServer.URL,
		runtimeCredentials{Username: "runtime", Password: "secret"},
		engine,
	)
	if err != nil {
		t.Fatal(err)
	}
	adapter := engine.SessionCommands()
	err = adapter.RunSession(context.Background(), backend, project, "s1", sessionRunInput{
		Text: "hello",
		Parts: []map[string]any{{"type": "text", "text": "hello"}},
		Agent: "code",
		Model: &sessionModelRef{ProviderID: "test", ID: "model-1"},
		Variant: "fast",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestKiloSessionCommandAdapterKeepsMutationRoutesPrivate(t *testing.T) {
	root := releaseRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash("cmd/launcher/runtime_kilo_sessions.go")))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, required := range []string{
		`"/session"`,
		`"/prompt_async"`,
		`"/abort"`,
		`"modelID"`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("Kilo command adapter is missing private runtime mapping %q", required)
		}
	}
}
