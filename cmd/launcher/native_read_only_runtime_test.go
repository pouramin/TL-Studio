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

type nativeReadOnlyMutationProbeModel struct {
	mu       sync.Mutex
	calls    int
	finished chan struct{}
	t        *testing.T
}

func (m *nativeReadOnlyMutationProbeModel) Complete(_ context.Context, request nativeModelRequest, _ func(string)) (nativeModelResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++

	switch m.calls {
	case 1:
		for _, tool := range request.Tools {
			if tool.ID == "files.write" || tool.ID == "files.edit" || tool.ID == "terminal.command" {
				m.t.Fatalf("read-only request exposed mutating tool %q", tool.ID)
			}
		}
		// Simulate a model hallucinating a tool that was not advertised. Runtime
		// enforcement must still block the mutation.
		return nativeModelResponse{
			ToolCalls: []nativeModelToolCall{{
				ID:        "hallucinated-write",
				Name:      "files.write",
				Arguments: json.RawMessage(`{"path":"must-not-exist.txt","content":"BAD"}`),
			}},
			FinishReason: "tool_calls",
			Usage:        sessionUsage{Input: 100, Output: 20},
		}, nil
	case 2:
		blocked := false
		for _, message := range request.Messages {
			if message.Role == "tool" && message.ToolCallID == "hallucinated-write" &&
				strings.Contains(message.Text, "tool blocked by TL Studio") {
				blocked = true
				break
			}
		}
		if !blocked {
			m.t.Fatalf("model continuation did not receive read-only block result: %#v", request.Messages)
		}
		close(m.finished)
		return nativeModelResponse{
			Text:         "READ_ONLY_MUTATION_BLOCKED",
			FinishReason: "stop",
			Usage:        sessionUsage{Input: 120, Output: 12},
		}, nil
	default:
		m.t.Fatalf("unexpected model call %d", m.calls)
		return nativeModelResponse{}, nil
	}
}

func TestNativeReadOnlyRuntimeBlocksHallucinatedMutationTool(t *testing.T) {
	project := t.TempDir()
	store := newSessionPersistenceStore(filepath.Join(t.TempDir(), "sessions"), "tl-read-only-runtime-test")
	session, err := store.createNativeSession(project, sessionCreateInput{Title: "Read-only proof"})
	if err != nil {
		t.Fatal(err)
	}

	model := &nativeReadOnlyMutationProbeModel{finished: make(chan struct{}), t: t}
	resolver := nativeTestResolver{
		provider: tlProviderDefinition{ID: "test", Protocol: "openai-compatible", BaseURL: "http://127.0.0.1:1/v1"},
		model:    tlProviderModel{ID: "test-model", ToolCall: true, ContextLimit: 32768, OutputLimit: 4096},
	}
	executor := newNativeToolExecutor(newProcessManager(func() string { return project }), nativeAllowAuthorizer{})
	runtime := newNativeAgentRuntime(resolver, model, executor, store, newLiveEventBus())

	input := sessionRunInput{
		Text:  "همه فایل‌ها را بخوان و تحلیل کن و نتیجه را بگو",
		Agent: "code",
		Model: &sessionModelRef{ProviderID: "test", ID: "test-model"},
	}
	if err := runtime.Start(project, session.ID, input); err != nil {
		t.Fatal(err)
	}

	select {
	case <-model.finished:
	case <-time.After(3 * time.Second):
		t.Fatal("read-only runtime did not complete")
	}

	if _, err := os.Stat(filepath.Join(project, "must-not-exist.txt")); !os.IsNotExist(err) {
		t.Fatalf("read-only turn mutated the workspace; stat err=%v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		messages, ok, err := store.getMessages(session.ID)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			for _, message := range messages {
				if message.Role == "assistant" && message.Text == "READ_ONLY_MUTATION_BLOCKED" {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("read-only final response not persisted: %#v", messages)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
