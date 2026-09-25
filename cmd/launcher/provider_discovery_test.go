package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type discoveryMemoryCredentialStore struct {
	mu      sync.Mutex
	secrets map[string]string
}

func newDiscoveryMemoryCredentialStore() *discoveryMemoryCredentialStore {
	return &discoveryMemoryCredentialStore{secrets: map[string]string{}}
}

func (s *discoveryMemoryCredentialStore) Put(id, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets[id] = secret
	return nil
}

func (s *discoveryMemoryCredentialStore) Get(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.secrets[id]
	if !ok {
		return "", errCredentialNotFound
	}
	return value, nil
}

func (s *discoveryMemoryCredentialStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.secrets, id)
	return nil
}

func (s *discoveryMemoryCredentialStore) Backend() string { return "memory" }

func boolPointerValue(value *bool) (bool, bool) {
	if value == nil {
		return false, false
	}
	return *value, true
}

func TestDiscoverOpenAICompatibleModelsNormalizesMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-key" {
			t.Fatalf("unexpected auth header %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [
				{
					"id": "vision-coder",
					"name": "Vision Coder",
					"context_length": 131072,
					"max_output_tokens": 16384,
					"capabilities": {
						"tool_calling": {"supported": true},
						"reasoning": {"supported": true}
					},
					"architecture": {"input_modalities": ["text", "image"]}
				},
				{
					"id": "plain-model",
					"owned_by": "example"
				}
			]
		}`))
	}))
	defer server.Close()

	models, err := discoverOpenAICompatibleModels(context.Background(), server.URL+"/v1", "secret-key")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	var vision providerDiscoveredModel
	var plain providerDiscoveredModel
	for _, model := range models {
		switch model.ID {
		case "vision-coder":
			vision = model
		case "plain-model":
			plain = model
		}
	}
	if vision.Name != "Vision Coder" || vision.ContextLimit != 131072 || vision.OutputLimit != 16384 {
		t.Fatalf("unexpected normalized model: %#v", vision)
	}
	if value, ok := boolPointerValue(vision.ToolCall); !ok || !value {
		t.Fatalf("expected explicit tool support: %#v", vision.ToolCall)
	}
	if value, ok := boolPointerValue(vision.Reasoning); !ok || !value {
		t.Fatalf("expected explicit reasoning support: %#v", vision.Reasoning)
	}
	if value, ok := boolPointerValue(vision.Vision); !ok || !value {
		t.Fatalf("expected vision support: %#v", vision.Vision)
	}
	if plain.ToolCall != nil || plain.Reasoning != nil || plain.Vision != nil {
		t.Fatalf("missing capability metadata must remain unknown: %#v", plain)
	}
}

func TestDiscoverAnthropicModelsUsesProviderSpecificAPIAndPagination(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "anthropic-secret" {
			t.Fatalf("unexpected x-api-key %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Fatalf("unexpected anthropic-version %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "1000" {
			t.Fatalf("unexpected limit %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after_id") == "" {
			_, _ = w.Write([]byte(`{
				"data": [{
					"id": "claude-first",
					"display_name": "Claude First",
					"max_input_tokens": 200000,
					"max_tokens": 32000,
					"capabilities": {"thinking": {"supported": true}}
				}],
				"has_more": true,
				"last_id": "claude-first"
			}`))
			return
		}
		if got := r.URL.Query().Get("after_id"); got != "claude-first" {
			t.Fatalf("unexpected after_id %q", got)
		}
		_, _ = w.Write([]byte(`{
			"data": [{
				"id": "claude-second",
				"display_name": "Claude Second",
				"max_input_tokens": 1000000,
				"max_tokens": 64000,
				"capabilities": {"thinking": {"supported": false}}
			}],
			"has_more": false,
			"last_id": "claude-second"
		}`))
	}))
	defer server.Close()

	models, err := discoverAnthropicModels(context.Background(), server.URL+"/v1", "anthropic-secret")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("expected 2 paginated requests, got %d", requests)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	for _, model := range models {
		switch model.ID {
		case "claude-first":
			if model.ContextLimit != 200000 || model.OutputLimit != 32000 {
				t.Fatalf("unexpected first model limits: %#v", model)
			}
			if value, ok := boolPointerValue(model.Reasoning); !ok || !value {
				t.Fatalf("expected reasoning=true: %#v", model)
			}
		case "claude-second":
			if model.ContextLimit != 1000000 || model.OutputLimit != 64000 {
				t.Fatalf("unexpected second model limits: %#v", model)
			}
			if value, ok := boolPointerValue(model.Reasoning); !ok || value {
				t.Fatalf("expected reasoning=false: %#v", model)
			}
		}
	}
}

func TestProviderDiscoveryUsesLastGoodCacheAfterTransientFailure(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("TL_STUDIO_STATE_DIR", stateDir)

	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"cached-model","name":"Cached Model"}]}`))
	}))
	defer server.Close()

	store := newProviderRegistryStore(filepath.Join(stateDir, "providers.json"))
	provider := tlProviderDefinition{
		ID:       "cached-provider",
		Name:     "Cached Provider",
		Protocol: "openai-compatible",
		BaseURL:  server.URL + "/v1",
		Models:   []tlProviderModel{{ID: "configured-model", Name: "Configured Model", ToolCall: true}},
	}
	if err := store.put(provider); err != nil {
		t.Fatal(err)
	}
	credentials := newDiscoveryMemoryCredentialStore()
	if err := credentials.Put(provider.ID, "secret"); err != nil {
		t.Fatal(err)
	}
	manager := &runtimeProviderManager{store: store, credentials: credentials}

	first, err := manager.discoverProviderModels(context.Background(), providerDiscoveryRequest{
		ProviderID: provider.ID,
		Protocol:   provider.Protocol,
		BaseURL:    provider.BaseURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Source != "live" || first.Stale || len(first.Models) != 1 {
		t.Fatalf("unexpected live result: %#v", first)
	}

	fail = true
	second, err := manager.discoverProviderModels(context.Background(), providerDiscoveryRequest{
		ProviderID: provider.ID,
		Protocol:   provider.Protocol,
		BaseURL:    provider.BaseURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Source != "cache" || !second.Stale || len(second.Models) != 1 {
		t.Fatalf("unexpected cache fallback: %#v", second)
	}
	if !strings.Contains(second.Warning, "rate limited") {
		t.Fatalf("expected rate-limit warning, got %q", second.Warning)
	}
}

func TestProviderDiscoveryDoesNotMaskAuthenticationFailureWithCache(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("TL_STUDIO_STATE_DIR", stateDir)

	authorized := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !authorized {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"auth-model"}]}`))
	}))
	defer server.Close()

	store := newProviderRegistryStore(filepath.Join(stateDir, "providers.json"))
	provider := tlProviderDefinition{
		ID:       "auth-provider",
		Name:     "Auth Provider",
		Protocol: "openai-compatible",
		BaseURL:  server.URL + "/v1",
		Models:   []tlProviderModel{{ID: "configured-model", ToolCall: true}},
	}
	if err := store.put(provider); err != nil {
		t.Fatal(err)
	}
	credentials := newDiscoveryMemoryCredentialStore()
	_ = credentials.Put(provider.ID, "good")
	manager := &runtimeProviderManager{store: store, credentials: credentials}

	if _, err := manager.discoverProviderModels(context.Background(), providerDiscoveryRequest{
		ProviderID: provider.ID, Protocol: provider.Protocol, BaseURL: provider.BaseURL,
	}); err != nil {
		t.Fatal(err)
	}
	authorized = false
	_, err := manager.discoverProviderModels(context.Background(), providerDiscoveryRequest{
		ProviderID: provider.ID, Protocol: provider.Protocol, BaseURL: provider.BaseURL, APIKey: "bad",
	})
	if err == nil {
		t.Fatal("expected authentication failure")
	}
	var httpErr *providerDiscoveryHTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401 discovery error, got %v", err)
	}
}
