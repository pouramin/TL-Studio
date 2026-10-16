package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionMaintenanceBackupRestoreAndDowngradeGuard(t *testing.T) {
	t.Setenv("TL_STUDIO_SQLITE_SESSIONS", "1")
	state, project := t.TempDir(), t.TempDir()
	t.Setenv("TL_STUDIO_STATE_DIR", state)
	store := newSessionPersistenceStore(sessionPersistenceRoot(), "test")
	session, err := store.createNativeSession(project, sessionCreateInput{Title: "restore proof"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.putNativeFinalMessage(session.ID, project, sessionMessageView{ID: "saved", Role: "assistant", Text: "important", Attachments: []sessionAttachmentView{{Name: "a.txt", MIME: "text/plain", Text: "ATTACHMENT-SAVED"}}}); err != nil {
		t.Fatal(err)
	}
	store.Close()
	backup := filepath.Join(t.TempDir(), "backup.sqlite")
	if err = runSessionMaintenance(backup, "", false); err != nil {
		t.Fatal(err)
	}
	if err = runSessionMaintenance(backup, "", false); err == nil {
		t.Fatal("maintenance overwrote backup")
	}
	if err = runSessionMaintenance("", backup, false); err == nil {
		t.Fatal("restore replaced a nonempty session directory")
	}
	t.Setenv("TL_STUDIO_SQLITE_SESSIONS", "0")
	if _, err = newSessionPersistenceStore(sessionPersistenceRoot(), "test").list(0); err == nil || !strings.Contains(err.Error(), "stale JSON") {
		t.Fatalf("downgrade wrote a stale authority: %v", err)
	}
	t.Setenv("TL_STUDIO_SQLITE_SESSIONS", "1")
	t.Setenv("TL_STUDIO_STATE_DIR", t.TempDir())
	if err = runSessionMaintenance("", backup, false); err != nil {
		t.Fatal(err)
	}
	restored := newSessionPersistenceStore(sessionPersistenceRoot(), "test")
	defer restored.Close()
	msgs, ok, err := restored.getMessages(session.ID)
	if err != nil || !ok || len(msgs) != 1 || msgs[0].Attachments[0].Text != "ATTACHMENT-SAVED" {
		t.Fatalf("restore lost native history: %+v %v", msgs, err)
	}
	if err = restored.sqlite.IntegrityCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = runSessionMaintenance("", "", true); err != nil {
		t.Fatal(err)
	}
}

func TestMaintenanceRefusesLiveOwnerAndMissingHistory(t *testing.T) {
	t.Setenv("TL_STUDIO_SQLITE_SESSIONS", "1")
	t.Setenv("TL_STUDIO_STATE_DIR", t.TempDir())
	lock, err := acquireStateDirectoryLock(tlStudioStateDirectory())
	if err != nil {
		t.Fatal(err)
	}
	if err = runSessionMaintenance(filepath.Join(t.TempDir(), "b.sqlite"), "", false); err == nil {
		t.Fatal("maintenance ignored active owner")
	}
	lock.Close()
	if err = runSessionMaintenance(filepath.Join(t.TempDir(), "b.sqlite"), "", false); err == nil {
		t.Fatal("missing history became empty backup")
	}
	if _, err = os.Stat(filepath.Join(sessionPersistenceRoot(), "sessions.sqlite")); !os.IsNotExist(err) {
		t.Fatal("failed backup created empty history")
	}
}

func TestImportedDuplicateAndAnonymousMessagesRemainEditable(t *testing.T) {
	t.Setenv("TL_STUDIO_SQLITE_SESSIONS", "")
	root, project := t.TempDir(), t.TempDir()
	legacy := newSessionPersistenceStore(root, "test")
	session, err := legacy.createNativeSession(project, sessionCreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := persistedSessionSnapshot{Version: 1, Session: session, Messages: []sessionMessageView{{ID: "same", Role: "user", Text: "first"}, {ID: "same", Role: "assistant", Text: "second"}, {Role: "assistant", Text: "anonymous"}}}
	data, _ := json.Marshal(snapshot)
	if err = os.WriteFile(legacy.snapshotPath(session.ID), data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TL_STUDIO_SQLITE_SESSIONS", "1")
	db := newSessionPersistenceStore(root, "test")
	defer db.Close()
	msgs, ok, err := db.getMessages(session.ID)
	if err != nil || !ok || len(msgs) != 3 {
		t.Fatal("legacy IDs lost messages", err)
	}
	if msgs[0].ID == msgs[1].ID || msgs[2].ID == "" {
		t.Fatal("imported messages do not expose stable distinct IDs")
	}
	if err = db.putNativeFinalMessage(session.ID, project, sessionMessageView{ID: "new", Role: "assistant", Text: "continued"}); err != nil {
		t.Fatal("could not continue migrated session", err)
	}
	msgs, _, err = db.getMessages(session.ID)
	if err != nil || len(msgs) != 4 || msgs[1].Text != "second" || msgs[2].Text != "anonymous" {
		t.Fatal("continued session lost migrated content", err)
	}
}
