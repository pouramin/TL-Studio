package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRecentSessionMessagesSkipUnneededArchives(t *testing.T) {
	t.Setenv("TL_STUDIO_SQLITE_SESSIONS", "")
	store := newSessionPersistenceStore(t.TempDir(), "test")
	session, err := store.createNativeSession(t.TempDir(), sessionCreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := persistedSessionSnapshot{Session: session, ArchivedBatches: 2, Messages: []sessionMessageView{{ID: "new", Role: "assistant", Text: "new"}}}
	if err := writePrivateJSONAtomic(store.archivePath(session.ID, 2), persistedSessionArchive{Version: sessionPersistenceVersion, SessionID: session.ID, Messages: []sessionMessageView{{ID: "older", Text: "older"}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.archivePath(session.ID, 1), []byte("damaged old archive"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.saveSnapshotLocked(snapshot); err != nil {
		t.Fatal(err)
	}
	recent, _, err := store.getRecentMessages(session.ID, 2)
	if err != nil || len(recent) != 2 || recent[0].Text != "older" || recent[1].Text != "new" {
		t.Fatalf("recent tail read wrong archives: %+v %v", recent, err)
	}
	if _, _, err := store.getMessages(session.ID); err == nil {
		t.Fatal("full history must still surface corruption")
	}
}

func TestNativeAttachmentsRejectUnsupportedPayloadsBeforeAcceptance(t *testing.T) {
	for _, part := range []map[string]any{
		{"type": "file", "mime": "image/png", "url": "data:image/png;base64,YQ=="},
		{"type": "file", "mime": "text/plain", "url": "data:text/plain;base64,invalid!!"},
		{"type": "file", "mime": "text/plain", "url": "data:text/plain;base64," + strings.Repeat("A", 2<<20)},
		{"type": "file", "mime": "text/plain", "url": "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", nativeAttachmentMaxBytes+1)))},
		{"type": "file", "mime": "text/plain", "url": "data:text/plain-extra;base64,YQ=="},
	} {
		if _, err := nativeTextAttachments([]map[string]any{part}); err == nil {
			t.Fatalf("unsupported attachment accepted: %s", part["mime"])
		}
	}
}

func TestChatOutputParameterCompatibility(t *testing.T) {
	for endpoint, expected := range map[string]string{
		"https://api.openai.com/v1": "max_completion_tokens",
		"https://api.mistral.ai/v1": "max_tokens",
		"http://localhost:1234/v1":  "max_tokens",
	} {
		if got := nativeChatOutputParameter(tlProviderDefinition{BaseURL: endpoint}); got != expected {
			t.Errorf("%s: got %s, want %s", endpoint, got, expected)
		}
	}
}

func TestProcessManagerCloseStopsAndRejectsCommands(t *testing.T) {
	manager := newProcessManager(func() string { return t.TempDir() })
	command := "sleep 30"
	if runtime.GOOS == "windows" {
		command = "ping -n 31 127.0.0.1 > nul"
	}
	child, err := manager.start(command)
	if err != nil {
		t.Fatal(err)
	}
	manager.Close()
	select {
	case <-child.done:
	case <-time.After(5 * time.Second):
		t.Fatal("owned command survived shutdown")
	}
	if _, err := manager.start("echo should-not-start"); err == nil {
		t.Fatal("command started after shutdown")
	}
}

func TestNativeRunCannotRecreateRemovedSession(t *testing.T) {
	t.Setenv("TL_STUDIO_SQLITE_SESSIONS", "")
	store := newSessionPersistenceStore(t.TempDir(), "test")
	session, err := store.createNativeSession(t.TempDir(), sessionCreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.remove(session.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.recordAcceptedRun(session.ID, session.Directory, sessionRunInput{Text: "late"}); err == nil {
		t.Fatal("late run recreated session")
	}
}

func TestModelStreamTotalByteLimit(t *testing.T) {
	reader := &boundedModelStream{reader: strings.NewReader("more"), read: nativeModelMaxResponseBytes}
	if _, err := reader.Read(make([]byte, 4)); err == nil {
		t.Fatal("stream exceeded total-byte budget")
	}
}

func TestWorkspaceMutationRejectsNestedProtectedPath(t *testing.T) {
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{".git/config", "folder/../.git/config"} {
		if _, _, err := resolveProjectMutationPath(project, path); err == nil {
			t.Fatal("protected path accepted")
		}
	}
}

func FuzzNativeToolArgumentsNeverPanic(f *testing.F) {
	for _, seed := range []string{"{}", "null", "[]", `{"path":"x"}`, "{bad"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > 1<<20 {
			t.Skip()
		}
		_, _ = decodeNativeToolArguments(json.RawMessage(value))
	})
}

func TestNativeCloseCancelsOwnedRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &nativeAgentRuntime{runs: map[string]nativeRunHandle{"active": {cancel: cancel}}, closed: false}
	r.runWG.Add(1)
	go func() { <-ctx.Done(); r.finishRun("active") }()
	r.Close()
	if ctx.Err() == nil || len(r.NativeStatuses("")) != 0 {
		t.Fatal("run survived shutdown")
	}
}
