package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

 "github.com/pouramin/TL-Studio/internal/sessiondb"
)

const (
	sessionPersistenceVersion = 1
	// Listing limit only; stored session history is never automatically pruned.
	maxPersistedSessions = 300
	maxPersistedMessages       = 1200
	maxSessionSnapshotBytes    = 32 << 20
)

type persistedSessionIndex struct {
	Version  int           `json:"version"`
	Sessions []sessionView `json:"sessions"`
}

type persistedSessionSnapshot struct {
	Version int                  `json:"version"`
	Runtime string               `json:"runtime,omitempty"`
	Session sessionView          `json:"session"`
	Messages []sessionMessageView `json:"messages,omitempty"`
	Changes []sessionChangeView  `json:"changes,omitempty"`
	ArchivedBatches int `json:"archivedBatches,omitempty"`
	PendingRun *persistedSessionPendingRun `json:"pendingRun,omitempty"`
	SavedAt int64                `json:"savedAt"`
}

type sessionPersistenceStore struct {
	mu       sync.Mutex
	root     string
	runtime  string
	loaded   bool
	sessions map[string]sessionView
 sqlite *sessiondb.Store
 sqliteMode bool
}

func sessionPersistenceRoot() string {
	return filepath.Join(tlStudioStateDirectory(), "sessions")
}

func newSessionPersistenceStore(root, runtimeID string) *sessionPersistenceStore {
	return &sessionPersistenceStore{
		root:     root,
		runtime:  strings.TrimSpace(runtimeID),
		sessions: map[string]sessionView{},
  sqliteMode: strings.TrimSpace(os.Getenv("TL_STUDIO_SQLITE_SESSIONS"))=="1",
	}
}

func (s *sessionPersistenceStore) indexPath() string {
	return filepath.Join(s.root, "index.json")
}

func persistedSessionFileName(sessionID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sessionID)))
	return "session-" + hex.EncodeToString(sum[:]) + ".json"
}

func (s *sessionPersistenceStore) snapshotPath(sessionID string) string {
	return filepath.Join(s.root, persistedSessionFileName(sessionID))
}

// privateJSONAtomicIO exists so failure-injection tests can verify the exact
// durable-write ordering without replacing OS functions globally.
type privateJSONAtomicIO struct {
	syncFile func(*os.File) error
	rename   func(string, string) error
}

func writePrivateJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateBytesAtomic(path, data)
}

func writePrivateBytesAtomic(path string, data []byte) error {
	return writePrivateBytesAtomicWithIO(path, data, privateJSONAtomicIO{
		syncFile: (*os.File).Sync,
		rename:   os.Rename,
	})
}

// Test seam for the same durable primitive used for JSON and encrypted bytes.
func writePrivateJSONAtomicWithIO(path string, value any, ops privateJSONAtomicIO) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateBytesAtomicWithIO(path, data, ops)
}

func writePrivateBytesAtomicWithIO(path string, data []byte, ops privateJSONAtomicIO) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".tl-studio-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	// Persist bytes before replacing the only live copy.
	if err := ops.syncFile(temp); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync staged private state: %w", err)
	}
	if err := temp.Close(); err != nil {
		return err
	}
	// No in-place-write fallback. Directory durability depends on the OS
	// and filesystem and must be validated separately.
	if err := ops.rename(tempName, path); err != nil {
		return fmt.Errorf("atomically replace private state: %w", err)
	}
	return nil
}

func (s *sessionPersistenceStore) loadLocked() error {
 if s.sqliteMode {return s.sqliteLoadLocked()}
	if s.loaded {
		return nil
	}
	sessions := map[string]sessionView{}
	data, err := os.ReadFile(s.indexPath())
	if errors.Is(err, os.ErrNotExist) {
		s.sessions = sessions
		s.loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	var index persistedSessionIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return err
	}
	if index.Version != 0 && index.Version != sessionPersistenceVersion {
		return errors.New("unsupported TL Studio session persistence version")
	}
	for _, session := range index.Sessions {
		session.ID = strings.TrimSpace(session.ID)
		session.Directory = strings.TrimSpace(session.Directory)
		if session.ID == "" || session.Directory == "" {
			continue
		}
		sessions[session.ID] = session
	}
	// This executes once per process/store instance. A previous process cannot
	// have left a live native run here; mark it interrupted but never replay
	// proposed or possibly completed tool calls.
	for id := range sessions {
		snapshot, err := s.loadSnapshotLocked(id)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			// Preserve damaged snapshots for diagnosis; their direct read will
			// report the error, without blocking unrelated sessions.
			continue
		}
		if snapshot.PendingRun == nil {
			continue
		}
		started := snapshot.PendingRun.StartedAt
		snapshot.PendingRun = nil
		// Persisted "running" tool activities belong to the dead process.
		// The tool might have completed before its reply was recorded.
		for messageIndex := range snapshot.Messages {
			for activityIndex := range snapshot.Messages[messageIndex].Activities {
				activity := &snapshot.Messages[messageIndex].Activities[activityIndex]
				if activity.Kind == "tool" && activity.Status == "running" {
					activity.Status = "failed"
					activity.Error = &sessionErrorView{
						Type: "interrupted",
						Message: "TL Studio stopped before the tool outcome was recorded. The operation may have completed; inspect the workspace before retrying.",
					}
				}
			}
		}
		snapshot.Messages = append(snapshot.Messages, interruptedNativeRunMessage(id, started))
		if err := s.saveSnapshotLocked(snapshot); err != nil {
			return fmt.Errorf("recover interrupted session %s: %w", id, err)
		}
	}
	s.sessions = sessions
	s.loaded = true
	return nil
}

func (s *sessionPersistenceStore) persistIndexLocked() error {
	return s.persistIndexForLocked(s.sessions)
}

func (s *sessionPersistenceStore) persistIndexForLocked(sessions map[string]sessionView) error {
	items := make([]sessionView, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, session)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt == items[j].UpdatedAt {
			return items[i].ID < items[j].ID
		}
		return items[i].UpdatedAt > items[j].UpdatedAt
	})
	// Never delete historical sessions to satisfy a display/listing limit.
	// Explicit session deletion is the only path that removes user history.
	return writePrivateJSONAtomic(s.indexPath(), persistedSessionIndex{
		Version:  sessionPersistenceVersion,
		Sessions: items,
	})
}

func sanitizePersistedMessages(messages []sessionMessageView) []sessionMessageView {
	result := make([]sessionMessageView, len(messages))
	for index, message := range messages {
		result[index] = message
		result[index].Attachments = append([]sessionAttachmentView(nil), message.Attachments...)
		for attachmentIndex := range result[index].Attachments {
			if strings.HasPrefix(result[index].Attachments[attachmentIndex].URL, "data:") {
				result[index].Attachments[attachmentIndex].URL = ""
			}
		}
	}
	return result
}

func (s *sessionPersistenceStore) loadSnapshotLocked(sessionID string) (persistedSessionSnapshot, error) {
 if s.sqliteMode {return s.sqliteSnapshotLocked(sessionID)}
	data, err := os.ReadFile(s.snapshotPath(sessionID))
	if errors.Is(err, os.ErrNotExist) {
		return persistedSessionSnapshot{}, os.ErrNotExist
	}
	if err != nil {
		return persistedSessionSnapshot{}, err
	}
	var snapshot persistedSessionSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return persistedSessionSnapshot{}, err
	}
	if snapshot.Version != 0 && snapshot.Version != sessionPersistenceVersion {
		return persistedSessionSnapshot{}, errors.New("unsupported TL Studio session snapshot version")
	}
	return snapshot, nil
}

func (s *sessionPersistenceStore) saveSnapshotLocked(snapshot persistedSessionSnapshot) error {
	snapshot.Version = sessionPersistenceVersion
	snapshot.Runtime = s.runtime
	snapshot.SavedAt = time.Now().UnixMilli()
	snapshot.Messages = sanitizePersistedMessages(snapshot.Messages)

	if err := s.archiveExcessSnapshotMessagesLocked(&snapshot); err != nil {
		return err
	}
	return writePrivateJSONAtomic(s.snapshotPath(snapshot.Session.ID), snapshot)
}

// commitSessionSnapshotLocked is the only path for a mutation that updates
// both a Session snapshot and its list index. Both must be on disk before the
// cache is updated. A failed index write restores the prior snapshot so that
// an unrelated later operation cannot accidentally commit rejected metadata.
// The session store mutex must be held by the caller.
func (s *sessionPersistenceStore) commitSessionSnapshotLocked(session sessionView, snapshot persistedSessionSnapshot) error {
 if s.sqliteMode {return s.sqliteCommitSnapshotLocked(session,snapshot)}
	previous, previousErr := s.loadSnapshotLocked(session.ID)
	if previousErr != nil && !errors.Is(previousErr, os.ErrNotExist) {
		return previousErr
	}
	next := make(map[string]sessionView, len(s.sessions)+1)
	for id, existing := range s.sessions {
		next[id] = existing
	}
	next[session.ID] = session

	if err := s.saveSnapshotLocked(snapshot); err != nil {
		return err
	}
	if err := s.persistIndexForLocked(next); err != nil {
		var rollbackErr error
		if errors.Is(previousErr, os.ErrNotExist) {
			rollbackErr = os.Remove(s.snapshotPath(session.ID))
			if errors.Is(rollbackErr, os.ErrNotExist) { rollbackErr = nil }
		} else {
			rollbackErr = s.saveSnapshotLocked(previous)
		}
		if rollbackErr != nil {
			return fmt.Errorf("commit Session index failed: %w; restoring previous snapshot also failed: %v", err, rollbackErr)
		}
		return err
	}
	s.sessions = next
	return nil
}

func (s *sessionPersistenceStore) upsertSession(session sessionView) error {
	if strings.TrimSpace(session.ID) == "" || strings.TrimSpace(session.Directory) == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	previous := s.sessions[session.ID]
	if session.CreatedAt == 0 {
		session.CreatedAt = previous.CreatedAt
	}
	if session.Execution == "" {
		session.Execution = previous.Execution
	}
	if previous.Execution == "native" {
		if previous.Model != nil {
			model := *previous.Model
			session.Model = &model
		}
		if previous.Agent != "" {
			session.Agent = previous.Agent
		}
		session.UpdatedAt = maxSessionTimestamp(session.UpdatedAt, previous.UpdatedAt)
	}
	if session.UpdatedAt == 0 {
		session.UpdatedAt = maxSessionTimestamp(previous.UpdatedAt, time.Now().UnixMilli())
	}
	snapshot, err := s.loadSnapshotLocked(session.ID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	snapshot.Session = session
	return s.commitSessionSnapshotLocked(session, snapshot)
}

func (s *sessionPersistenceStore) putMessages(session sessionView, messages []sessionMessageView) error {
	if strings.TrimSpace(session.ID) == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	if session.Directory == "" {
		session = s.sessions[session.ID]
	}
	if session.ID == "" || session.Directory == "" {
		return nil
	}
	if session.UpdatedAt == 0 {
		session.UpdatedAt = time.Now().UnixMilli()
	}
	snapshot, err := s.loadSnapshotLocked(session.ID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	snapshot.Session = session
	if len(messages) > 0 || len(snapshot.Messages) == 0 {
		snapshot.Messages = append([]sessionMessageView(nil), messages...)
	}
	return s.commitSessionSnapshotLocked(session, snapshot)
}

func (s *sessionPersistenceStore) putChanges(session sessionView, changes []sessionChangeView) error {
	if strings.TrimSpace(session.ID) == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	if session.Directory == "" {
		session = s.sessions[session.ID]
	}
	if session.ID == "" || session.Directory == "" {
		return nil
	}
	snapshot, err := s.loadSnapshotLocked(session.ID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	snapshot.Session = session
	snapshot.Changes = append([]sessionChangeView(nil), changes...)
	return s.commitSessionSnapshotLocked(session, snapshot)
}

func (s *sessionPersistenceStore) list(limit int) ([]sessionView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	items := make([]sessionView, 0, len(s.sessions))
	for _, session := range s.sessions {
		items = append(items, session)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt == items[j].UpdatedAt {
			return items[i].ID < items[j].ID
		}
		return items[i].UpdatedAt > items[j].UpdatedAt
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *sessionPersistenceStore) getSession(sessionID string) (sessionView, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return sessionView{}, false, err
	}
	session, ok := s.sessions[strings.TrimSpace(sessionID)]
	return session, ok, nil
}

func (s *sessionPersistenceStore) getMessages(sessionID string) ([]sessionMessageView, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, false, err
	}
	snapshot, err := s.loadSnapshotLocked(strings.TrimSpace(sessionID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	messages, err := s.readArchivedSnapshotMessagesLocked(snapshot)
	if err != nil {
		return nil, false, err
	}
	return append(messages, snapshot.Messages...), true, nil
}

func (s *sessionPersistenceStore) getChanges(sessionID string) ([]sessionChangeView, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, false, err
	}
	snapshot, err := s.loadSnapshotLocked(strings.TrimSpace(sessionID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return append([]sessionChangeView(nil), snapshot.Changes...), true, nil
}

func (s *sessionPersistenceStore) recordAcceptedRun(sessionID, directory string, input sessionRunInput) error {
	sessionID = strings.TrimSpace(sessionID)
	directory = strings.TrimSpace(directory)
	if sessionID == "" || directory == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}

	session := s.sessions[sessionID]
	if session.ID == "" {
		session = sessionView{ID: sessionID, Directory: directory}
	}
	now := time.Now().UnixMilli()
	if session.CreatedAt == 0 {
		session.CreatedAt = now
	}
	session.UpdatedAt = now
	if input.Agent != "" {
		session.Agent = input.Agent
	}
	if input.Model != nil {
		model := *input.Model
		session.Model = &model
	}
	snapshot, err := s.loadSnapshotLocked(sessionID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	snapshot.Session = session

	text := strings.TrimSpace(input.Text)
	attachments := []sessionAttachmentView{}
	for _, part := range input.Parts {
		partType := sessionString(part["type"])
		if partType == "text" && text == "" {
			if value := sessionString(part["text"]); value != "" {
				if text != "" {
					text += "\n"
				}
				text += value
			}
		}
		if partType == "file" {
			attachments = append(attachments, sessionAttachmentView{
				Name: firstSessionString(part["filename"], part["name"]),
				MIME: sessionString(part["mime"]),
			})
		}
	}

	snapshot.PendingRun = &persistedSessionPendingRun{StartedAt: now, MessageID: input.MessageID}
	snapshot.Messages = append(snapshot.Messages, sessionMessageView{
		ID:          input.MessageID,
		SessionID:   sessionID,
		Role:        "user",
		Agent:       input.Agent,
		Model:       input.Model,
		CreatedAt:   now,
		Text:        text,
		Activities:  []sessionActivityView{},
		Attachments: attachments,
		Usage:       sessionUsage{},
		Changes:     []sessionChangeView{},
	})
	return s.commitSessionSnapshotLocked(session, snapshot)
}

func (s *sessionPersistenceStore) updateTitle(sessionID, title string) (sessionView, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	title = strings.TrimSpace(title)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return sessionView{}, false, err
	}
	session, ok := s.sessions[sessionID]
	if !ok {
		return sessionView{}, false, nil
	}
	session.Title = title
	session.UpdatedAt = time.Now().UnixMilli()
	snapshot, err := s.loadSnapshotLocked(sessionID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return sessionView{}, false, err
	}
	snapshot.Session = session
	if err := s.commitSessionSnapshotLocked(session, snapshot); err != nil {
		return sessionView{}, false, err
	}
	return session, true, nil
}

func (s *sessionPersistenceStore) remove(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
 if s.sqliteMode {return s.sqliteRemoveLocked(sessionID)}
	next := make(map[string]sessionView, len(s.sessions))
	for id, existing := range s.sessions {
		if id != sessionID { next[id] = existing }
	}
	if err := s.persistIndexForLocked(next); err != nil {
		return err
	}
	s.sessions = next
	// Remove the snapshot and its archived history only after the index no
	// longer references the session.
	_ = os.Remove(s.snapshotPath(sessionID))
	return s.removeSessionArchives(sessionID)
}
