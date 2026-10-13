package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDurableSessionRecoversInterruptedRunOnceWithoutReplaying(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	s := newSessionPersistenceStore(dir, "test")
	session, err := s.createNativeSession(project, sessionCreateInput{Title: "crash test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.recordAcceptedRun(session.ID, project, sessionRunInput{Text: "create a file", MessageID: "req-1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.putNativeMessage(session.ID, project, sessionMessageView{
		ID: "step-1", Role: "assistant", Text: "",
		Activities: []sessionActivityView{{Kind: "tool", ToolID: "files.write", Status: "running", Title: "Write file", Input: map[string]any{"path": "file.txt"}}},
	}); err != nil {
		t.Fatal(err)
	}
	// A new store simulates a fresh process with no surviving in-memory run.
	restored := newSessionPersistenceStore(dir, "test")
	messages, ok, err := restored.getMessages(session.ID)
	if err != nil || !ok {
		t.Fatalf("reload failed: %v %v", ok, err)
	}
	if len(messages) != 3 {
		t.Fatalf("expected user, interrupted tool, recovery note; got %#v", messages)
	}
	if messages[0].Role != "user" || messages[0].ID != "req-1" {
		t.Fatalf("lost accepted prompt: %#v", messages[0])
	}
	if len(messages[1].Activities) != 1 || messages[1].Activities[0].Status != "failed" ||
		messages[1].Activities[0].Error == nil || messages[1].Activities[0].Error.Type != "interrupted" {
		t.Fatalf("in-flight tool outcome should be marked unknown after restart: %#v", messages[1])
	}
	if messages[2].Error == nil || messages[2].Error.Type != "interrupted" {
		t.Fatalf("interruption not visible: %#v", messages[2])
	}
	again := newSessionPersistenceStore(dir, "test")
	reloaded, _, err := again.getMessages(session.ID)
	if err != nil || len(reloaded) != 3 {
		t.Fatalf("recovery generated duplicate interruption: %d %v", len(reloaded), err)
	}
}

func TestDurableSessionSuccessfulRunIsNotMarkedInterrupted(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	s := newSessionPersistenceStore(dir, "test")
	session, err := s.createNativeSession(project, sessionCreateInput{Title: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.recordAcceptedRun(session.ID, project, sessionRunInput{Text: "answer", MessageID: "u-1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.putNativeFinalMessage(session.ID, project, sessionMessageView{
		ID: "final-1", Role: "assistant", Text: "done", CompletedAt: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	restored := newSessionPersistenceStore(dir, "test")
	messages, _, err := restored.getMessages(session.ID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("completed run improperly interrupted: %#v %v", messages, err)
	}
	if messages[1].Text != "done" || messages[1].Error != nil {
		t.Fatalf("final answer was lost: %#v", messages[1])
	}
}

func TestDurableSessionArchivesHistoryRatherThanDroppingMessages(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	s := newSessionPersistenceStore(dir, "test")
	session, err := s.createNativeSession(project, sessionCreateInput{Title: "archive"})
	if err != nil {
		t.Fatal(err)
	}
	messages := make([]sessionMessageView, 1305)
	for i := range messages {
		messages[i] = sessionMessageView{ID: fmt.Sprintf("message-%04d", i), Role: "user", Text: fmt.Sprintf("important-%04d", i)}
	}
	if err := s.putMessages(session, messages); err != nil {
		t.Fatal(err)
	}
	restored := newSessionPersistenceStore(dir, "test")
	all, ok, err := restored.getMessages(session.ID)
	if err != nil || !ok {
		t.Fatalf("archived messages inaccessible: %v %v", ok, err)
	}
	if len(all) != len(messages) {
		t.Fatalf("history was discarded: got=%d want=%d", len(all), len(messages))
	}
	for i := range all {
		if all[i].ID != messages[i].ID || all[i].Text != messages[i].Text {
			t.Fatalf("archive changed history at %d: %#v", i, all[i])
		}
	}
	snapshot, err := restored.loadSnapshotLocked(session.ID)
	if err != nil || snapshot.ArchivedBatches < 1 {
		t.Fatalf("snapshot was not segmented: %#v %v", snapshot.ArchivedBatches, err)
	}
	// Append after archiving: existing archived turns remain intact.
	if err := restored.putNativeMessage(session.ID, project, sessionMessageView{ID: "appended", Role: "assistant", Text: "new"}); err != nil {
		t.Fatal(err)
	}
	last, _, err := newSessionPersistenceStore(dir, "test").getMessages(session.ID)
	if err != nil || len(last) != len(messages)+1 || last[0].ID != "message-0000" || last[len(last)-1].ID != "appended" {
		t.Fatalf("archive append corrupted previous history: %d %v", len(last), err)
	}
}

func TestDurableSessionRefusesOversizedSingleMessageWithoutLosingPriorSnapshot(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	s := newSessionPersistenceStore(dir, "test")
	session, err := s.createNativeSession(project, sessionCreateInput{Title: "size guard"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.putMessages(session, []sessionMessageView{{ID: "safe", Role: "user", Text: "old message"}}); err != nil {
		t.Fatal(err)
	}
	oversized := sessionMessageView{ID: "oversized", Role: "user", Text: strings.Repeat("x", maxSessionSnapshotBytes+1024)}
	if err := s.putMessages(session, []sessionMessageView{oversized}); err == nil {
		t.Fatal("oversized single message silently accepted or truncated")
	}
	got, _, err := newSessionPersistenceStore(dir, "test").getMessages(session.ID)
	if err != nil || len(got) != 1 || got[0].ID != "safe" {
		t.Fatalf("failed save damaged existing history: %#v %v", got, err)
	}
}

func TestDurableSessionReportsMissingArchiveRatherThanSilentlyDroppingHistory(t *testing.T) {
	dir := t.TempDir()
	s := newSessionPersistenceStore(dir, "test")
	session, err := s.createNativeSession(t.TempDir(), sessionCreateInput{Title: "missing archive"})
	if err != nil {
		t.Fatal(err)
	}
	messages := make([]sessionMessageView, 1300)
	for i := range messages {
		messages[i] = sessionMessageView{ID: fmt.Sprint(i), Role: "user", Text: "message"}
	}
	if err := s.putMessages(session, messages); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s.archivePath(session.ID, 1)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := newSessionPersistenceStore(dir, "test").getMessages(session.ID); err == nil {
		t.Fatal("missing archive was silently ignored")
	}
}

func TestDurableSessionDoesNotPruneOlderSessionsWithoutConsent(t *testing.T) {
	dir := t.TempDir()
	s := newSessionPersistenceStore(dir, "test")
	project := t.TempDir()
	const total = 305
	for i := 0; i < total; i++ {
		if err := s.upsertSession(sessionView{ID: fmt.Sprintf("session-%03d", i), Directory: project, CreatedAt: int64(i + 1), UpdatedAt: int64(i + 1)}); err != nil {
			t.Fatal(err)
		}
	}
	reloaded := newSessionPersistenceStore(dir, "test")
	all, err := reloaded.list(0)
	if err != nil || len(all) != total {
		t.Fatalf("sessions pruned unexpectedly: %d %v", len(all), err)
	}
	if _, ok, err := reloaded.getSession("session-000"); err != nil || !ok {
		t.Fatalf("oldest session deleted: %v %v", ok, err)
	}
}

func TestDurableSessionCrashRecoveryDoesNotAutomaticallyExecuteTools(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	s := newSessionPersistenceStore(dir, "test")
	session, err := s.createNativeSession(project, sessionCreateInput{Title: "no replay"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.recordAcceptedRun(session.ID, project, sessionRunInput{Text: "write", MessageID: "u-1"}); err != nil {
		t.Fatal(err)
	}
	counter := filepath.Join(project, "side-effect.txt")
	if err := os.WriteFile(counter, []byte("committed exactly once"), 0o600); err != nil {
		t.Fatal(err)
	}
	restored := newSessionPersistenceStore(dir, "test")
	if _, _, err := restored.getMessages(session.ID); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(counter)
	if err != nil || string(data) != "committed exactly once" {
		t.Fatalf("restart unexpectedly modified workspace: %q %v", data, err)
	}
}

type durabilityWaitingPermission struct {
	entered chan struct{}
	once    sync.Once
}

func (p *durabilityWaitingPermission) AuthorizeNativeTool(ctx context.Context, _ string, _ string, _ toolDescriptor, _ map[string]any) error {
	p.once.Do(func() { close(p.entered) })
	<-ctx.Done()
	return ctx.Err()
}

type durabilityOneCallModel struct{}

func (*durabilityOneCallModel) Complete(_ context.Context, _ nativeModelRequest, _ func(string)) (nativeModelResponse, error) {
	return nativeModelResponse{ToolCalls: []nativeModelToolCall{{ID: "pending-write", Name: "files.write", Arguments: json.RawMessage(`{"path":"never.txt","content":"do not create"}`)}}}, nil
}

func TestDurableAgentJournalsToolBeforePermissionOrExecution(t *testing.T) {
	project := t.TempDir()
	s := newSessionPersistenceStore(filepath.Join(t.TempDir(), "sessions"), "test")
	session, err := s.createNativeSession(project, sessionCreateInput{Title: "journal"})
	if err != nil {
		t.Fatal(err)
	}
	auth := &durabilityWaitingPermission{entered: make(chan struct{})}
	executor := newNativeToolExecutor(newProcessManager(func() string { return project }), auth)
	runtime := newNativeAgentRuntime(
		nativeTestResolver{
			provider: tlProviderDefinition{ID: "test", Protocol: "openai-compatible", BaseURL: "http://127.0.0.1:1/v1"},
			model:    tlProviderModel{ID: "test-model", ToolCall: true},
		},
		&durabilityOneCallModel{}, executor, s, newLiveEventBus(),
	)
	if err := runtime.Start(project, session.ID, sessionRunInput{Text: "Create a file named never.txt in this project.", Agent: "code", Model: &sessionModelRef{ProviderID: "test", ID: "test-model"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-auth.entered:
	case <-time.After(4 * time.Second):
		t.Fatal("tool permission was never entered")
	}
	messages, _, err := s.getMessages(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, msg := range messages {
		for _, act := range msg.Activities {
			if act.ToolID == "files.write" && act.Status == "running" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("tool intent was not persisted before execution: %#v", messages)
	}
	if _, err := os.Stat(filepath.Join(project, "never.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file should not exist yet: %v", err)
	}
	if !runtime.Abort(session.ID) {
		t.Fatal("cannot cancel in-flight run")
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if len(runtime.NativeStatuses(project)) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("agent did not stop after cancellation")
}

func TestDurableSessionArchivesHistoryWhenByteLimitIsExceeded(t *testing.T) {
	dir := t.TempDir()
	s := newSessionPersistenceStore(dir, "test")
	session, err := s.createNativeSession(t.TempDir(), sessionCreateInput{Title: "large messages"})
	if err != nil {
		t.Fatal(err)
	}
	const partBytes = maxSessionSnapshotBytes/2 + 1024
	bigA := strings.Repeat("A", partBytes)
	bigB := strings.Repeat("B", partBytes)
	if err := s.putMessages(session, []sessionMessageView{
		{ID: "a", Role: "user", Text: bigA},
		{ID: "b", Role: "assistant", Text: bigB},
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.loadSnapshotLocked(session.ID)
	if err != nil || snapshot.ArchivedBatches < 1 {
		t.Fatalf("large history was not archived: batches=%d error=%v", snapshot.ArchivedBatches, err)
	}
	restored := newSessionPersistenceStore(dir, "test")
	all, ok, err := restored.getMessages(session.ID)
	if !ok || err != nil || len(all) != 2 {
		t.Fatalf("large archived history was not restored: count=%d error=%v", len(all), err)
	}
	if all[0].ID != "a" || all[0].Text != bigA || all[1].ID != "b" || all[1].Text != bigB {
		t.Fatal("large archived history did not round-trip byte-for-byte")
	}
}

func TestDurableSessionExplicitDeletionRemovesArchivedHistory(t *testing.T) {
	dir := t.TempDir()
	s := newSessionPersistenceStore(dir, "test")
	session, err := s.createNativeSession(t.TempDir(), sessionCreateInput{Title: "deletion"})
	if err != nil {
		t.Fatal(err)
	}
	messages := make([]sessionMessageView, maxPersistedMessages+1)
	for i := range messages {
		messages[i] = sessionMessageView{ID: fmt.Sprint(i), Role: "user", Text: "preserve until user deletes"}
	}
	if err := s.putMessages(session, messages); err != nil {
		t.Fatal(err)
	}
	archive := s.archivePath(session.ID, 1)
	if _, err := os.Stat(archive); err != nil {
		t.Fatal(err)
	}
	if err := s.remove(session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(archive); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("explicit deletion left archived session data behind: %v", err)
	}
}
