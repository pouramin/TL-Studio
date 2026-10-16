package sessiondb

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrationRefusesDivergenceOrUnreferencedArchives(t *testing.T) {
	for _, failure := range []string{"metadata", "orphan-archive", "backup-failure"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			snapshot, _ := writeLegacy(t, root, "s1", false)
			switch failure {
			case "metadata":
				var snap map[string]any
				raw, _ := os.ReadFile(snapshot)
				json.Unmarshal(raw, &snap)
				snap["session"].(map[string]any)["title"] = "uncommitted title"
				raw, _ = json.Marshal(snap)
				os.WriteFile(snapshot, raw, 0600)
			case "orphan-archive":
				os.WriteFile(filepath.Join(root, legacySessionFilename("s1")+"-history-000001.json"), []byte(`{"version":1,"sessionID":"s1","messages":[]}`), 0600)
			case "backup-failure":
				os.WriteFile(filepath.Join(root, "legacy-backups"), []byte("not a directory"), 0600)
			}
			before, _ := os.ReadFile(snapshot)
			db, _ := openTestStore(t)
			if _, err := db.ImportLegacy(context.Background(), root); err == nil {
				t.Fatal("unsafe migration succeeded")
			}
			rows, err := db.List(context.Background(), 0)
			if err != nil || len(rows) != 0 {
				t.Fatal("failed migration exposed data", err)
			}
			if after, _ := os.ReadFile(snapshot); !bytes.Equal(before, after) {
				t.Fatal("failed migration modified legacy history")
			}
		})
	}
}

func TestMigrationReportAndSourceBackupSurviveReopen(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeLegacy(t, root, "s1", true)
	db, path := openTestStore(t)
	first, err := db.ImportLegacy(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Sources) != 3 {
		t.Fatalf("missing source manifest: %+v", first)
	}
	files := map[string][]byte{}
	for _, source := range first.Sources {
		data, err := os.ReadFile(filepath.Join(root, source.Name))
		if err != nil {
			t.Fatal(err)
		}
		files[source.Name] = data
	}
	if err = verifyLegacyZIP(first.BackupPath, files); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	report, err := db.ImportLegacy(ctx, root)
	if err != nil || !report.AlreadyMigrated || report.Sessions != 1 || report.Messages != 3 || report.BackupPath != first.BackupPath {
		t.Fatalf("migration report lost: %+v %v", report, err)
	}
	stored, err := db.MigrationReport(ctx)
	if err != nil || stored.SourceDigest != first.SourceDigest {
		t.Fatal("durable report mismatch", err)
	}
}

func TestOlderCompletedMigrationCanReopenRepeatedly(t *testing.T) {
	db, path := openTestStore(t)
	ctx := context.Background()
	root := t.TempDir()
	writeLegacy(t, root, "s1", false)
	if _, err := db.ImportLegacy(ctx, root); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("DROP TABLE migration_reports"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("PRAGMA application_id=0"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("PRAGMA user_version=0"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for i := 0; i < 3; i++ {
		fresh, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		report, err := fresh.MigrationReport(ctx)
		if err != nil || !report.ReportUnavailable {
			t.Fatal("old report was invented", err)
		}
		report, err = fresh.ImportLegacy(ctx, root)
		if err != nil || !report.AlreadyMigrated || !report.ReportUnavailable {
			t.Fatalf("older migration failed on restart %d: %+v %v", i, report, err)
		}
		counts, err := fresh.Counts(ctx)
		fresh.Close()
		if err != nil || counts.Sessions != 1 || counts.Messages != 1 {
			t.Fatal("old migration history lost", err)
		}
	}
}
