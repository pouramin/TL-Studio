package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeAgentHTTPModelToolModelProofWithoutKilo(t *testing.T) {
	project := t.TempDir()
	store := newSessionPersistenceStore(filepath.Join(t.TempDir(), "sessions"), "native-proof")
	session, err := store.createNativeSession(project, sessionCreateInput{Title: "HTTP native proof"})
	if err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected model path %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			_, _ = io.WriteString(w, `{
				"choices":[{"message":{"content":null,"tool_calls":[{"id":"proof-call","type":"function","function":{"name":"tl_files__write","arguments":"{\"path\":\"proof.txt\",\"content\":\"NATIVE_HTTP_PROOF\"}"}}]},"finish_reason":"tool_calls"}],
				"usage":{"prompt_tokens":8,"completion_tokens":3}
			}`)
			return
		}
		if n == 2 {
			messages, _ := payload["messages"].([]any)
			sawToolResult := false
			for _, raw := range messages {
				message, _ := raw.(map[string]any)
				if message["role"] == "tool" {
					content, _ := message["content"].(string)
					if strings.Contains(content, `"ok":true`) && strings.Contains(content, `"path":"proof.txt"`) {
						sawToolResult = true
					}
				}
			}
			if !sawToolResult {
				t.Fatalf("second HTTP model turn did not receive TL Studio tool result: %#v", payload)
			}
			_, _ = io.WriteString(w, `{
				"choices":[{"message":{"content":"NATIVE_HTTP_FINAL"},"finish_reason":"stop"}],
				"usage":{"prompt_tokens":12,"completion_tokens":4}
			}`)
			return
		}
		t.Fatalf("unexpected model call %d", n)
	}))
	defer modelServer.Close()

	resolver := nativeTestResolver{
		provider: tlProviderDefinition{
			ID:       "proof",
			Protocol: "openai-compatible",
			BaseURL:  modelServer.URL + "/v1",
		},
		model: tlProviderModel{ID: "proof-model", ToolCall: true},
	}
	runtime := newNativeAgentRuntime(
		resolver,
		newNativeModelClient(),
		newNativeToolExecutor(newProcessManager(func() string { return project }), nativeAllowAuthorizer{}),
		store,
		newLiveEventBus(),
	)
	if err := runtime.Start(project, session.ID, sessionRunInput{
		Text: "Create proof.txt",
		Model: &sessionModelRef{ProviderID: "proof", ID: "proof-model"},
		Agent: "code",
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		messages, _, readErr := store.getMessages(session.ID)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, message := range messages {
			if message.Role == "assistant" && message.Text == "NATIVE_HTTP_FINAL" {
				data, err := os.ReadFile(filepath.Join(project, "proof.txt"))
				if err != nil {
					t.Fatal(err)
				}
				if string(data) != "NATIVE_HTTP_PROOF" {
					t.Fatalf("unexpected proof file %q", string(data))
				}
				if calls.Load() != 2 {
					t.Fatalf("expected exactly 2 HTTP model turns, got %d", calls.Load())
				}
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("native HTTP model/tool/model proof did not complete")
}

func TestNativeTerminalCancellationStopsProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("process cancellation test")
	}
	project := t.TempDir()
	executor := newNativeToolExecutor(newProcessManager(func() string { return project }), nativeAllowAuthorizer{})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	command := "sleep 5"
	result := executor.Execute(ctx, "cancel-session", project, nativeToolCall{
		ID:        "terminal.command",
		Arguments: json.RawMessage(`{"command":"sleep 5","timeoutSeconds":5}`),
	})
	if result.Error == "" {
		t.Fatal("expected cancelled terminal command to return an error")
	}
}
