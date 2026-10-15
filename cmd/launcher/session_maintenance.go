package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pouramin/TL-Studio/internal/sessiondb"
)

func runSessionMaintenance(backup, restore string, report bool) error {
	count := 0
	for _, selected := range []bool{backup != "", restore != "", report} {
		if selected {
			count++
		}
	}
	if count != 1 {
		return errors.New("select exactly one of --session-backup, --session-restore or --session-report")
	}
	if strings.TrimSpace(os.Getenv("TL_STUDIO_SQLITE_SESSIONS")) != "1" {
		return errors.New("session maintenance requires the opt-in SQLite backend: TL_STUDIO_SQLITE_SESSIONS=1")
	}
	lock, err := acquireStateDirectoryLock(tlStudioStateDirectory())
	if err != nil {
		return err
	}
	defer lock.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	root := sessionPersistenceRoot()
	path := filepath.Join(root, "sessions.sqlite")
	if restore != "" {
		entries, err := os.ReadDir(root)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if len(entries) > 0 {
			return errors.New("restore requires an empty session directory; use a new TL_STUDIO_STATE_DIR and preserve the existing history")
		}
		result, err := sessiondb.RestoreBackup(ctx, restore, path)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	if st, err := os.Lstat(path); err != nil {
		return err
	} else if !st.Mode().IsRegular() {
		return errors.New("session database must be a regular file")
	}
	db, err := sessiondb.Open(path)
	if err != nil {
		return err
	}
	defer db.Close()
	if backup != "" {
		result, err := db.Backup(ctx, backup)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	if err = db.IntegrityCheck(ctx); err != nil {
		return err
	}
	counts, err := db.Counts(ctx)
	if err != nil {
		return err
	}
	migration, err := db.MigrationReport(ctx)
	if err != nil {
		return fmt.Errorf("read migration report: %w", err)
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"integrity": "ok", "current": counts, "migration": migration})
}
