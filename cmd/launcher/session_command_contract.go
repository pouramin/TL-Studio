package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const maxSessionCommandBody = 24 << 20

type sessionCreateInput struct {
	ParentID string `json:"parentID,omitempty"`
	Title    string `json:"title,omitempty"`
}

type sessionUpdateInput struct {
	Title *string `json:"title,omitempty"`
}

type sessionRunInput struct {
	Text                     string           `json:"text,omitempty"`
	Parts                    []map[string]any `json:"parts,omitempty"`
	Agent                    string           `json:"agent,omitempty"`
	Model                    *sessionModelRef `json:"model,omitempty"`
	Variant                  string           `json:"variant,omitempty"`
	MessageID                string           `json:"messageID,omitempty"`
	MaxToolRounds            int              `json:"maxToolRounds,omitempty"`
	ModelTimeoutSeconds      int              `json:"modelTimeoutSeconds,omitempty"`
	StreamIdleTimeoutSeconds int              `json:"streamIdleTimeoutSeconds,omitempty"`
	MaxRunTokens             int64            `json:"maxRunTokens,omitempty"`
}

type sessionAbortInput struct {
	Scope string `json:"scope,omitempty"`
}

type sessionCommandContract struct {
	state  *appState
	read   *sessionReadContract
	native *nativeAgentRuntime
}

func newSessionCommandContract(state *appState, read *sessionReadContract, native *nativeAgentRuntime) *sessionCommandContract {
	return &sessionCommandContract{state: state, read: read, native: native}
}

func (c *sessionCommandContract) allowedDirectory(requested string) (string, error) {
	if c.read == nil {
		return "", errors.New("session read contract is unavailable")
	}
	return c.read.allowedDirectory(requested)
}

func (c *sessionCommandContract) store() (*sessionPersistenceStore, error) {
	if c.read == nil || c.read.store == nil {
		return nil, errors.New("native session store is unavailable")
	}
	return c.read.store, nil
}

func (c *sessionCommandContract) requireSession(sessionID, directory string) (sessionView, error) {
	store, err := c.store()
	if err != nil {
		return sessionView{}, err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return sessionView{}, errors.New("session id is required")
	}
	session, ok, err := store.getSession(sessionID)
	if err != nil {
		return sessionView{}, err
	}
	if !ok {
		return sessionView{}, errors.New("session not found")
	}
	if directory != "" && !sameProjectPath(session.Directory, directory) {
		return sessionView{}, errors.New("session does not belong to the selected project")
	}
	return session, nil
}

func (c *sessionCommandContract) create(_ context.Context, directory string, input sessionCreateInput) (sessionView, error) {
	store, err := c.store()
	if err != nil {
		return sessionView{}, err
	}
	input.Title = strings.TrimSpace(input.Title)
	input.ParentID = strings.TrimSpace(input.ParentID)
	if len(input.Title) > 500 {
		return sessionView{}, errors.New("session title is too long")
	}
	return store.createNativeSession(directory, input)
}

func (c *sessionCommandContract) update(_ context.Context, directory, sessionID string, input sessionUpdateInput) (sessionView, error) {
	if _, err := c.requireSession(sessionID, directory); err != nil {
		return sessionView{}, err
	}
	if input.Title == nil {
		return sessionView{}, errors.New("no supported session fields were supplied")
	}
	title := strings.TrimSpace(*input.Title)
	if title == "" {
		return sessionView{}, errors.New("session title cannot be empty")
	}
	if len(title) > 500 {
		return sessionView{}, errors.New("session title is too long")
	}
	store, err := c.store()
	if err != nil {
		return sessionView{}, err
	}
	updated, ok, err := store.updateTitle(strings.TrimSpace(sessionID), title)
	if err != nil {
		return sessionView{}, err
	}
	if !ok {
		return sessionView{}, errors.New("session not found")
	}
	return updated, nil
}

func (c *sessionCommandContract) remove(_ context.Context, directory, sessionID string) error {
	if _, err := c.requireSession(sessionID, directory); err != nil {
		return err
	}
	store, err := c.store()
	if err != nil {
		return err
	}
	if c.native != nil {
		c.native.mu.Lock()
		defer c.native.mu.Unlock()
		if _, running := c.native.runs[strings.TrimSpace(sessionID)]; running {
			return errors.New("abort the active run before deleting this session")
		}
	}
	return store.remove(strings.TrimSpace(sessionID))
}

func (c *sessionCommandContract) run(_ context.Context, directory, sessionID string, input sessionRunInput) error {
	if _, err := c.requireSession(sessionID, directory); err != nil {
		return err
	}
	if c.native == nil {
		return errors.New("native Agent runtime is unavailable")
	}
	sessionID = strings.TrimSpace(sessionID)
	input.Text = strings.TrimSpace(input.Text)
	input.Agent = strings.TrimSpace(input.Agent)
	input.Variant = strings.TrimSpace(input.Variant)
	input.MessageID = strings.TrimSpace(input.MessageID)
	if input.Model != nil {
		input.Model.ProviderID = strings.TrimSpace(input.Model.ProviderID)
		input.Model.ID = strings.TrimSpace(input.Model.ID)
		if input.Model.ProviderID == "" && input.Model.ID == "" {
			input.Model = nil
		}
	}
	if input.Text == "" && len(input.Parts) == 0 {
		return errors.New("prompt text or parts are required")
	}
	if err := validateNativeRunLimits(input); err != nil {
		return err
	}
	if _, err := nativeTextAttachments(input.Parts); err != nil {
		return err
	}
	return c.native.Start(directory, sessionID, input)
}

func (c *sessionCommandContract) abort(_ context.Context, directory, sessionID string, input sessionAbortInput) error {
	if _, err := c.requireSession(sessionID, directory); err != nil {
		return err
	}
	if c.native == nil {
		return errors.New("native Agent runtime is unavailable")
	}
	_ = strings.TrimSpace(input.Scope)
	c.native.Abort(strings.TrimSpace(sessionID))
	return nil
}

func decodeSessionCommandJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxSessionCommandBody)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(value); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonError{Error: "invalid JSON body"})
		return false
	}
	return true
}

func writeSessionCommandError(w http.ResponseWriter, err error) {
	if strings.Contains(err.Error(), "recent-project history") || strings.Contains(err.Error(), "does not belong") {
		writeJSON(w, http.StatusForbidden, jsonError{Error: err.Error()})
		return
	}
	if strings.Contains(err.Error(), "not found") {
		writeJSON(w, http.StatusNotFound, jsonError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
}

func registerSessionCommandRoutes(mux *http.ServeMux, contract *sessionCommandContract) {
	mux.HandleFunc("POST /local/sessions", func(w http.ResponseWriter, r *http.Request) {
		directory, err := contract.allowedDirectory(r.URL.Query().Get("directory"))
		if err != nil {
			writeSessionCommandError(w, err)
			return
		}
		var input sessionCreateInput
		if !decodeSessionCommandJSON(w, r, &input) {
			return
		}
		session, err := contract.create(r.Context(), directory, input)
		if err != nil {
			writeSessionCommandError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, session)
	})
	mux.HandleFunc("PATCH /local/sessions/{sessionID}", func(w http.ResponseWriter, r *http.Request) {
		directory, err := contract.allowedDirectory(r.URL.Query().Get("directory"))
		if err != nil {
			writeSessionCommandError(w, err)
			return
		}
		var input sessionUpdateInput
		if !decodeSessionCommandJSON(w, r, &input) {
			return
		}
		session, err := contract.update(r.Context(), directory, r.PathValue("sessionID"), input)
		if err != nil {
			writeSessionCommandError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, session)
	})
	mux.HandleFunc("DELETE /local/sessions/{sessionID}", func(w http.ResponseWriter, r *http.Request) {
		directory, err := contract.allowedDirectory(r.URL.Query().Get("directory"))
		if err != nil {
			writeSessionCommandError(w, err)
			return
		}
		sessionID := r.PathValue("sessionID")
		if err := contract.remove(r.Context(), directory, sessionID); err != nil {
			writeSessionCommandError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "sessionID": sessionID})
	})
	mux.HandleFunc("POST /local/sessions/{sessionID}/runs", func(w http.ResponseWriter, r *http.Request) {
		directory, err := contract.allowedDirectory(r.URL.Query().Get("directory"))
		if err != nil {
			writeSessionCommandError(w, err)
			return
		}
		var input sessionRunInput
		if !decodeSessionCommandJSON(w, r, &input) {
			return
		}
		sessionID := r.PathValue("sessionID")
		if err := contract.run(r.Context(), directory, sessionID, input); err != nil {
			writeSessionCommandError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "sessionID": sessionID})
	})
	mux.HandleFunc("POST /local/sessions/{sessionID}/abort", func(w http.ResponseWriter, r *http.Request) {
		directory, err := contract.allowedDirectory(r.URL.Query().Get("directory"))
		if err != nil {
			writeSessionCommandError(w, err)
			return
		}
		var input sessionAbortInput
		if r.ContentLength != 0 && !decodeSessionCommandJSON(w, r, &input) {
			return
		}
		sessionID := r.PathValue("sessionID")
		if err := contract.abort(r.Context(), directory, sessionID, input); err != nil {
			writeSessionCommandError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"aborted": true, "sessionID": sessionID})
	})
}

func (c *sessionCommandContract) String() string {
	return fmt.Sprintf("sessionCommandContract(native=%t)", c.native != nil)
}
