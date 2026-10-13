package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type phase1BlankToolIDModel struct {
	mu       sync.Mutex
	calls    int
	finished chan struct{}
	t        *testing.T
}

func (m *phase1BlankToolIDModel) Complete(_ context.Context, request nativeModelRequest, _ func(string)) (nativeModelResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	switch m.calls {
	case 1:
		return nativeModelResponse{
			ToolCalls: []nativeModelToolCall{{
				Name:      "files.read",
				Arguments: json.RawMessage(`{"path":"x.txt"}`),
			}},
			FinishReason: "tool_calls",
		}, nil
	case 2:
		assistantCallID := ""
		toolResultID := ""
		for _, message := range request.Messages {
			if message.Role == "assistant" && len(message.ToolCalls) == 1 && message.ToolCalls[0].Name == "files.read" {
				assistantCallID = strings.TrimSpace(message.ToolCalls[0].ID)
			}
			if message.Role == "tool" && message.ToolName == "files.read" {
				toolResultID = strings.TrimSpace(message.ToolCallID)
			}
		}
		if assistantCallID == "" || toolResultID == "" || assistantCallID != toolResultID {
			m.t.Fatalf("generated Tool-call ID was not preserved across continuation: assistant=%q tool=%q messages=%#v", assistantCallID, toolResultID, request.Messages)
		}
		close(m.finished)
		return nativeModelResponse{Text: "BLANK_TOOL_ID_RECOVERED", FinishReason: "stop"}, nil
	default:
		m.t.Fatalf("unexpected model call %d", m.calls)
		return nativeModelResponse{}, nil
	}
}

func TestPhase1NativeAgentPreservesGeneratedToolCallIDInConversation(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "x.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newSessionPersistenceStore(filepath.Join(t.TempDir(), "sessions"), "tl-native-tool-id-test")
	session, err := store.createNativeSession(project, sessionCreateInput{Title: "Tool ID proof"})
	if err != nil {
		t.Fatal(err)
	}

	model := &phase1BlankToolIDModel{finished: make(chan struct{}), t: t}
	resolver := nativeTestResolver{
		provider: tlProviderDefinition{ID: "test", Protocol: "openai-compatible", BaseURL: "http://127.0.0.1:1/v1"},
		model:    tlProviderModel{ID: "test-model", ToolCall: true},
	}
	executor := newNativeToolExecutor(newProcessManager(func() string { return project }), nativeAllowAuthorizer{})
	runtime := newNativeAgentRuntime(resolver, model, executor, store, newLiveEventBus())

	if err := runtime.Start(project, session.ID, sessionRunInput{
		Text: "Inspect x.txt", Agent: "code",
		Model: &sessionModelRef{ProviderID: "test", ID: "test-model"},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-model.finished:
	case <-time.After(3 * time.Second):
		t.Fatal("blank Tool-call ID continuation did not complete")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, running := runtime.NativeStatuses(project)[session.ID]; !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("blank Tool-call ID run did not become idle")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

type phase1SeparatedRepeatModel struct {
	mu       sync.Mutex
	calls    int
	finished chan struct{}
	t        *testing.T
}

func (m *phase1SeparatedRepeatModel) Complete(_ context.Context, request nativeModelRequest, _ func(string)) (nativeModelResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.calls <= 9 {
		path := "a.txt"
		if m.calls%2 == 0 {
			path = "b.txt"
		}
		return nativeModelResponse{
			ToolCalls: []nativeModelToolCall{{
				ID:        fmt.Sprintf("call-%d", m.calls),
				Name:      "files.read",
				Arguments: json.RawMessage(fmt.Sprintf(`{"path":%q}`, path)),
			}},
			FinishReason: "tool_calls",
		}, nil
	}
	if m.calls == 10 {
		close(m.finished)
		return nativeModelResponse{Text: "SEPARATED_REPEATS_OK", FinishReason: "stop"}, nil
	}
	m.t.Fatalf("unexpected model call %d", m.calls)
	return nativeModelResponse{}, nil
}

func TestPhase1NativeAgentRepeatGuardOnlyCountsConsecutiveRounds(t *testing.T) {
	project := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(project, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store := newSessionPersistenceStore(filepath.Join(t.TempDir(), "sessions"), "tl-native-separated-repeat-test")
	session, err := store.createNativeSession(project, sessionCreateInput{Title: "Separated repeats"})
	if err != nil {
		t.Fatal(err)
	}
	model := &phase1SeparatedRepeatModel{finished: make(chan struct{}), t: t}
	resolver := nativeTestResolver{
		provider: tlProviderDefinition{ID: "test", Protocol: "openai-compatible", BaseURL: "http://127.0.0.1:1/v1"},
		model:    tlProviderModel{ID: "test-model", ToolCall: true},
	}
	runtime := newNativeAgentRuntime(
		resolver,
		model,
		newNativeToolExecutor(newProcessManager(func() string { return project }), nativeAllowAuthorizer{}),
		store,
		newLiveEventBus(),
	)

	if err := runtime.Start(project, session.ID, sessionRunInput{
		Text: "Inspect both files repeatedly as needed", Agent: "code",
		Model: &sessionModelRef{ProviderID: "test", ID: "test-model"},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-model.finished:
	case <-time.After(3 * time.Second):
		t.Fatal("non-consecutive repeat sequence was incorrectly blocked")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, running := runtime.NativeStatuses(project)[session.ID]; !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("non-consecutive repeat run did not become idle")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

type phase1ConsecutiveRepeatModel struct {
	mu    sync.Mutex
	calls int
}

func (m *phase1ConsecutiveRepeatModel) Complete(_ context.Context, _ nativeModelRequest, _ func(string)) (nativeModelResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return nativeModelResponse{
		ToolCalls: []nativeModelToolCall{{
			ID:        fmt.Sprintf("repeat-%d", m.calls),
			Name:      "files.read",
			Arguments: json.RawMessage(`{"path":"a.txt"}`),
		}},
		FinishReason: "tool_calls",
	}, nil
}

func TestPhase1NativeAgentRepeatGuardStillStopsConsecutiveLoop(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newSessionPersistenceStore(filepath.Join(t.TempDir(), "sessions"), "tl-native-consecutive-repeat-test")
	session, err := store.createNativeSession(project, sessionCreateInput{Title: "Consecutive repeats"})
	if err != nil {
		t.Fatal(err)
	}
	model := &phase1ConsecutiveRepeatModel{}
	resolver := nativeTestResolver{
		provider: tlProviderDefinition{ID: "test", Protocol: "openai-compatible", BaseURL: "http://127.0.0.1:1/v1"},
		model:    tlProviderModel{ID: "test-model", ToolCall: true},
	}
	runtime := newNativeAgentRuntime(
		resolver,
		model,
		newNativeToolExecutor(newProcessManager(func() string { return project }), nativeAllowAuthorizer{}),
		store,
		newLiveEventBus(),
	)
	if err := runtime.Start(project, session.ID, sessionRunInput{
		Text: "Inspect a.txt", Agent: "code",
		Model: &sessionModelRef{ProviderID: "test", ID: "test-model"},
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		messages, ok, err := store.getMessages(session.ID)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			for _, message := range messages {
				if message.Role == "assistant" && message.Error != nil &&
					strings.Contains(message.Error.Message, "consecutive times") {
					if model.calls != nativeAgentMaxRepeatedCalls+1 {
						t.Fatalf("repeat guard fired after %d calls, want %d", model.calls, nativeAgentMaxRepeatedCalls+1)
					}
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("consecutive repeat guard did not persist failure: %#v", messages)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
