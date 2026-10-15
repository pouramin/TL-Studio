package sessiondb

import (
	"context"
	"os"
	"testing"
)

func TestDurabilityPragmasSurviveConnectionReplacement(t *testing.T) {
	db, _ := openTestStore(t)
	// Force each query to use a new connection, not merely the one configured
	// at Open. Connection-scoped durability/FK settings must remain active.
	db.db.SetMaxIdleConns(0)
	for pragma, want := range map[string]int{"synchronous": 2, "foreign_keys": 1, "busy_timeout": 10000} {
		var got int
		if err := db.db.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil || got != want {
			t.Fatalf("%s=%d want %d: %v", pragma, got, want, err)
		}
	}
	var mode string
	if err := db.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode=%q %v", mode, err)
	}
}

func TestNewerDatabaseFormatFailsClosed(t *testing.T) {
	db, path := openTestStore(t)
	if _, err := db.db.Exec("PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if newer, err := Open(path); err == nil {
		newer.Close()
		t.Fatal("newer schema version was silently downgraded")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("future database bytes changed")
	}
}

func TestBackupRejectsExistingTargetSidecars(t *testing.T) {
	db, path := openTestStore(t)
	target := path + "-backup"
	if err := os.WriteFile(target+"-wal", []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Backup(context.Background(), target); err == nil {
		t.Fatal("backup published beside stale WAL")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("unsafe target published")
	}
}
