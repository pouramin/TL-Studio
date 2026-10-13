package main

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestNativeFailurePersistsContextActivities(t *testing.T) {
	project := t.TempDir()
	store := newSessionPersistenceStore(filepath.Join(t.TempDir(), "sessions"), "tl-context-failure-test")
	session, err := store.createNativeSession(project, sessionCreateInput{Title: "Context failure"})
	if err != nil {
		t.Fatal(err)
	}

	runtime := &nativeAgentRuntime{store: store, events: newLiveEventBus()}
	contextActivity := sessionActivityView{
		Kind: "context", Status: "warning", Title: "Run token budget reached",
		Metadata: map[string]any{"usedTokens": int64(250000), "runTokenBudget": int64(262144)},
	}
	runErr := nativeErrorWithActivities(errors.New("budget stop"), []sessionActivityView{contextActivity})
	runtime.persistFailure(project, session.ID, sessionRunInput{
		Agent: "code",
		Model: &sessionModelRef{ProviderID: "test", ID: "test-model"},
	}, runErr)

	messages, ok, err := store.getMessages(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("session messages missing")
	}
	for _, message := range messages {
		if message.Error == nil || message.Error.Message != "budget stop" {
			continue
		}
		if len(message.Activities) != 1 || message.Activities[0].Kind != "context" ||
			message.Activities[0].Title != "Run token budget reached" {
			t.Fatalf("context failure activity was not persisted: %#v", message.Activities)
		}
		return
	}
	t.Fatalf("visible failure message not found: %#v", messages)
}
