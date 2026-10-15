package sessiondb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestTenThousandMessageMigrationBackupAndContinuation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	snapshot, _ := writeLegacy(t, root, "long", false)
	raw, _ := os.ReadFile(snapshot)
	var content map[string]any
	if err := json.Unmarshal(raw, &content); err != nil {
		t.Fatal(err)
	}
	messages := make([]json.RawMessage, 10000)
	for i := range messages {
		messages[i], _ = json.Marshal(map[string]any{"id": fmt.Sprintf("m-%05d", i), "role": "user", "text": fmt.Sprint(i), "attachments": []map[string]string{{"name": "source.txt", "text": fmt.Sprintf("ATTACHMENT-%05d", i)}}})
	}
	content["archivedBatches"] = 49
	for batch := 0; batch < 49; batch++ {
		data, _ := json.Marshal(map[string]any{"version": 1, "sessionID": "long", "messages": messages[batch*200 : (batch+1)*200]})
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%s-history-%06d.json", legacySessionFilename("long"), batch+1)), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	content["messages"] = messages[9800:]
	raw, _ = json.Marshal(content)
	if err := os.WriteFile(snapshot, raw, 0600); err != nil {
		t.Fatal(err)
	}
	db, _ := openTestStore(t)
	report, err := db.ImportLegacy(ctx, root)
	if err != nil || report.Messages != 10000 || len(report.Sources) != 51 {
		t.Fatalf("long migration failed: %+v %v", report, err)
	}
	rows, err := db.Messages(ctx, "long")
	if err != nil || len(rows) != 10000 {
		t.Fatal("history length changed", err)
	}
	for i, row := range rows {
		if !bytes.Equal(row.Payload, messages[i]) {
			t.Fatalf("migration changed payload/order at %d", i)
		}
	}
	for i := 0; i < 100; i++ {
		_, err = db.Apply(ctx, Mutation{OperationID: fmt.Sprintf("continuation-%d", i), SessionID: "long", Directory: "/project", Metadata: []byte("{}"), ExpectedRevision: int64(i + 1), Messages: []Message{{ID: fmt.Sprintf("new-%03d", i), Payload: []byte("continued")}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	tail, err := db.RecentMessages(ctx, "long", 200)
	if err != nil || len(tail) != 200 || tail[0].ID != "m-09900" || tail[199].ID != "new-099" {
		t.Fatal("tail read omitted/reordered history", err)
	}
	backup := filepath.Join(t.TempDir(), "long.sqlite")
	saved, err := db.Backup(ctx, backup)
	if err != nil || saved.Messages != 10100 {
		t.Fatal("long backup lost history", err)
	}
	target := filepath.Join(t.TempDir(), "restored.sqlite")
	if _, err = RestoreBackup(ctx, backup, target); err != nil {
		t.Fatal(err)
	}
	fresh, err := Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	rows, err = fresh.Messages(ctx, "long")
	if err != nil || len(rows) != 10100 || !bytes.Equal(rows[0].Payload, messages[0]) || rows[10099].ID != "new-099" {
		t.Fatal("long restore lost oldest/newest data", err)
	}
}
