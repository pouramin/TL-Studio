package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	sessionPersistenceVersion = 1
	maxPersistedSessions       = 300
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
	SavedAt int64                `json:"savedAt"`
}

type sessionPersistenceStore struct {
	mu       sync.Mutex
	root     string
	runtime  string
	loaded   bool
	sessions map[string]sessionView
}

func sessionPersistenceRoot() string {
	return filepath.Join(tlStudioStateDirectory(), "sessions")
}

func newSessionPersistenceStore(root, runtimeID string) *sessionPersistenceStore {
	return &sessionPersistenceStore{
		root:     root,
		runtime:  strings.TrimSpace(runtimeID),
		sessions: map[string]sessionView{},
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

func writePrivateJSONAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
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
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		if writeErr := os.WriteFile(path, data, 0o600); writeErr != nil {
			return err
		}
	}
	return nil
}

func (s *sessionPersistenceStore) loadLocked() error {
	if s.loaded {
		return nil
	}
	s.loaded = true
	s.sessions = map[string]sessionView{}
	data, err := os.ReadFile(s.indexPath())
	if errors.Is(err, os.ErrNotExist) {
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
		s.sessions[session.ID] = session
	}
	return nil
}

func (s *sessionPersistenceStore) persistIndexLocked() error {
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
	if len(items) > maxPersistedSessions {
		for _, stale := range items[maxPersistedSessions:] {
			delete(s.sessions, stale.ID)
			_ = os.Remove(s.snapshotPath(stale.ID))
		}
		items = items[:maxPersistedSessions]
	}
	return writePrivateJSONAtomic(s.indexPath(), persistedSessionIndex{
		Version:  sessionPersistenceVersion,
		Sessions: items,
	})
}

func sanitizePersistedMessages(messages []sessionMessageView) []sessionMessageView {
	if len(messages) > maxPersistedMessages {
		messages = messages[len(messages)-maxPersistedMessages:]
	}
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

	for len(snapshot.Messages) > 1 {
		data, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		if len(data) <= maxSessionSnapshotBytes {
			break
		}
		snapshot.Messages = snapshot.Messages[len(snapshot.Messages)/4:]
	}
	return writePrivateJSONAtomic(s.snapshotPath(snapshot.Session.ID), snapshot)
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
	if session.UpdatedAt == 0 {
		session.UpdatedAt = maxSessionTimestamp(previous.UpdatedAt, time.Now().UnixMilli())
	}
	s.sessions[session.ID] = session

	snapshot, err := s.loadSnapshotLocked(session.ID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	snapshot.Session = session
	if err := s.saveSnapshotLocked(snapshot); err != nil {
		return err
	}
	return s.persistIndexLocked()
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
	s.sessions[session.ID] = session
	snapshot, err := s.loadSnapshotLocked(session.ID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	snapshot.Session = session
	if len(messages) > 0 || len(snapshot.Messages) == 0 {
		snapshot.Messages = append([]sessionMessageView(nil), messages...)
	}
	if err := s.saveSnapshotLocked(snapshot); err != nil {
		return err
	}
	return s.persistIndexLocked()
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
	s.sessions[session.ID] = session
	snapshot, err := s.loadSnapshotLocked(session.ID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	snapshot.Session = session
	snapshot.Changes = append([]sessionChangeView(nil), changes...)
	if err := s.saveSnapshotLocked(snapshot); err != nil {
		return err
	}
	return s.persistIndexLocked()
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
	return append([]sessionMessageView(nil), snapshot.Messages...), true, nil
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
	s.sessions[sessionID] = session

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
	if err := s.saveSnapshotLocked(snapshot); err != nil {
		return err
	}
	return s.persistIndexLocked()
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
	s.sessions[sessionID] = session
	snapshot, err := s.loadSnapshotLocked(sessionID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return sessionView{}, false, err
	}
	snapshot.Session = session
	if err := s.saveSnapshotLocked(snapshot); err != nil {
		return sessionView{}, false, err
	}
	if err := s.persistIndexLocked(); err != nil {
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
	delete(s.sessions, sessionID)
	_ = os.Remove(s.snapshotPath(sessionID))
	return s.persistIndexLocked()
}
