package sessiondb

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

type MigrationSource struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

func equalJSON(a, b []byte) (bool, error) {
	normalize := func(raw []byte) ([]byte, error) {
		var v any
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		if err := d.Decode(&v); err != nil {
			return nil, err
		}
		return json.Marshal(v)
	}
	x, err := normalize(a)
	if err != nil {
		return false, err
	}
	y, err := normalize(b)
	return bytes.Equal(x, y), err
}

func describeMigrationSources(files map[string][]byte) []MigrationSource {
	result := make([]MigrationSource, 0, len(files))
	for name, data := range files {
		sum := sha256.Sum256(data)
		result = append(result, MigrationSource{Name: name, SHA256: hex.EncodeToString(sum[:]), Bytes: len(data)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func verifyMigrationSources(root string, files map[string][]byte) error {
	if _, indexed := files["index.json"]; !indexed {
		if _, err := os.Lstat(filepath.Join(root, "index.json")); !errors.Is(err, os.ErrNotExist) {
			return errors.New("legacy index appeared during empty migration")
		}
	}
	paths, err := filepath.Glob(filepath.Join(root, "session-*.json"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		if _, known := files[filepath.Base(path)]; !known {
			return errors.New("legacy file inventory changed during migration")
		}
	}
	for name, data := range files {
		now, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return err
		}
		if !bytes.Equal(data, now) {
			return fmt.Errorf("legacy source changed during migration: %s", name)
		}
	}
	return nil
}

func verifyLegacyZIP(path string, files map[string][]byte) error {
	z, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer z.Close()
	if len(z.File) != len(files) {
		return errors.New("legacy backup file count mismatch")
	}
	seen := map[string]bool{}
	for _, entry := range z.File {
		data, found := files[entry.Name]
		if !found || seen[entry.Name] {
			return errors.New("unexpected legacy backup entry")
		}
		seen[entry.Name] = true
		reader, e := entry.Open()
		if e != nil {
			return e
		}
		got, e := io.ReadAll(io.LimitReader(reader, int64(len(data))+1))
		reader.Close()
		if e != nil {
			return e
		}
		if !bytes.Equal(data, got) {
			return errors.New("legacy backup contents differ from source")
		}
	}
	return nil
}

// Source backups precede the authority commit. A crash may leave an unused
// verified ZIP, but cannot expose a completed migration without its backup.
func backupLegacySource(root, digest string, files map[string][]byte) (string, error) {
	target := filepath.Join(root, "legacy-backups", "legacy-"+digest+".zip")
	if _, err := os.Lstat(target); err == nil {
		st, e := os.Lstat(target)
		if e != nil || !st.Mode().IsRegular() {
			return "", errors.New("legacy backup destination must be a regular file")
		}
		return target, verifyLegacyZIP(target, files)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	temp, err := stageFile(target)
	if err != nil {
		return "", err
	}
	name := temp.Name()
	defer os.Remove(name)
	w := zip.NewWriter(temp)
	for _, source := range describeMigrationSources(files) {
		out, e := w.Create(source.Name)
		if e != nil {
			w.Close()
			temp.Close()
			return "", e
		}
		if _, e = out.Write(files[source.Name]); e != nil {
			w.Close()
			temp.Close()
			return "", e
		}
	}
	if err = w.Close(); err != nil {
		temp.Close()
		return "", err
	}
	if err = temp.Close(); err != nil {
		return "", err
	}
	if err = verifyLegacyZIP(name, files); err != nil {
		return "", err
	}
	if err = publishNew(name, target); err != nil {
		if errors.Is(err, os.ErrExist) {
			return target, verifyLegacyZIP(target, files)
		}
		return "", err
	}
	return target, nil
}

func verifyImportedSession(ctx context.Context, tx *sql.Tx, session importedSession) error {
	var metadata []byte
	var directory string
	if err := tx.QueryRowContext(ctx, "SELECT directory,metadata FROM sessions WHERE id=?", session.key.ID).Scan(&directory, &metadata); err != nil {
		return err
	}
	if directory != session.key.Directory || !bytes.Equal(metadata, session.metadata) {
		return errors.New("imported session metadata verification failed")
	}
	rows, err := tx.QueryContext(ctx, "SELECT message_id,sequence,payload FROM messages WHERE session_id=? ORDER BY sequence", session.key.ID)
	if err != nil {
		return err
	}
	i := 0
	for rows.Next() {
		var id string
		var sequence int
		var payload []byte
		if err = rows.Scan(&id, &sequence, &payload); err != nil {
			rows.Close()
			return err
		}
		if i >= len(session.messages) || id != session.messages[i].ID || sequence != i+1 || !bytes.Equal(payload, session.messages[i].Payload) {
			rows.Close()
			return errors.New("imported message payload/order verification failed")
		}
		i++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if i != len(session.messages) {
		return errors.New("imported message count verification failed")
	}
	for table, want := range map[string][]byte{"session_changes": session.changes, "session_pending_run": session.pending} {
		var got []byte
		e := tx.QueryRowContext(ctx, "SELECT payload FROM "+table+" WHERE session_id=?", session.key.ID).Scan(&got)
		if errors.Is(e, sql.ErrNoRows) && len(want) == 0 {
			continue
		}
		if e != nil {
			return e
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("imported %s verification failed", table)
		}
	}
	return nil
}

func (s *Store) MigrationReport(ctx context.Context) (MigrationReport, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name='migration_reports'").Scan(&exists); err != nil {
		return MigrationReport{}, err
	}
	if exists == 0 {
		var digest string
		err := s.db.QueryRowContext(ctx, "SELECT source_digest FROM migration_state WHERE name='legacy-v1'").Scan(&digest)
		if err != nil {
			return MigrationReport{}, err
		}
		return MigrationReport{AlreadyMigrated: true, ReportUnavailable: true, SourceDigest: digest}, nil
	}
	var payload []byte
	err := s.db.QueryRowContext(ctx, "SELECT payload FROM migration_reports WHERE name='legacy-v1'").Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return MigrationReport{}, errors.New("no completed legacy migration report")
	}
	if err != nil {
		return MigrationReport{}, err
	}
	var report MigrationReport
	err = json.Unmarshal(payload, &report)
	return report, err
}
