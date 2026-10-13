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

const phase1StressRounds = 10
const phase1StressToolsPerRound = 3

type phase1LongRunModel struct {
	mu       sync.Mutex
	calls    int
	finished chan struct{}
	t        *testing.T
}

func (m *phase1LongRunModel) Complete(_ context.Context, request nativeModelRequest, _ func(string)) (nativeModelResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	expected := (m.calls - 1) * phase1StressToolsPerRound
	declared := make(map[string]bool)
	completed := make(map[string]bool)
	for _, message := range request.Messages {
		if message.Role == "assistant" {
			for _, call := range message.ToolCalls {
				if call.ID == "" || declared[call.ID] {
					m.t.Fatalf("round %d: missing or duplicate tool call ID %q", m.calls, call.ID)
				}
				declared[call.ID] = true
			}
		}
		if message.Role == "tool" {
			if len(message.Text) > nativeToolContextMaxBytes {
				m.t.Fatalf("round %d: tool result exceeded context budget (%d bytes)", m.calls, len(message.Text))
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(message.Text), &payload); err != nil {
				m.t.Fatalf("round %d: invalid model-facing tool JSON: %v", m.calls, err)
			}
			if payload["outputTruncated"] != true {
				m.t.Fatalf("round %d: large read result was not context bounded", m.calls)
			}
			if completed[message.ToolCallID] {
				m.t.Fatalf("round %d: repeated tool result ID %q", m.calls, message.ToolCallID)
			}
			completed[message.ToolCallID] = true
		}
	}
	if len(declared) != expected || len(completed) != expected {
		m.t.Fatalf("round %d: declared=%d completed=%d, want %d", m.calls, len(declared), len(completed), expected)
	}
	for id := range completed {
		if !declared[id] {
			m.t.Fatalf("round %d: tool result references missing assistant call %q", m.calls, id)
		}
	}
	if m.calls > phase1StressRounds {
		close(m.finished)
		return nativeModelResponse{Text: "PHASE1_LONG_RUN_OK", FinishReason: "stop"}, nil
	}
	calls := make([]nativeModelToolCall, 0, phase1StressToolsPerRound)
	for i := 0; i < phase1StressToolsPerRound; i++ {
		index := (m.calls-1)*phase1StressToolsPerRound + i
		calls = append(calls, nativeModelToolCall{
			// Intentionally leave IDs blank: normalization must assign unique IDs.
			Name:      "files.read",
			Arguments: json.RawMessage(fmt.Sprintf(`{"path":"stress-%02d.txt"}`, index)),
		})
	}
	return nativeModelResponse{ToolCalls: calls, FinishReason: "tool_calls", Usage: sessionUsage{Input: 8000, Output: 2000}}, nil
}

func TestPhase1AgentThirtyToolsTenRoundsPersistedAndBounded(t *testing.T) {
	project := t.TempDir()
	const total = phase1StressRounds * phase1StressToolsPerRound
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("stress-%02d.txt", i)
		content := strings.Repeat("long UTF-8 content 🌍 ", 2000)
		if err := os.WriteFile(filepath.Join(project, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store := newSessionPersistenceStore(filepath.Join(t.TempDir(), "sessions"), "tl-phase1-long-run")
	session, err := store.createNativeSession(project, sessionCreateInput{Title: "30-tool long run"})
	if err != nil {
		t.Fatal(err)
	}
	model := &phase1LongRunModel{t: t, finished: make(chan struct{})}
	runtime := newNativeAgentRuntime(
		nativeTestResolver{
			provider: tlProviderDefinition{ID: "test", Protocol: "openai-compatible", BaseURL: "http://127.0.0.1:1/v1"},
			model:    tlProviderModel{ID: "test-model", ToolCall: true, ContextLimit: 200000, OutputLimit: 8192},
		},
		model,
		newNativeToolExecutor(newProcessManager(func() string { return project }), nativeAllowAuthorizer{}),
		store,
		newLiveEventBus(),
	)
	if err := runtime.Start(project, session.ID, sessionRunInput{
		Text:  "Inspect the 30 stress files and report findings without changing files.",
		Agent: "code",
		Model: &sessionModelRef{ProviderID: "test", ID: "test-model"},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-model.finished:
	case <-time.After(15 * time.Second):
		t.Fatal("30-tool agent simulation timed out")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, active := runtime.NativeStatuses(project)[session.ID]; !active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("agent did not become idle")
		}
		time.Sleep(15 * time.Millisecond)
	}
	messages, ok, err := store.getMessages(session.ID)
	if err != nil || !ok {
		t.Fatalf("read persisted session: ok=%v err=%v", ok, err)
	}
	toolActivities := 0
	finalFound := false
	for _, message := range messages {
		if message.Text == "PHASE1_LONG_RUN_OK" {
			finalFound = true
		}
		for _, activity := range message.Activities {
			if activity.Kind == "tool" {
				toolActivities++
				if activity.Status != "completed" {
					t.Fatalf("persisted tool activity failed: %+v", activity)
				}
			}
		}
	}
	if toolActivities != total || !finalFound {
		t.Fatalf("persisted session incomplete: tool activities=%d want=%d final=%v", toolActivities, total, finalFound)
	}
}
