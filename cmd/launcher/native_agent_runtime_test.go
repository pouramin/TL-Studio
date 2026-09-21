package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type nativeTestResolver struct {
	provider tlProviderDefinition
	model    tlProviderModel
}

func (r nativeTestResolver) resolveNativeModel(providerID, modelID string) (tlProviderDefinition, tlProviderModel, string, error) {
	return r.provider, r.model, "test-key", nil
}

type nativeAllowAuthorizer struct{}

func (nativeAllowAuthorizer) AuthorizeNativeTool(context.Context, string, string, toolDescriptor, map[string]any) error {
	return nil
}

type nativeLoopFakeModel struct {
	mu       sync.Mutex
	calls    int
	finished chan struct{}
	t        *testing.T
}

func (m *nativeLoopFakeModel) Complete(_ context.Context, request nativeModelRequest, _ func(string)) (nativeModelResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	switch m.calls {
	case 1:
		return nativeModelResponse{
			ToolCalls: []nativeModelToolCall{{
				ID:        "call-1",
				Name:      "files.write",
				Arguments: json.RawMessage(`{"path":"hello.txt","content":"TL_STUDIO_NATIVE_OK"}`),
			}},
			FinishReason: "tool_calls",
			Usage:        sessionUsage{Input: 7, Output: 3},
		}, nil
	case 2:
		found := false
		for _, message := range request.Messages {
			if message.Role == "tool" && message.ToolCallID == "call-1" &&
				strings.Contains(message.Text, `"path":"hello.txt"`) &&
				strings.Contains(message.Text, `"ok":true`) {
				found = true
				break
			}
		}
		if !found {
			m.t.Errorf("second model turn did not receive the real tool result: %#v", request.Messages)
		}
		close(m.finished)
		return nativeModelResponse{
			Text:         "NATIVE_AGENT_OK",
			FinishReason: "stop",
			Usage:        sessionUsage{Input: 11, Output: 5},
		}, nil
	default:
		m.t.Fatalf("unexpected extra model call %d", m.calls)
		return nativeModelResponse{}, nil
	}
}

func TestNativeAgentLoopExecutesToolAndContinuesWithoutKilo(t *testing.T) {
	project := t.TempDir()
	store := newSessionPersistenceStore(filepath.Join(t.TempDir(), "sessions"), "tl-native-test")
	session, err := store.createNativeSession(project, sessionCreateInput{Title: "Native proof"})
	if err != nil {
		t.Fatal(err)
	}

	model := &nativeLoopFakeModel{finished: make(chan struct{}), t: t}
	resolver := nativeTestResolver{
		provider: tlProviderDefinition{
			ID: "test",
			Protocol: "openai-compatible",
			BaseURL: "http://127.0.0.1:1/v1",
		},
		model: tlProviderModel{ID: "test-model", ToolCall: true},
	}
	executor := newNativeToolExecutor(newProcessManager(func() string { return project }), nativeAllowAuthorizer{})
	runtime := newNativeAgentRuntime(resolver, model, executor, store, newLiveEventBus())

	input := sessionRunInput{
		Text:  "Create hello.txt",
		Agent: "code",
		Model: &sessionModelRef{ProviderID: "test", ID: "test-model"},
	}
	if err := runtime.Start(project, session.ID, input); err != nil {
		t.Fatal(err)
	}

	select {
	case <-model.finished:
	case <-time.After(3 * time.Second):
		t.Fatal("native Agent did not complete model/tool/model loop")
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		messages, ok, err := store.getMessages(session.ID)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			for _, message := range messages {
				if message.Role == "assistant" && message.Text == "NATIVE_AGENT_OK" {
					data, err := os.ReadFile(filepath.Join(project, "hello.txt"))
					if err != nil {
						t.Fatal(err)
					}
					if string(data) != "TL_STUDIO_NATIVE_OK" {
						t.Fatalf("unexpected file contents %q", string(data))
					}
					if model.calls != 2 {
						t.Fatalf("expected 2 model turns, got %d", model.calls)
					}
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("final native assistant message was not persisted: %#v", messages)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestNativeToolExecutorRejectsTraversalAndUnknownTools(t *testing.T) {
	project := t.TempDir()
	executor := newNativeToolExecutor(newProcessManager(func() string { return project }), nativeAllowAuthorizer{})

	traversal := executor.Execute(context.Background(), "s1", project, nativeToolCall{
		ID: "files.write",
		Arguments: json.RawMessage(`{"path":"../escape.txt","content":"no"}`),
	})
	if traversal.Error == "" {
		t.Fatal("expected path traversal to be rejected")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(project), "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("escape file should not exist, stat err=%v", err)
	}

	unknown := executor.Execute(context.Background(), "s1", project, nativeToolCall{
		ID:        "runtime.mystery",
		Arguments: json.RawMessage(`{}`),
	})
	if unknown.Error == "" {
		t.Fatal("expected unknown tool to be rejected")
	}
}
