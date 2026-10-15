package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/pouramin/TL-Studio/internal/sessiondb"
)

// Common Agent writes touch one message and metadata, not the entire history.
// Read the metadata revision first and retain it through Apply; another writer
// changing any part of this session produces a conflict, never a stale overwrite.
func (s *sessionPersistenceStore) sqliteDeltaLocked(id, directory string, message *sessionMessageView, changes []sessionChangeView, updatePending bool, pending *persistedSessionPendingRun, mutate func(*sessionView)) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	row, found, err := s.sqlite.Get(ctx, id)
	if err != nil {
		return err
	}
	if !found {
		return os.ErrNotExist
	}
	var session sessionView
	if err = json.Unmarshal(row.Metadata, &session); err != nil {
		return err
	}
	if session.ID != id || session.Directory != row.Directory {
		return errors.New("SQLite session metadata integrity mismatch")
	}
	if directory != "" && !sameProjectPath(directory, session.Directory) {
		return errors.New("session does not belong to the selected project")
	}
	mutate(&session)
	metadata, err := json.Marshal(session)
	if err != nil {
		return err
	}
	m := sessiondb.Mutation{SessionID: id, Directory: session.Directory, ExpectedRevision: row.Revision, Metadata: metadata, UpdatePendingRun: updatePending}
	if message != nil {
		message.SessionID = id
		if strings.TrimSpace(message.ID) == "" {
			token, e := randomSecret(16)
			if e != nil {
				return e
			}
			message.ID = "tlsm_" + token
		}
		if message.Activities == nil {
			message.Activities = []sessionActivityView{}
		}
		if message.Attachments == nil {
			message.Attachments = []sessionAttachmentView{}
		}
		if message.Changes == nil {
			message.Changes = []sessionChangeView{}
		}
		payload, e := json.Marshal(message)
		if e != nil {
			return e
		}
		m.Messages = []sessiondb.Message{{ID: message.ID, Payload: payload}}
	}
	if len(changes) > 0 {
		prior, e := s.sqlite.Changes(ctx, id)
		if e != nil {
			return e
		}
		var all []sessionChangeView
		if len(prior) > 0 {
			if e = json.Unmarshal(prior, &all); e != nil {
				return e
			}
		}
		m.ChangeData, err = json.Marshal(mergeSessionChanges(append(all, changes...)))
		if err != nil {
			return err
		}
	}
	if pending != nil {
		m.PendingRun, err = json.Marshal(pending)
		if err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(m)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(encoded)
	m.OperationID = "session-delta-" + hex.EncodeToString(sum[:])
	if _, err = s.sqlite.Apply(ctx, m); err != nil {
		return err
	}
	return s.sqliteLoadLocked()
}

func (s *sessionPersistenceStore) sqlitePutNativeLocked(id, directory string, message sessionMessageView, completed bool) error {
	now := time.Now().UnixMilli()
	return s.sqliteDeltaLocked(id, directory, &message, message.Changes, completed, nil, func(session *sessionView) {
		session.UpdatedAt = now
		if message.Agent != "" {
			session.Agent = message.Agent
		}
		if message.Model != nil {
			model := *message.Model
			session.Model = &model
		}
	})
}

func (s *sessionPersistenceStore) sqliteAcceptRunLocked(id, directory string, input sessionRunInput) error {
	attachments, err := nativeTextAttachments(input.Parts)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	text := strings.TrimSpace(input.Text)
	if text == "" {
		text = nativeRoutingPrompt(input)
	}
	message := sessionMessageView{ID: input.MessageID, SessionID: id, Role: "user", Agent: input.Agent, Model: input.Model, CreatedAt: now, Text: text, Attachments: attachments}
	if message.ID == "" {
		token, e := randomSecret(16)
		if e != nil {
			return e
		}
		message.ID = "tlsm_" + token
	}
	pending := &persistedSessionPendingRun{OwnerPID: os.Getpid(), MessageID: message.ID, StartedAt: now}
	return s.sqliteDeltaLocked(id, directory, &message, nil, true, pending, func(session *sessionView) {
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
	})
}
