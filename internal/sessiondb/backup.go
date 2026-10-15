package sessiondb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const sessionApplicationID = 0x544c5353 // TLSS

type BackupReport struct {
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	Bytes       int64  `json:"bytes"`
	Sessions    int64  `json:"sessions"`
	Messages    int64  `json:"messages"`
	PendingRuns int64  `json:"pendingRuns"`
}

// readOnlyBackup never executes schema migrations or recovers pending runs.
// Backups are standalone snapshots, not copies of the live WAL main file.
func readOnlyBackup(ctx context.Context, path string) (*sql.DB, BackupReport, error) {
	report := BackupReport{Path: path}
	st, err := os.Lstat(path)
	if err != nil {
		return nil, report, err
	}
	if !st.Mode().IsRegular() || st.Size() == 0 {
		return nil, report, errors.New("backup must be a nonempty regular file")
	}
	for _, suffix := range []string{"-wal", "-journal"} {
		if side, e := os.Stat(path + suffix); e == nil && side.Size() > 0 {
			return nil, report, errors.New("backup has a live journal; use the session backup command instead of copying a running database")
		}
	}
	db, err := sql.Open("sqlite3", databaseURI(path)+"?mode=ro&immutable=1")
	if err != nil {
		return nil, report, err
	}
	db.SetMaxOpenConns(1)
	fail := func(err error) (*sql.DB, BackupReport, error) { db.Close(); return nil, report, err }
	var id int
	if err = db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&id); err != nil {
		return fail(err)
	}
	if id != sessionApplicationID {
		return fail(errors.New("backup is not a TL Studio session snapshot"))
	}
	var version int
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fail(err)
	}
	if version != 1 {
		return fail(errors.New("unsupported session backup format"))
	}
	var answer string
	if err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&answer); err != nil {
		return fail(err)
	}
	if answer != "ok" {
		return fail(fmt.Errorf("backup integrity check: %s", answer))
	}
	// Reject executable schema objects. Restoring a backup must never install
	// a trigger or view supplied by an untrusted database file.
	var objects int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE type IN ('trigger','view')").Scan(&objects); err != nil {
		return fail(err)
	}
	if objects != 0 {
		return fail(errors.New("backup contains unsupported executable schema objects"))
	}
	for _, query := range []string{
		"SELECT id,directory,metadata,revision,changed_at FROM sessions LIMIT 0",
		"SELECT session_id,message_id,sequence,payload FROM messages LIMIT 0",
		"SELECT session_id,payload FROM session_changes LIMIT 0",
		"SELECT session_id,payload FROM session_pending_run LIMIT 0",
		"SELECT operation_id,fingerprint,session_id,revision FROM commits LIMIT 0",
	} {
		rows, e := db.QueryContext(ctx, query)
		if e != nil {
			return fail(e)
		}
		rows.Close()
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fail(err)
	}
	violations := rows.Next()
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		return fail(rowErr)
	}
	if violations {
		return fail(errors.New("backup foreign-key check failed"))
	}
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions").Scan(&report.Sessions); err != nil {
		return fail(err)
	}
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM messages").Scan(&report.Messages); err != nil {
		return fail(err)
	}
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM session_pending_run").Scan(&report.PendingRuns); err != nil {
		return fail(err)
	}
	report.Bytes = st.Size()
	return db, report, nil
}

func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// publishNew uses a link to publish complete synced bytes without replacing an
// existing target, including an existing symlink. Failure leaves old data alone.
// Staging must be on the same local filesystem as the destination.
func publishNew(staged, target string) error {
	f, err := os.OpenFile(staged, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Link(staged, target); err != nil {
		return fmt.Errorf("publish new file without overwrite: %w", err)
	}
	return syncDirectory(filepath.Dir(target))
}

func stageFile(target string) (*os.File, error) {
	if _, err := os.Lstat(target); err == nil {
		return nil, fmt.Errorf("destination already exists: %s", target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(target + suffix); err == nil {
			return nil, errors.New("destination has existing SQLite sidecars; preserve them and restore into a new directory")
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return nil, err
	}
	return os.CreateTemp(filepath.Dir(target), ".tl-session-stage-*")
}

// Backup asks SQLite for a consistent snapshot, including committed WAL data.
// It never copies the live main file directly and never overwrites a backup.
func (s *Store) Backup(ctx context.Context, destination string) (BackupReport, error) {
	target, err := filepath.Abs(destination)
	if err != nil {
		return BackupReport{}, err
	}
	temp, err := stageFile(target)
	if err != nil {
		return BackupReport{}, err
	}
	name := temp.Name()
	temp.Close()
	defer os.Remove(name)
	if _, err = s.db.ExecContext(ctx, "VACUUM main INTO ?", databaseURI(name)); err != nil {
		return BackupReport{}, fmt.Errorf("create consistent session snapshot: %w", err)
	}
	db, report, err := readOnlyBackup(ctx, name)
	if err != nil {
		return report, err
	}
	db.Close()
	report.Path = target
	if report.SHA256, err = fileDigest(name); err != nil {
		return report, err
	}
	s.phase("backup.before_publish")
	if err = publishNew(name, target); err != nil {
		return BackupReport{}, err
	}
	s.phase("backup.after_publish")
	return report, nil
}

// RestoreBackup validates a standalone backup and publishes it only to a new
// destination. Callers must hold the application's state-directory lock and
// restore into an empty session directory; existing histories are never replaced.
func RestoreBackup(ctx context.Context, source, destination string) (BackupReport, error) {
	target, err := filepath.Abs(destination)
	if err != nil {
		return BackupReport{}, err
	}
	temp, err := stageFile(target)
	if err != nil {
		return BackupReport{}, err
	}
	name := temp.Name()
	defer os.Remove(name)
	if st, e := os.Lstat(source); e != nil || !st.Mode().IsRegular() || st.Size() == 0 {
		temp.Close()
		return BackupReport{}, errors.New("restore source must be a nonempty regular file")
	}
	in, err := os.Open(source)
	if err != nil {
		temp.Close()
		return BackupReport{}, err
	}
	if st, e := in.Stat(); e != nil || !st.Mode().IsRegular() || st.Size() == 0 {
		in.Close()
		temp.Close()
		return BackupReport{}, errors.New("restore source must be a nonempty regular file")
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(temp, h), contextReader{ctx: ctx, reader: in})
	in.Close()
	if err != nil {
		temp.Close()
		return BackupReport{}, err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return BackupReport{}, err
	}
	if err = temp.Close(); err != nil {
		return BackupReport{}, err
	}
	// Validate the private copy so a changing source cannot race validation
	// and publication. Also refuse live journal sources before copying.
	for _, suffix := range []string{"-wal", "-journal"} {
		if st, e := os.Stat(source + suffix); e == nil && st.Size() > 0 {
			return BackupReport{}, errors.New("source has a live journal; restore only a generated standalone backup")
		}
	}
	db, report, err := readOnlyBackup(ctx, name)
	if err != nil {
		return report, err
	}
	db.Close()
	report.Path = target
	report.SHA256 = hex.EncodeToString(h.Sum(nil))
	if err = publishNew(name, target); err != nil {
		return BackupReport{}, err
	}
	return report, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

type HistoryCounts struct {
	Sessions    int64 `json:"sessions"`
	Messages    int64 `json:"messages"`
	PendingRuns int64 `json:"pendingRuns"`
}

func (s *Store) Counts(ctx context.Context) (HistoryCounts, error) {
	var c HistoryCounts
	err := s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM sessions),(SELECT COUNT(*) FROM messages),(SELECT COUNT(*) FROM session_pending_run)`).Scan(&c.Sessions, &c.Messages, &c.PendingRuns)
	return c, err
}
