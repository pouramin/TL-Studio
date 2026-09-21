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

var errSessionCommandsUnsupported = errors.New("runtime engine does not provide session command capability")

type sessionCreateInput struct {
	ParentID string `json:"parentID,omitempty"`
	Title    string `json:"title,omitempty"`
}

type sessionUpdateInput struct {
	Title *string `json:"title,omitempty"`
}

type sessionRunInput struct {
	Text      string           `json:"text,omitempty"`
	Parts     []map[string]any `json:"parts,omitempty"`
	Agent     string           `json:"agent,omitempty"`
	Model     *sessionModelRef `json:"model,omitempty"`
	Variant   string           `json:"variant,omitempty"`
	MessageID string           `json:"messageID,omitempty"`
}

type sessionAbortInput struct {
	Scope string `json:"scope,omitempty"`
}

type runtimeSessionCommandAdapter interface {
	CreateSession(ctx context.Context, backend *runtimeBackend, directory string, input sessionCreateInput) (string, error)
	UpdateSession(ctx context.Context, backend *runtimeBackend, directory, sessionID string, input sessionUpdateInput) error
	DeleteSession(ctx context.Context, backend *runtimeBackend, directory, sessionID string) error
	RunSession(ctx context.Context, backend *runtimeBackend, directory, sessionID string, input sessionRunInput) error
	AbortSession(ctx context.Context, backend *runtimeBackend, directory, sessionID string, input sessionAbortInput) error
}

type runtimeSessionCommandProvider interface {
	SessionCommands() runtimeSessionCommandAdapter
}

type sessionRunPersistenceOwner interface {
	ownsRunPersistence(input sessionRunInput) bool
}

type sessionCommandContract struct {
	state   *appState
	backend *runtimeBackend
	read    *sessionReadContract
	adapter runtimeSessionCommandAdapter
}

func newSessionCommandContract(state *appState, backend *runtimeBackend, read *sessionReadContract) *sessionCommandContract {
	var adapter runtimeSessionCommandAdapter
	if provider, ok := backend.engine.(runtimeSessionCommandProvider); ok {
		adapter = provider.SessionCommands()
	}
	return &sessionCommandContract{
		state:   state,
		backend: backend,
		read:    read,
		adapter: adapter,
	}
}

func (c *sessionCommandContract) requireAdapter() (runtimeSessionCommandAdapter, error) {
	if c.adapter == nil {
		return nil, errSessionCommandsUnsupported
	}
	return c.adapter, nil
}

func (c *sessionCommandContract) allowedDirectory(requested string) (string, error) {
	if c.read == nil {
		return "", errors.New("session read contract is unavailable")
	}
	return c.read.allowedDirectory(requested)
}

func (c *sessionCommandContract) create(ctx context.Context, directory string, input sessionCreateInput) (sessionView, error) {
	adapter, err := c.requireAdapter()
	if err != nil {
		return sessionView{}, err
	}
	input.Title = strings.TrimSpace(input.Title)
	input.ParentID = strings.TrimSpace(input.ParentID)
	if len(input.Title) > 500 {
		return sessionView{}, errors.New("session title is too long")
	}
	sessionID, err := adapter.CreateSession(ctx, c.backend, directory, input)
	if err != nil {
		return sessionView{}, err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return sessionView{}, errors.New("runtime did not return a session id")
	}
	return c.read.getSession(ctx, sessionID, directory)
}

func (c *sessionCommandContract) update(ctx context.Context, directory, sessionID string, input sessionUpdateInput) (sessionView, error) {
	adapter, err := c.requireAdapter()
	if err != nil {
		return sessionView{}, err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return sessionView{}, errors.New("session id is required")
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
	input.Title = &title
	if err := adapter.UpdateSession(ctx, c.backend, directory, sessionID, input); err != nil {
		var runtimeErr *sessionRuntimeError
		if errors.As(err, &runtimeErr) && runtimeErr.Status == http.StatusNotFound && c.read != nil && c.read.store != nil {
			if persisted, ok, storeErr := c.read.store.updateTitle(sessionID, title); storeErr != nil {
				return sessionView{}, storeErr
			} else if ok {
				return persisted, nil
			}
		}
		return sessionView{}, err
	}
	return c.read.getSession(ctx, sessionID, directory)
}

func (c *sessionCommandContract) remove(ctx context.Context, directory, sessionID string) error {
	adapter, err := c.requireAdapter()
	if err != nil {
		return err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session id is required")
	}
	runtimeDeleteErr := adapter.DeleteSession(ctx, c.backend, directory, sessionID)
	if runtimeDeleteErr != nil {
		var runtimeErr *sessionRuntimeError
		if !errors.As(runtimeDeleteErr, &runtimeErr) || runtimeErr.Status != http.StatusNotFound {
			return runtimeDeleteErr
		}
		if c.read == nil || c.read.store == nil {
			return runtimeDeleteErr
		}
		if _, ok, storeErr := c.read.store.getSession(sessionID); storeErr != nil {
			return storeErr
		} else if !ok {
			return runtimeDeleteErr
		}
	}
	if c.read != nil && c.read.store != nil {
		if err := c.read.store.remove(sessionID); err != nil {
			return err
		}
	}
	return nil
}

func (c *sessionCommandContract) run(ctx context.Context, directory, sessionID string, input sessionRunInput) error {
	adapter, err := c.requireAdapter()
	if err != nil {
		return err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session id is required")
	}
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
	if err := adapter.RunSession(ctx, c.backend, directory, sessionID, input); err != nil {
		return err
	}
	ownsPersistence := false
	if owner, ok := adapter.(sessionRunPersistenceOwner); ok {
		ownsPersistence = owner.ownsRunPersistence(input)
	}
	if !ownsPersistence && c.read != nil && c.read.store != nil {
		if err := c.read.store.recordAcceptedRun(sessionID, directory, input); err != nil {
			return err
		}
	}
	return nil
}

func (c *sessionCommandContract) abort(ctx context.Context, directory, sessionID string, input sessionAbortInput) error {
	adapter, err := c.requireAdapter()
	if err != nil {
		return err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session id is required")
	}
	input.Scope = strings.TrimSpace(input.Scope)
	return adapter.AbortSession(ctx, c.backend, directory, sessionID, input)
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
	if errors.Is(err, errSessionCommandsUnsupported) {
		writeJSON(w, http.StatusNotImplemented, jsonError{Error: err.Error()})
		return
	}
	var runtimeErr *sessionRuntimeError
	if errors.As(err, &runtimeErr) || strings.Contains(err.Error(), "recent-project history") {
		writeSessionContractError(w, err)
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
		if r.ContentLength != 0 {
			if !decodeSessionCommandJSON(w, r, &input) {
				return
			}
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
	return fmt.Sprintf("sessionCommandContract(engine=%s)", c.backend.engine.ID())
}
