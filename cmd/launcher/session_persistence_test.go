package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestSessionPersistenceSurvivesRestartAndStripsInlineAttachmentData(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	project := t.TempDir()
	store := newSessionPersistenceStore(root, "engine-a")

	session := sessionView{
		ID: "s1", Title: "Persisted session", Directory: project,
		Agent: "code", CreatedAt: 1000, UpdatedAt: 2000,
	}
	if err := store.upsertSession(session); err != nil {
		t.Fatal(err)
	}
	messages := []sessionMessageView{{
		ID: "m1", SessionID: "s1", Role: "user", Text: "hello", CreatedAt: 1500,
		Activities: []sessionActivityView{},
		Attachments: []sessionAttachmentView{{
			Name: "note.txt", MIME: "text/plain", URL: "data:text/plain;base64,aGVsbG8=",
		}},
		Usage: sessionUsage{}, Changes: []sessionChangeView{},
	}}
	if err := store.putMessages(session, messages); err != nil {
		t.Fatal(err)
	}
	if err := store.putChanges(session, []sessionChangeView{{File: "hello.txt", Additions: 1}}); err != nil {
		t.Fatal(err)
	}

	reloaded := newSessionPersistenceStore(root, "engine-b")
	got, ok, err := reloaded.getSession("s1")
	if err != nil || !ok {
		t.Fatalf("session reload failed: ok=%v err=%v", ok, err)
	}
	if got.Title != "Persisted session" || got.Directory != project {
		t.Fatalf("session metadata mismatch: %#v", got)
	}

	gotMessages, ok, err := reloaded.getMessages("s1")
	if err != nil || !ok || len(gotMessages) != 1 {
		t.Fatalf("message reload failed: ok=%v err=%v messages=%#v", ok, err, gotMessages)
	}
	if gotMessages[0].Text != "hello" {
		t.Fatalf("persisted message text mismatch: %#v", gotMessages[0])
	}
	if gotMessages[0].Attachments[0].URL != "" {
		t.Fatalf("inline attachment data must not be duplicated in session persistence")
	}

	gotChanges, ok, err := reloaded.getChanges("s1")
	if err != nil || !ok || len(gotChanges) != 1 || gotChanges[0].File != "hello.txt" {
		t.Fatalf("change reload failed: ok=%v err=%v changes=%#v", ok, err, gotChanges)
	}
}

func TestSessionReadFallsBackToTLStudioPersistenceWhenRuntimeHistoryIsUnavailable(t *testing.T) {
	project := t.TempDir()
	root := filepath.Join(t.TempDir(), "sessions")
	store := newSessionPersistenceStore(root, "kilo-code")
	session := sessionView{
		ID: "persisted", Title: "Offline history", Directory: project,
		CreatedAt: 1000, UpdatedAt: 2000,
	}
	if err := store.upsertSession(session); err != nil {
		t.Fatal(err)
	}
	if err := store.putMessages(session, []sessionMessageView{{
		ID: "m1", SessionID: "persisted", Role: "assistant", Text: "saved answer",
		Activities: []sessionActivityView{}, Attachments: []sessionAttachmentView{},
		Usage: sessionUsage{}, Changes: []sessionChangeView{},
	}}); err != nil {
		t.Fatal(err)
	}

	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, jsonError{Error: "runtime history missing"})
	}))
	defer runtimeServer.Close()

	state := &appState{project: project}
	contract, err := newSessionReadContract(state, runtimeServer.URL, "runtime", "secret")
	if err != nil {
		t.Fatal(err)
	}
	contract.history = newProjectHistoryStoreForTest(filepath.Join(t.TempDir(), "projects.json"))
	contract.history.remember(project)
	contract.store = newSessionPersistenceStore(root, "kilo-code")

	sessions, err := contract.listSessions(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "persisted" {
		t.Fatalf("persisted session list fallback mismatch: %#v", sessions)
	}

	got, err := contract.getSession(context.Background(), "persisted", project)
	if err != nil || got.Title != "Offline history" {
		t.Fatalf("persisted session fallback mismatch: %#v err=%v", got, err)
	}

	messages, err := contract.getMessages(context.Background(), "persisted", project, 50)
	if err != nil || len(messages) != 1 || messages[0].Text != "saved answer" {
		t.Fatalf("persisted message fallback mismatch: %#v err=%v", messages, err)
	}
}

func TestAcceptedRunIsRecordedBeforeRuntimeTranscriptRefresh(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	project := t.TempDir()
	store := newSessionPersistenceStore(root, "engine-a")
	session := sessionView{ID: "s1", Title: "Run", Directory: project, CreatedAt: 1000, UpdatedAt: 1000}
	if err := store.upsertSession(session); err != nil {
		t.Fatal(err)
	}

	err := store.recordAcceptedRun("s1", project, sessionRunInput{
		Text: "continue",
		Agent: "code",
		Model: &sessionModelRef{ProviderID: "test", ID: "model"},
		Parts: []map[string]any{
			{"type": "text", "text": "continue"},
			{"type": "file", "filename": "note.txt", "mime": "text/plain", "url": "data:text/plain;base64,eA=="},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	messages, ok, err := store.getMessages("s1")
	if err != nil || !ok || len(messages) != 1 {
		t.Fatalf("accepted run was not persisted: ok=%v err=%v messages=%#v", ok, err, messages)
	}
	if messages[0].Role != "user" || messages[0].Text != "continue" || len(messages[0].Attachments) != 1 {
		t.Fatalf("accepted run semantic message mismatch: %#v", messages[0])
	}
}

func TestPersistedOnlySessionCanBeRenamedAndDeleted(t *testing.T) {
	project := t.TempDir()
	root := filepath.Join(t.TempDir(), "sessions")
	store := newSessionPersistenceStore(root, "kilo-code")
	session := sessionView{
		ID: "offline", Title: "Original", Directory: project,
		CreatedAt: 1000, UpdatedAt: 2000,
	}
	if err := store.upsertSession(session); err != nil {
		t.Fatal(err)
	}

	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, jsonError{Error: "runtime session missing"})
	}))
	defer runtimeServer.Close()

	state := &appState{project: project}
	backend, err := newRuntimeBackend(
		state,
		runtimeServer.URL,
		runtimeCredentials{Username: "runtime", Password: "secret"},
		kiloRuntimeEngine{},
	)
	if err != nil {
		t.Fatal(err)
	}
	read := newSessionReadContractWithBackend(state, backend)
	read.history = newProjectHistoryStoreForTest(filepath.Join(t.TempDir(), "projects.json"))
	read.history.remember(project)
	read.store = newSessionPersistenceStore(root, "kilo-code")
	commands := newSessionCommandContract(state, backend, read)

	title := "Renamed offline"
	updated, err := commands.update(context.Background(), project, "offline", sessionUpdateInput{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != title {
		t.Fatalf("persisted-only rename failed: %#v", updated)
	}

	if err := commands.remove(context.Background(), project, "offline"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := read.store.getSession("offline"); err != nil || ok {
		t.Fatalf("persisted-only delete failed: ok=%v err=%v", ok, err)
	}
}
