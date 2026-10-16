package sessiondb

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestBackupRestoreIncludesWALAttachmentsAndIdempotency(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := Open(filepath.Join(root, "state # 100%.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.ImportLegacy(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.Exec("PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	m := Mutation{OperationID: "accepted", SessionID: "s", Directory: "/project", Metadata: []byte(`{"title":"preserve"}`), Messages: []Message{{ID: "upload", Payload: []byte(`{"attachments":[{"name":"a.txt","text":"PRIVATE_MARKER"}]}`)}}, ChangeData: []byte(`[{"file":"a.txt"}]`), PendingRun: []byte(`{"messageID":"upload","ownerPID":-1}`), UpdatePendingRun: true}
	if _, err = db.Apply(ctx, m); err != nil {
		t.Fatal(err)
	}
	wal, err := os.Stat(filepath.Join(root, "state # 100%.sqlite-wal"))
	if err != nil || wal.Size() == 0 {
		t.Fatal("fixture did not retain committed WAL bytes", err)
	}
	backup := filepath.Join(root, "backup # 100%.sqlite")
	report, err := db.Backup(ctx, backup)
	if err != nil {
		t.Fatal(err)
	}
	if report.Sessions != 1 || report.Messages != 1 || report.PendingRuns != 1 || len(report.SHA256) != 64 {
		t.Fatalf("bad backup report: %+v", report)
	}
	original, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Backup(ctx, backup); err == nil {
		t.Fatal("backup overwrote an existing destination")
	}
	if now, _ := os.ReadFile(backup); !bytes.Equal(now, original) {
		t.Fatal("old backup changed")
	}
	target := filepath.Join(t.TempDir(), "restored.sqlite")
	restored, err := RestoreBackup(ctx, backup, target)
	if err != nil {
		t.Fatal(err)
	}
	if restored.SHA256 != report.SHA256 {
		t.Fatal("restored bytes differ from validated backup")
	}
	fresh, err := Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	a, b, c, d, found, err := fresh.ReadSnapshot(ctx, "s")
	if err != nil || !found || a.Revision != 1 || !reflect.DeepEqual(b, m.Messages) || !bytes.Equal(c, m.ChangeData) || !bytes.Equal(d, m.PendingRun) {
		t.Fatalf("restore lost logical state: %+v %+v %s %s %v", a, b, c, d, err)
	}
	commit, err := fresh.Apply(ctx, m)
	if err != nil || !commit.PreviouslyCommitted {
		t.Fatalf("restore lost operation journal: %+v %v", commit, err)
	}
	if _, err = RestoreBackup(ctx, backup, target); err == nil {
		t.Fatal("restore replaced existing history")
	}
}

func TestBackupRejectsCorruptionSchemaAndCancellationWithoutPublishing(t *testing.T) {
	db, _ := openTestStore(t)
	ctx := context.Background()
	bad := filepath.Join(t.TempDir(), "broken.sqlite")
	bytesBefore := []byte("damaged original")
	if err := os.WriteFile(bad, bytesBefore, 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "restore.sqlite")
	if _, err := RestoreBackup(ctx, bad, target); err == nil {
		t.Fatal("corrupt backup restored")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("failed restore published a destination")
	}
	if got, _ := os.ReadFile(bad); !bytes.Equal(got, bytesBefore) {
		t.Fatal("failed restore modified source")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := db.Backup(cancelled, target); err == nil {
		t.Fatal("cancelled backup succeeded")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("cancelled backup published output")
	}
	if _, err := db.db.Exec(`CREATE TRIGGER forbidden AFTER INSERT ON sessions BEGIN DELETE FROM messages; END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Backup(ctx, target); err == nil {
		t.Fatal("backup with executable schema accepted")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("unsupported backup published")
	}
}

func TestBackupsDuringIndependentWritesAreConsistent(t *testing.T) {
	db, path := openTestStore(t)
	writer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	ctx := context.Background()
	var wg sync.WaitGroup
	errorsCh := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; i <= 30; i++ {
			_, e := writer.Apply(ctx, Mutation{OperationID: fmt.Sprint(i), SessionID: "s", Directory: "/p", ExpectedRevision: int64(i - 1), Metadata: []byte(fmt.Sprint(i)), Messages: []Message{{ID: fmt.Sprint(i), Payload: []byte(fmt.Sprint(i))}}})
			if e != nil {
				errorsCh <- e
				return
			}
		}
	}()
	for i := 0; i < 4; i++ {
		path := filepath.Join(t.TempDir(), "copy.sqlite")
		if _, err := db.Backup(ctx, path); err != nil {
			t.Fatal(err)
		}
		copy, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		session, msgs, _, _, found, err := copy.ReadSnapshot(ctx, "s")
		copy.Close()
		if err != nil {
			t.Fatal(err)
		}
		if found && (int64(len(msgs)) != session.Revision || string(session.Metadata) != fmt.Sprint(session.Revision)) {
			t.Fatalf("mixed backup generations: revision=%d rows=%d metadata=%s", session.Revision, len(msgs), session.Metadata)
		}
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Fatal(err)
	}
}
