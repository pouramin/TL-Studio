package main

import (
	"context"
	"errors"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type memoryProviderCredentialStore struct {
	mu      sync.Mutex
	secrets map[string]string
}

func newMemoryProviderCredentialStore() *memoryProviderCredentialStore {
	return &memoryProviderCredentialStore{secrets: map[string]string{}}
}

func (s *memoryProviderCredentialStore) Backend() string { return "memory-test" }

func (s *memoryProviderCredentialStore) Put(providerID, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets[providerID] = secret
	return nil
}

func (s *memoryProviderCredentialStore) Get(providerID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.secrets[providerID]
	if !ok {
		return "", errCredentialNotFound
	}
	return value, nil
}

func (s *memoryProviderCredentialStore) Delete(providerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.secrets, providerID)
	return nil
}

func testProviderDefinition() tlProviderDefinition {
	return tlProviderDefinition{
		ID:       "example-provider",
		Name:     "Example Provider",
		Protocol: "openai-compatible",
		BaseURL:  "https://api.example.com/v1",
		Models: []tlProviderModel{{
			ID:           "example-model",
			Name:         "Example Model",
			ToolCall:     true,
			Reasoning:    true,
			ContextLimit: 128000,
			OutputLimit:  16384,
		}},
	}
}

func TestProviderRegistryPersistsTLStudioSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	store := newProviderRegistryStore(path)
	if err := store.put(testProviderDefinition()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{"@ai-sdk/", "apiKey", "kilo-auto/free"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("TL Studio provider registry leaked runtime detail %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, `"protocol": "openai-compatible"`) {
		t.Fatalf("registry missing TL Studio protocol: %s", text)
	}

	reloaded := newProviderRegistryStore(path)
	providers, existed, err := reloaded.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !existed || len(providers) != 1 || providers[0].ID != "example-provider" {
		t.Fatalf("unexpected reloaded registry: existed=%v providers=%#v", existed, providers)
	}
}

func TestRuntimeProviderRoutesTranslateTLStudioConfig(t *testing.T) {
	project := t.TempDir()
	stateDir := t.TempDir()
	t.Setenv("TL_STUDIO_STATE_DIR", stateDir)

	var mu sync.Mutex
	var lastPatch map[string]any
	var credentialBody map[string]any
	disposeCalls := 0
	authDeleteCalls := 0

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "runtime" || pass != "secret" {
			t.Fatalf("unexpected runtime auth: %q %q %v", user, pass, ok)
		}
		if got := r.Header.Get("x-kilo-directory"); got == "" {
			t.Fatal("runtime provider bridge did not scope request to the selected project")
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/config/overlay":
			_, _ = io.WriteString(w, `{"effective":{"provider":{},"disabled_providers":[]}}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/config/overlay":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			lastPatch = body
			mu.Unlock()
			_, _ = io.WriteString(w, `{"ok":true}`)
		case r.Method == http.MethodPut && r.URL.Path == "/auth/example-provider":
			if err := json.NewDecoder(r.Body).Decode(&credentialBody); err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(w, `{"ok":true}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/auth/example-provider":
			authDeleteCalls++
			_, _ = io.WriteString(w, `{"ok":true}`)
		case r.Method == http.MethodPost && r.URL.Path == "/global/dispose":
			disposeCalls++
			_, _ = io.WriteString(w, `{"ok":true}`)
		case r.Method == http.MethodGet && r.URL.Path == "/provider":
			_, _ = io.WriteString(w, `{"all":[{"id":"kilo","name":"Hosted","models":{"kilo-auto/free":{"name":"Auto Free"}}},{"id":"example-provider","name":"Example Provider","models":{"example-model":{"name":"Example Model"}}}],"connected":["kilo","example-provider"],"default":{"kilo":"kilo-auto/free"},"failed":[]}`)
		default:
			t.Fatalf("unexpected runtime request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer backend.Close()

	state := &appState{project: project, backendURL: backend.URL, frontendURL: "http://127.0.0.1"}
	manager, err := newRuntimeProviderManager(state, backend.URL, "runtime", "secret")
	if err != nil {
		t.Fatal(err)
	}
	credentialStore := newMemoryProviderCredentialStore()
	manager.credentials = credentialStore
	mux := http.NewServeMux()
	registerRuntimeProviderRoutes(mux, manager)
	server := httptest.NewServer(mux)
	defer server.Close()

	get, err := http.Get(server.URL + "/runtime/providers/config")
	if err != nil {
		t.Fatal(err)
	}
	_ = get.Body.Close()
	if get.StatusCode != http.StatusOK {
		t.Fatalf("initial provider config status=%d", get.StatusCode)
	}

	definition := testProviderDefinition()
	body, _ := json.Marshal(providerWriteRequest{Provider: definition, APIKey: "top-secret"})
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/runtime/providers/config/example-provider", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("provider save status=%d", res.StatusCode)
	}

	mu.Lock()
	patch := lastPatch
	mu.Unlock()
	set, _ := patch["set"].(map[string]any)
	runtimeProviders, _ := set["provider"].(map[string]any)
	runtimeProvider, _ := runtimeProviders["example-provider"].(map[string]any)
	if runtimeProvider["npm"] != "@ai-sdk/openai-compatible" {
		t.Fatalf("runtime translation missing package: %#v", runtimeProvider)
	}
	if credentialBody["key"] != "top-secret" {
		t.Fatalf("credential was not synchronized to the runtime execution store: %#v", credentialBody)
	}
	if stored, err := credentialStore.Get("example-provider"); err != nil || stored != "top-secret" {
		t.Fatalf("TL Studio credential store did not become source of truth: value=%q err=%v", stored, err)
	}

	registryData, err := os.ReadFile(filepath.Join(stateDir, "providers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(registryData), "top-secret") || strings.Contains(string(registryData), "@ai-sdk") {
		t.Fatalf("TL Studio registry must not persist credential/runtime package details: %s", registryData)
	}

	catalogRes, err := http.Get(server.URL + "/runtime/providers/catalog?directory=" + url.QueryEscape(project))
	if err != nil {
		t.Fatal(err)
	}
	defer catalogRes.Body.Close()
	if catalogRes.StatusCode != http.StatusOK {
		t.Fatalf("catalog status=%d", catalogRes.StatusCode)
	}
	var catalog providerCatalogResponse
	if err := json.NewDecoder(catalogRes.Body).Decode(&catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.Hosted.ProviderID != "kilo" || len(catalog.Hosted.PreferredModels) != 1 || catalog.Hosted.PreferredModels[0] != "kilo-auto/free" {
		t.Fatalf("hosted metadata missing from TL Studio catalog: %#v", catalog.Hosted)
	}
	foundCustom := false
	for _, provider := range catalog.All {
		if provider.ID == "example-provider" {
			foundCustom = provider.Source == "custom"
		}
	}
	if !foundCustom {
		t.Fatalf("custom provider was not marked as TL Studio-managed: %#v", catalog.All)
	}

	deleteReq, _ := http.NewRequest(http.MethodDelete, server.URL+"/runtime/providers/config/example-provider", nil)
	deleteRes, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = deleteRes.Body.Close()
	if deleteRes.StatusCode != http.StatusOK {
		t.Fatalf("provider delete status=%d", deleteRes.StatusCode)
	}
	if authDeleteCalls != 1 || disposeCalls < 2 {
		t.Fatalf("delete/dispose calls unexpected: authDelete=%d dispose=%d", authDeleteCalls, disposeCalls)
	}
	if _, err := credentialStore.Get("example-provider"); !errors.Is(err, errCredentialNotFound) {
		t.Fatalf("TL Studio credential was not removed with provider: %v", err)
	}
}

func TestProviderManagerRestoresTLStudioCredentialIntoFreshRuntime(t *testing.T) {
	project := t.TempDir()
	stateDir := t.TempDir()
	t.Setenv("TL_STUDIO_STATE_DIR", stateDir)

	registry := newProviderRegistryStore(filepath.Join(stateDir, "providers.json"))
	if err := registry.put(testProviderDefinition()); err != nil {
		t.Fatal(err)
	}
	credentials := newMemoryProviderCredentialStore()
	if err := credentials.Put("example-provider", "restored-secret"); err != nil {
		t.Fatal(err)
	}

	credentialWrites := 0
	disposeCalls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/config/overlay":
			_, _ = io.WriteString(w, `{"effective":{"provider":{},"disabled_providers":[]}}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/config/overlay":
			_, _ = io.WriteString(w, `{"ok":true}`)
		case r.Method == http.MethodPut && r.URL.Path == "/auth/example-provider":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["key"] != "restored-secret" {
				t.Fatalf("unexpected restored credential: %#v", body)
			}
			credentialWrites++
			_, _ = io.WriteString(w, `{"ok":true}`)
		case r.Method == http.MethodPost && r.URL.Path == "/global/dispose":
			disposeCalls++
			_, _ = io.WriteString(w, `{"ok":true}`)
		default:
			t.Fatalf("unexpected runtime request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer backend.Close()

	state := &appState{project: project}
	manager, err := newRuntimeProviderManager(state, backend.URL, "runtime", "secret")
	if err != nil {
		t.Fatal(err)
	}
	manager.store = registry
	manager.credentials = credentials

	if err := manager.ensureBootstrapped(context.Background()); err != nil {
		t.Fatal(err)
	}
	if credentialWrites != 1 {
		t.Fatalf("expected one credential restore into runtime, got %d", credentialWrites)
	}
	if disposeCalls < 2 {
		t.Fatalf("expected provider sync and post-credential runtime reload, got %d dispose calls", disposeCalls)
	}
}

func urlQueryEscape(value string) string {
	request := httptest.NewRequest(http.MethodGet, "/?directory="+value, nil)
	return request.URL.Query().Get("directory")
}
