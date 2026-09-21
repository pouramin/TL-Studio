package main

import (
	"errors"
	"os"
	"strings"
	"time"
)

func (s *sessionPersistenceStore) createNativeSession(directory string, input sessionCreateInput) (sessionView, error) {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return sessionView{}, errors.New("session directory is required")
	}
	token, err := randomSecret(12)
	if err != nil {
		return sessionView{}, err
	}
	now := time.Now().UnixMilli()
	session := sessionView{
		ID:        "tls_" + token,
		Title:     strings.TrimSpace(input.Title),
		Directory: directory,
		ParentID:  strings.TrimSpace(input.ParentID),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if session.Title == "" {
		session.Title = "New session"
	}
	if err := s.upsertSession(session); err != nil {
		return sessionView{}, err
	}
	return session, nil
}

func (s *sessionPersistenceStore) putNativeMessage(sessionID, directory string, message sessionMessageView) error {
	sessionID = strings.TrimSpace(sessionID)
	directory = strings.TrimSpace(directory)
	if sessionID == "" {
		return errors.New("session id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}

	session := s.sessions[sessionID]
	if session.ID == "" {
		session = sessionView{ID: sessionID, Directory: directory, CreatedAt: time.Now().UnixMilli()}
	}
	if session.Directory == "" {
		session.Directory = directory
	}
	session.UpdatedAt = time.Now().UnixMilli()
	if message.Agent != "" {
		session.Agent = message.Agent
	}
	if message.Model != nil {
		model := *message.Model
		session.Model = &model
	}
	s.sessions[sessionID] = session

	snapshot, err := s.loadSnapshotLocked(sessionID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) {
		snapshot = persistedSessionSnapshot{}
	}
	snapshot.Session = session
	message.SessionID = sessionID
	if message.Activities == nil {
		message.Activities = []sessionActivityView{}
	}
	if message.Attachments == nil {
		message.Attachments = []sessionAttachmentView{}
	}
	if message.Changes == nil {
		message.Changes = []sessionChangeView{}
	}

	replaced := false
	if message.ID != "" {
		for index := range snapshot.Messages {
			if snapshot.Messages[index].ID == message.ID {
				snapshot.Messages[index] = message
				replaced = true
				break
			}
		}
	}
	if !replaced {
		snapshot.Messages = append(snapshot.Messages, message)
	}
	if len(message.Changes) > 0 {
		snapshot.Changes = mergeSessionChanges(append(snapshot.Changes, message.Changes...))
	}
	if err := s.saveSnapshotLocked(snapshot); err != nil {
		return err
	}
	return s.persistIndexLocked()
}
