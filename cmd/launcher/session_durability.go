package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// Bound the current mutable session snapshot without discarding old turns.
	sessionArchiveBatchMessages = 200
	sessionArchiveTargetBytes   = 8 << 20
)

type persistedSessionPendingRun struct {
	MessageID string `json:"messageID,omitempty"`
	StartedAt int64  `json:"startedAt"`
}

type persistedSessionArchive struct {
	Version   int                  `json:"version"`
	SessionID string               `json:"sessionID"`
	Messages  []sessionMessageView `json:"messages"`
}

func interruptedNativeRunMessage(sessionID string, startedAt int64) sessionMessageView {
	now := time.Now().UnixMilli()
	return sessionMessageView{
		ID:          fmt.Sprintf("tlsm_recovered_%d", startedAt),
		SessionID:   sessionID,
		Role:        "assistant",
		Agent:       "code",
		CreatedAt:   now,
		CompletedAt: now,
		Error: &sessionErrorView{
			Type:    "interrupted",
			Message: "TL Studio stopped before the previous Agent run finished. Some tool operations may have completed. Inspect the project and recorded tool activity before asking the Agent to continue; no tools were replayed automatically.",
		},
		Activities:  []sessionActivityView{},
		Attachments: []sessionAttachmentView{},
		Changes:     []sessionChangeView{},
	}
}

func (s *sessionPersistenceStore) archivePath(sessionID string, index int) string {
	filename := strings.TrimSuffix(persistedSessionFileName(sessionID), ".json")
	return filepath.Join(s.root, fmt.Sprintf("%s-history-%06d.json", filename, index))
}

func (s *sessionPersistenceStore) archiveExcessSnapshotMessagesLocked(snapshot *persistedSessionSnapshot) error {
	for {
		data, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		if len(snapshot.Messages) <= maxPersistedMessages && len(data) <= maxSessionSnapshotBytes {
			return nil
		}
		if len(snapshot.Messages) <= 1 {
			return fmt.Errorf("session snapshot exceeds %d bytes and cannot be archived without losing its last message", maxSessionSnapshotBytes)
		}
		count := len(snapshot.Messages) / 4
		if count > sessionArchiveBatchMessages {
			count = sessionArchiveBatchMessages
		}
		if count < 1 {
			count = 1
		}
		// Bound an archive batch too; if one message alone exceeds the budget,
		// return an explicit error rather than silently truncating it.
		for count > 1 {
			encoded, err := json.Marshal(snapshot.Messages[:count])
			if err != nil {
				return err
			}
			if len(encoded) <= sessionArchiveTargetBytes {
				break
			}
			count /= 2
		}
		archived := persistedSessionArchive{
			Version:   sessionPersistenceVersion,
			SessionID: snapshot.Session.ID,
			Messages:  append([]sessionMessageView(nil), snapshot.Messages[:count]...),
		}
		encoded, err := json.Marshal(archived)
		if err != nil {
			return err
		}
		if len(encoded) > maxSessionSnapshotBytes {
			return fmt.Errorf("session archive batch exceeds %d bytes; refusing to discard history", maxSessionSnapshotBytes)
		}
		next := snapshot.ArchivedBatches + 1
		if err := writePrivateJSONAtomic(s.archivePath(snapshot.Session.ID, next), archived); err != nil {
			return fmt.Errorf("write session history archive %d: %w", next, err)
		}
		// The snapshot is committed only AFTER its immutable archive is safe.
		// An interrupted save can leave an unreferenced file, never lost data.
		snapshot.ArchivedBatches = next
		snapshot.Messages = snapshot.Messages[count:]
	}
}

func (s *sessionPersistenceStore) readArchivedSnapshotMessagesLocked(snapshot persistedSessionSnapshot) ([]sessionMessageView, error) {
	result := []sessionMessageView{}
	for i := 1; i <= snapshot.ArchivedBatches; i++ {
		data, err := os.ReadFile(s.archivePath(snapshot.Session.ID, i))
		if err != nil {
			return nil, fmt.Errorf("read session history archive %d: %w", i, err)
		}
		var archived persistedSessionArchive
		if err := json.Unmarshal(data, &archived); err != nil {
			return nil, fmt.Errorf("decode session history archive %d: %w", i, err)
		}
		if archived.Version != sessionPersistenceVersion || archived.SessionID != snapshot.Session.ID {
			return nil, fmt.Errorf("session history archive %d does not match its session", i)
		}
		result = append(result, archived.Messages...)
	}
	return result, nil
}

func (s *sessionPersistenceStore) removeSessionArchives(sessionID string) error {
	pattern := strings.TrimSuffix(s.snapshotPath(sessionID), ".json") + "-history-*.json"
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
