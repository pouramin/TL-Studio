package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type phase1ModelRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn phase1ModelRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestPhase1OfficialMistralStreamingUsesPromptCacheAndParsesUsage(t *testing.T) {
	var received map[string]any
	client := &nativeHTTPModelClient{
		httpClient: &http.Client{Transport: phase1ModelRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.String() != "https://api.mistral.ai/v1/chat/completions" {
				t.Fatalf("unexpected Mistral endpoint %s", request.URL.String())
			}
			if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
				t.Fatal(err)
			}
			body := "data: {\"model\":\"mistral-large-4\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}],\"usage\":{}}\n\n" +
				"data: {\"model\":\"mistral-large-4\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"ee61e87d-25d8-4bad-8e47-02939eb234c5\",\"function\":{\"name\":\"tl_files__write\",\"arguments\":\"{\\\"path\\\":\\\"x.txt\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":20,\"prompt_tokens_details\":{\"cached_tokens\":896}}}\n\n" +
				"data: [DONE]\n\n"
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    request,
			}, nil
		})},
		bridges: map[string]nativeModelBridge{},
	}

	response, err := client.Complete(context.Background(), nativeModelRequest{
		Provider: tlProviderDefinition{Protocol: "openai-compatible", BaseURL: "https://api.mistral.ai/v1"},
		Model:    tlProviderModel{ID: "mistral-large-4", ToolCall: true},
		APIKey:   "mistral-secret",
		CacheKey: "tlstudio-session-proof",
		Messages: []nativeConversationMessage{{Role: "user", Text: "inspect the project"}},
		Tools:    []nativeModelToolDefinition{nativeTestToolDefinition()},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := received["stream_options"]; ok {
		t.Fatalf("official Mistral does not accept OpenAI stream_options: %#v", received)
	}
	if received["prompt_cache_key"] != "tlstudio-session-proof" {
		t.Fatalf("Mistral prompt cache key missing: %#v", received["prompt_cache_key"])
	}
	if response.Text != "ok" {
		t.Fatalf("unexpected Mistral text %q", response.Text)
	}
	if response.Usage.Input != 104 || response.Usage.Output != 20 || response.Usage.CacheRead != 896 {
		t.Fatalf("unexpected Mistral usage %#v", response.Usage)
	}
	if len(response.ToolCalls) != 1 {
		t.Fatalf("expected one Mistral Tool call, got %#v", response.ToolCalls)
	}
	callID := response.ToolCalls[0].ID
	if len(callID) != 9 {
		t.Fatalf("Mistral Tool-call ID must be 9 characters, got %q", callID)
	}
	for _, ch := range callID {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') {
			t.Fatalf("Mistral Tool-call ID must be alphanumeric, got %q", callID)
		}
	}
}

func TestPhase1NonMistralOpenAICompatibleDoesNotInjectMistralFields(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`)
	}))
	defer server.Close()

	_, err := newNativeModelClient().Complete(context.Background(), nativeModelRequest{
		Provider: tlProviderDefinition{Protocol: "openai-compatible", BaseURL: server.URL + "/v1"},
		Model:    tlProviderModel{ID: "generic-model", ToolCall: true},
		APIKey:   "secret",
		CacheKey: "tlstudio-should-not-leak",
		Messages: []nativeConversationMessage{{Role: "user", Text: "hello"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := received["stream_options"]; ok {
		t.Fatalf("generic OpenAI-compatible request received Mistral-only stream_options: %#v", received)
	}
	if _, ok := received["prompt_cache_key"]; ok {
		t.Fatalf("generic OpenAI-compatible request received Mistral-only prompt_cache_key: %#v", received)
	}
}
