package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestJevRouterDiscoveryMetadataUsesProviderCapabilities(t *testing.T) {
	model, ok := normalizeProviderDiscoveredModel(map[string]any{
		"id":             jevRouterModelID,
		"name":           "TypeSafe: Jev Router",
		"context_length": json.Number("1000000"),
		"supported_parameters": []any{
			"reasoning",
			"tool_choice",
			"tools",
		},
		"pricing": map[string]any{"prompt": "0", "completion": "0"},
	})
	if !ok {
		t.Fatal("expected Jev Router discovery record to normalize")
	}
	if model.ID != jevRouterModelID || model.Name != jevRouterDisplayName || model.Kind != "router" {
		t.Fatalf("unexpected Jev Router metadata %#v", model)
	}
	if model.ToolCall == nil || !*model.ToolCall {
		t.Fatalf("tool support should come from provider supported_parameters, got %#v", model.ToolCall)
	}
	if model.Reasoning == nil || !*model.Reasoning {
		t.Fatalf("reasoning support should come from provider supported_parameters, got %#v", model.Reasoning)
	}
	if model.ContextLimit != 1000000 {
		t.Fatalf("unexpected context limit %d", model.ContextLimit)
	}
}

func TestJevRouterPublishedCapabilitiesCanExplicitlyDisableTools(t *testing.T) {
	model, ok := normalizeProviderDiscoveredModel(map[string]any{
		"id": jevRouterModelID,
		"name": "TypeSafe: Jev Router",
		"supported_parameters": []any{"temperature"},
	})
	if !ok {
		t.Fatal("expected Jev Router discovery record")
	}
	if model.ToolCall == nil || *model.ToolCall {
		t.Fatalf("published OpenRouter parameters without tools must not be treated as tool support: %#v", model.ToolCall)
	}
	if model.Reasoning == nil || *model.Reasoning {
		t.Fatalf("published OpenRouter parameters without reasoning must not be treated as reasoning support: %#v", model.Reasoning)
	}
}

func TestJevRouterRegistersAndResolvesThroughExistingProviderRegistry(t *testing.T) {
	stateDir := t.TempDir()
	store := newProviderRegistryStore(filepath.Join(stateDir, "providers.json"))
	if err := store.put(tlProviderDefinition{
		ID: "openrouter", Name: "OpenRouter", Protocol: "openai-compatible", BaseURL: openRouterBaseURL,
		Models: []tlProviderModel{{ID: jevRouterModelID, Name: jevRouterDisplayName, Kind: "router", ToolCall: true}},
	}); err != nil {
		t.Fatal(err)
	}
	credentials := newMemoryProviderCredentialStore()
	if err := credentials.Put("openrouter", "shared-key"); err != nil {
		t.Fatal(err)
	}
	manager := &runtimeProviderManager{store: store, credentials: credentials}
	provider, model, key, err := manager.resolveNativeModel("openrouter", jevRouterModelID)
	if err != nil {
		t.Fatal(err)
	}
	if provider.BaseURL != openRouterBaseURL || model.ID != jevRouterModelID || model.Kind != "router" || key != "shared-key" {
		t.Fatalf("unexpected Jev Router resolution: provider=%#v model=%#v key=%q", provider, model, key)
	}
}

func TestJevRouterNativeRequestUsesExactFreeRouterModel(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/api/alpha/decisions" {
			t.Fatal("selecting Jev Router must never call the paid Decisions API")
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected Jev Router request path %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != jevRouterModelID {
			t.Fatalf("expected exact free router model %q, got %#v", jevRouterModelID, payload["model"])
		}
		messages, _ := payload["messages"].([]any)
		if len(messages) < 2 {
			t.Fatalf("system prompt and conversation history were not preserved: %#v", payload["messages"])
		}
		if _, ok := payload["tools"]; !ok {
			t.Fatal("existing tool definitions were not preserved")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"model":"openai/gpt-6-luna",
			"choices":[{"message":{"content":"done"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":12,"completion_tokens":3}
		}`)
	}))
	defer server.Close()

	response, err := newNativeModelClient().Complete(context.Background(), nativeModelRequest{
		System:   "Keep the current system prompt.",
		Provider: tlProviderDefinition{ID: "openrouter", Protocol: "openai-compatible", BaseURL: server.URL + "/v1"},
		Model:    tlProviderModel{ID: jevRouterModelID, Name: jevRouterDisplayName, Kind: "router", ToolCall: true},
		APIKey:   "sk-test",
		Messages: []nativeConversationMessage{{Role: "user", Text: "continue the task"}},
		Tools:    []nativeModelToolDefinition{nativeTestToolDefinition()},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one generative request, got %d", calls.Load())
	}
	if response.Text != "done" || response.RoutedModel != "openai/gpt-6-luna" {
		t.Fatalf("unexpected normalized Jev Router response %#v", response)
	}
	activities := nativeRoutedModelActivity(
		tlProviderDefinition{ID: "openrouter"},
		tlProviderModel{ID: jevRouterModelID, Kind: "router"},
		response,
	)
	if len(activities) != 1 || activities[0].Title != "Routed by Jev" || activities[0].Model == nil || activities[0].Model.ID != "openai/gpt-6-luna" {
		t.Fatalf("unexpected routed-model activity %#v", activities)
	}
}

func TestJevRouterProviderErrorsRemainErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "router unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	_, err := newNativeModelClient().Complete(context.Background(), nativeModelRequest{
		Provider: tlProviderDefinition{ID: "openrouter", Protocol: "openai-compatible", BaseURL: server.URL + "/v1"},
		Model:    tlProviderModel{ID: jevRouterModelID, Kind: "router", ToolCall: true},
		APIKey:   "sk-test",
		Messages: []nativeConversationMessage{{Role: "user", Text: "task"}},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("expected normal provider error without runtime crash, got %v", err)
	}
}

func TestOpenRouterDecisionEndpointIsSeparateFromChatAPI(t *testing.T) {
	endpoint, err := openRouterDecisionEndpoint(openRouterBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "https://openrouter.ai/api/alpha/decisions" {
		t.Fatalf("unexpected decision endpoint %q", endpoint)
	}
	if isOpenRouterBaseURL("https://example.com/api/v1") {
		t.Fatal("non-OpenRouter endpoints must not be treated as the shared OpenRouter credential source")
	}
}

func TestOpenRouterCredentialReuseForDecisionEngine(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("TL_STUDIO_STATE_DIR", stateDir)
	store := newProviderRegistryStore(filepath.Join(stateDir, "providers.json"))
	if err := store.put(tlProviderDefinition{
		ID: "my-existing-openrouter", Name: "My OpenRouter", Protocol: "openai-compatible", BaseURL: openRouterBaseURL,
		Models: []tlProviderModel{{ID: jevRouterModelID, Name: jevRouterDisplayName, Kind: "router", ToolCall: true}},
	}); err != nil {
		t.Fatal(err)
	}
	credentials := newMemoryProviderCredentialStore()
	if err := credentials.Put("my-existing-openrouter", "existing-secret"); err != nil {
		t.Fatal(err)
	}
	manager := &runtimeProviderManager{store: store, credentials: credentials}

	provider, key, err := manager.findOpenRouterProvider()
	if err != nil {
		t.Fatal(err)
	}
	if provider.ID != "my-existing-openrouter" || key != "existing-secret" {
		t.Fatalf("decision engine did not reuse existing OpenRouter configuration: provider=%#v key=%q", provider, key)
	}
}

func TestDecisionEngineDefaultsOffAndPersistsExplicitEnablement(t *testing.T) {
	t.Setenv("TL_STUDIO_STATE_DIR", t.TempDir())
	config, err := loadDecisionEngineConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Engine != "off" || config.Model != jevDecisionDefaultModel {
		t.Fatalf("unexpected default decision config %#v", config)
	}
	if _, err := saveDecisionEngineConfig(decisionEngineConfig{Engine: jevDecisionEngineID}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := loadDecisionEngineConfig()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Engine != jevDecisionEngineID || reloaded.Model != jevDecisionDefaultModel {
		t.Fatalf("decision engine configuration did not persist %#v", reloaded)
	}
}

func TestDecisionEngineCannotEnableWithoutOpenRouterCredential(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("TL_STUDIO_STATE_DIR", stateDir)
	store := newProviderRegistryStore(filepath.Join(stateDir, "providers.json"))
	if err := store.put(tlProviderDefinition{
		ID: "openrouter", Name: "OpenRouter", Protocol: "openai-compatible", BaseURL: openRouterBaseURL,
		Models: []tlProviderModel{{ID: jevRouterModelID, Name: jevRouterDisplayName, Kind: "router", ToolCall: true}},
	}); err != nil {
		t.Fatal(err)
	}
	service := newDecisionEngineService(&runtimeProviderManager{
		store: store, credentials: newMemoryProviderCredentialStore(),
	})
	_, err := service.configure(decisionEngineConfig{Engine: jevDecisionEngineID})
	var typed *decisionEngineError
	if !errors.As(err, &typed) || typed.Kind != "credential" {
		t.Fatalf("expected missing credential error, got %v", err)
	}
	config, loadErr := loadDecisionEngineConfig()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if config.Engine != "off" {
		t.Fatalf("failed paid enablement must not mutate the default-off config: %#v", config)
	}
}

func TestOpenRouterDecisionEngineParsesTypedAnswers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/alpha/decisions" {
			t.Fatalf("unexpected decision request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer shared-openrouter-key" {
			t.Fatalf("decision request did not reuse OpenRouter credential")
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != jevDecisionDefaultModel {
			t.Fatalf("unexpected decision model %#v", payload["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"gen-dec-test",
			"model":"typesafe/jev-1.13-20260917",
			"provider":"TypeSafe",
			"answers":{
				"route":{"type":"choice","choice":"complex","probabilities":{"simple":0.1,"complex":0.9},"confidence":0.8},
				"risk":{"type":"score","score":1.5,"probabilities":{"0":0.1,"1":0.3,"2":0.6},"legend":{"0":"safe","1":"review","2":"dangerous"},"confidence":0.6},
				"continue":{"type":"noul","noul":0.77}
			},
			"usage":{"input_tokens":100,"output_tokens":20,"cost":0.0000042}
		}`)
	}))
	defer server.Close()

	engine := &openRouterDecisionEngine{
		endpoint: server.URL + "/api/alpha/decisions",
		model: jevDecisionDefaultModel,
		apiKey: "shared-openrouter-key",
		httpClient: server.Client(),
	}
	result, err := engine.Evaluate(context.Background(), decisionEvaluateRequest{
		State: map[string]any{"task": "test"},
		Questions: map[string]decisionQuestion{
			"route": {
				Type: "choice", Instructions: "Classify task complexity.",
				Criteria: map[string]any{"simple": "simple", "complex": "complex"},
			},
			"risk": {
				Type: "score", Instructions: "Score risk.",
				Criteria: []any{"safe", "review", "dangerous"},
			},
			"continue": {Type: "noul", Instructions: "Should the loop continue?"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Answers["route"].Choice != "complex" || result.Answers["continue"].Noul == nil || *result.Answers["continue"].Noul != 0.77 {
		t.Fatalf("unexpected typed decision response %#v", result.Answers)
	}
	if result.Answers["risk"].Score == nil || *result.Answers["risk"].Score != 1.5 || result.Usage.Cost <= 0 {
		t.Fatalf("score/usage parsing failed %#v", result)
	}
}

func TestDecisionEngineValidationAndHTTPFailuresAreTyped(t *testing.T) {
	t.Run("validation", func(t *testing.T) {
		engine := &openRouterDecisionEngine{
			endpoint: "http://127.0.0.1/never",
			model: jevDecisionDefaultModel,
			apiKey: "x",
			httpClient: http.DefaultClient,
		}
		_, err := engine.Evaluate(context.Background(), decisionEvaluateRequest{
			State: "x",
			Questions: map[string]decisionQuestion{
				"bad": {Type: "choice", Instructions: "pick", Criteria: map[string]any{"only": "one"}},
			},
		})
		var typed *decisionEngineError
		if !errors.As(err, &typed) || typed.Kind != "validation" {
			t.Fatalf("expected typed validation error, got %v", err)
		}
	})

	for _, item := range []struct {
		name   string
		status int
		kind   string
	}{
		{"auth", http.StatusUnauthorized, "authentication"},
		{"rate", http.StatusTooManyRequests, "rate_limit"},
		{"upstream", http.StatusServiceUnavailable, "upstream"},
	} {
		t.Run(item.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, item.name, item.status)
			}))
			defer server.Close()
			engine := &openRouterDecisionEngine{
				endpoint: server.URL,
				model: jevDecisionDefaultModel,
				apiKey: "x",
				httpClient: server.Client(),
			}
			_, err := engine.Evaluate(context.Background(), decisionEvaluateRequest{
				State: "x",
				Questions: map[string]decisionQuestion{
					"ok": {Type: "noul", Instructions: "Is this x?"},
				},
			})
			var typed *decisionEngineError
			if !errors.As(err, &typed) || typed.Kind != item.kind {
				t.Fatalf("expected %s error, got %v", item.kind, err)
			}
		})
	}
}

func TestDecisionEngineOffNeverCallsNetwork(t *testing.T) {
	t.Setenv("TL_STUDIO_STATE_DIR", t.TempDir())
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "must not be called", http.StatusInternalServerError)
	}))
	defer server.Close()

	service := &decisionEngineService{client: server.Client()}
	_, err := service.evaluate(context.Background(), decisionEvaluateRequest{
		State: "x",
		Questions: map[string]decisionQuestion{
			"ok": {Type: "noul", Instructions: "Is x?"},
		},
	})
	var typed *decisionEngineError
	if !errors.As(err, &typed) || typed.Kind != "disabled" {
		t.Fatalf("expected disabled Decision Engine, got %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("Decision Engine off must not make network calls, got %d", calls.Load())
	}
}
