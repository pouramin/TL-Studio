package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// This model deliberately tries two writes in one tool batch. If the runtime
// were to ask for another model turn after a rejection, it would also request
// the denied operation a second time.
type nativeRejectedWriteModel struct {
	calls atomic.Int32
}

func (m *nativeRejectedWriteModel) Complete(_ context.Context, _ nativeModelRequest, _ func(string)) (nativeModelResponse, error) {
	if m.calls.Add(1) == 1 {
		return nativeModelResponse{
			ToolCalls: []nativeModelToolCall{
				{
					ID:        "denied-write",
					Name:      "files.write",
					Arguments: json.RawMessage(`{"path":"permission-deny-test.txt","content":"SHOULD_NOT_EXIST"}`),
				},
				{
					ID:        "unauthorized-followup",
					Name:      "files.write",
					Arguments: json.RawMessage(`{"path":"second-write.txt","content":"SHOULD_NOT_EXIST"}`),
				},
			},
			FinishReason: "tool_calls",
		}, nil
	}
	return nativeModelResponse{
		ToolCalls: []nativeModelToolCall{{
			ID:        "model-retry",
			Name:      "files.write",
			Arguments: json.RawMessage(`{"path":"permission-deny-test.txt","content":"SHOULD_NOT_EXIST"}`),
		}},
		FinishReason: "tool_calls",
	}, nil
}

func TestNativeAgentExplicitPermissionRejectStopsToolBatchAndModelRetries(t *testing.T) {
	project := t.TempDir()
	store := newSessionPersistenceStore(filepath.Join(t.TempDir(), "sessions"), "permission-reject")
	session, err := store.createNativeSession(project, sessionCreateInput{Title: "Reject must stop"})
	if err != nil {
		t.Fatal(err)
	}
	permissions := &permissionEngine{
		state:         &appState{project: project},
		store:         newPermissionPolicyStore(filepath.Join(t.TempDir(), "permissions.json")),
		nativePending: map[string]*nativePermissionWaiter{},
	}
	model := &nativeRejectedWriteModel{}
	executor := newNativeToolExecutor(newProcessManager(func() string { return project }), permissions)
	resolver := nativeTestResolver{
		provider: tlProviderDefinition{ID: "test", Protocol: "openai-compatible", BaseURL: "http://127.0.0.1:1/v1"},
		model:    tlProviderModel{ID: "test-model", ToolCall: true},
	}
	runtime := newNativeAgentRuntime(resolver, model, executor, store, newLiveEventBus())

	if err := runtime.Start(project, session.ID, sessionRunInput{
		Text: "Create permission-deny-test.txt; do not retry if denied.",
		Agent: "code",
		Model: &sessionModelRef{ProviderID: "test", ID: "test-model"},
	}); err != nil {
		t.Fatal(err)
	}

	var pending []map[string]any
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		pending = permissions.nativePendingSnapshot(session.ID)
		if len(pending) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(pending) != 1 {
		t.Fatalf("expected exactly one pending write permission, got %#v", pending)
	}
	if _, handled, err := permissions.replyNativePermission(permissionID(pending[0]), session.ID, "reject"); !handled || err != nil {
		t.Fatalf("could not reject permission: handled=%v err=%v", handled, err)
	}

	// A model continuation or second tool in the same response would normally
	// create another pending permission. Instead, the entire run must end.
	for time.Now().Before(deadline) {
		if _, running := runtime.NativeStatuses(project)[session.ID]; !running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, running := runtime.NativeStatuses(project)[session.ID]; running {
		t.Fatal("agent continued after explicit rejection")
	}
	if pending := permissions.nativePendingSnapshot(session.ID); len(pending) != 0 {
		t.Fatalf("agent requested another permission after rejection: %#v", pending)
	}
	if got := model.calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 model turn before rejection, got %d", got)
	}
	for _, name := range []string{"permission-deny-test.txt", "second-write.txt"} {
		if _, err := os.Stat(filepath.Join(project, name)); !os.IsNotExist(err) {
			t.Fatalf("rejected operation created %s (stat error=%v)", name, err)
		}
	}

	messages, _, err := store.getMessages(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	var deniedToolActivities, unexpectedToolActivities, deniedRunErrors int
	for _, message := range messages {
		if message.Error != nil && message.Error.Type == "permission_denied" {
			deniedRunErrors++
			if !strings.Contains(message.Error.Message, "Permission rejected by user") ||
				!strings.Contains(message.Error.Message, "unfinished") {
				t.Fatalf("misleading denial summary: %q", message.Error.Message)
			}
		}
		for _, activity := range message.Activities {
			if activity.Kind != "tool" {
				continue
			}
			if activity.Error != nil && strings.Contains(activity.Error.Message, "tool permission was rejected") {
				deniedToolActivities++
			} else {
				unexpectedToolActivities++
			}
		}
	}
	if deniedToolActivities != 1 || deniedRunErrors != 1 || unexpectedToolActivities != 0 {
		t.Fatalf("expected one persisted denied tool and one truthful final denial, got deniedTools=%d deniedRuns=%d otherTools=%d messages=%#v",
			deniedToolActivities, deniedRunErrors, unexpectedToolActivities, messages)
	}
}

func TestNativeToolOrdinaryFailureIsNotMisclassifiedAsUserRejection(t *testing.T) {
	project := t.TempDir()
	executor := newNativeToolExecutor(newProcessManager(func() string { return project }), nativeAllowAuthorizer{})
	result := executor.Execute(context.Background(), "session", project, nativeToolCall{
		ID: "files.read", Arguments: json.RawMessage(`{"path":"missing-file.txt"}`),
	})
	if result.Error == "" {
		t.Fatal("missing file was expected to fail")
	}
	if result.PermissionRejected {
		t.Fatalf("ordinary file error was misclassified as permission rejection: %+v", result)
	}
}
