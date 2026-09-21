package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
)

type commandTestAdapter struct {
	createInput sessionCreateInput
	updateInput sessionUpdateInput
	runInput    sessionRunInput
	abortInput  sessionAbortInput
	updatedID   string
	deletedID   string
	runID       string
	abortID     string
}

func (a *commandTestAdapter) CreateSession(_ context.Context, _ *runtimeBackend, _ string, input sessionCreateInput) (string, error) {
	a.createInput = input
	return "s1", nil
}

func (a *commandTestAdapter) UpdateSession(_ context.Context, _ *runtimeBackend, _ string, sessionID string, input sessionUpdateInput) error {
	a.updatedID = sessionID
	a.updateInput = input
	return nil
}

func (a *commandTestAdapter) DeleteSession(_ context.Context, _ *runtimeBackend, _ string, sessionID string) error {
	a.deletedID = sessionID
	return nil
}

func (a *commandTestAdapter) RunSession(_ context.Context, _ *runtimeBackend, _ string, sessionID string, input sessionRunInput) error {
	a.runID = sessionID
	a.runInput = input
	return nil
}

func (a *commandTestAdapter) AbortSession(_ context.Context, _ *runtimeBackend, _ string, sessionID string, input sessionAbortInput) error {
	a.abortID = sessionID
	a.abortInput = input
	return nil
}

type commandTestEngine struct {
	adapter *commandTestAdapter
}

func (*commandTestEngine) ID() string { return "command-test" }
func (*commandTestEngine) FindBinary(override string) (string, error) { return override, nil }
func (*commandTestEngine) Command(ctx context.Context, binary string, _ int, _ runtimeCredentials) *exec.Cmd {
	return exec.CommandContext(ctx, binary)
}
func (*commandTestEngine) PrepareRequest(req *http.Request, project string, credentials runtimeCredentials) {
	req.Header.Set("X-TL-Project", project)
	req.Header.Set("X-TL-Credential", credentials.Username+":"+credentials.Password)
}
func (e *commandTestEngine) SessionCommands() runtimeSessionCommandAdapter { return e.adapter }

func sessionCommandRequest(t *testing.T, method, address string, body any) *http.Response {
	t.Helper()
	var encoded *bytes.Reader
	if body == nil {
		encoded = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		encoded = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, address, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestSessionCommandRoutesUseEngineAdapterAndSemanticSurface(t *testing.T) {
	project := t.TempDir()
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session/s1" {
			t.Fatalf("unexpected semantic read path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "s1",
			"title": "Updated",
			"directory": project,
			"time": map[string]any{"created": 1000, "updated": 2000},
		})
	}))
	defer runtimeServer.Close()

	state := &appState{project: project}
	adapter := &commandTestAdapter{}
	engine := &commandTestEngine{adapter: adapter}
	backend, err := newRuntimeBackend(
		state,
		runtimeServer.URL,
		runtimeCredentials{Username: "runtime", Password: "secret"},
		engine,
	)
	if err != nil {
		t.Fatal(err)
	}
	read := newSessionReadContractWithBackend(state, backend)
	read.history = newProjectHistoryStoreForTest(t.TempDir() + "/projects.json")
	read.store = newSessionPersistenceStore(t.TempDir()+"/sessions", engine.ID())
	read.history.remember(project)
	commands := newSessionCommandContract(state, backend, read)

	mux := http.NewServeMux()
	registerSessionReadRoutes(mux, read)
	registerSessionCommandRoutes(mux, commands)
	server := httptest.NewServer(mux)
	defer server.Close()

	res := sessionCommandRequest(t, http.MethodPost, server.URL+"/local/sessions", map[string]any{"title": "First"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create status=%d", res.StatusCode)
	}
	_ = res.Body.Close()
	if adapter.createInput.Title != "First" {
		t.Fatalf("create input not projected: %#v", adapter.createInput)
	}

	res = sessionCommandRequest(t, http.MethodPatch, server.URL+"/local/sessions/s1", map[string]any{"title": "Updated"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update status=%d", res.StatusCode)
	}
	_ = res.Body.Close()
	if adapter.updatedID != "s1" || adapter.updateInput.Title == nil || *adapter.updateInput.Title != "Updated" {
		t.Fatalf("update input not projected: %#v", adapter.updateInput)
	}

	res = sessionCommandRequest(t, http.MethodPost, server.URL+"/local/sessions/s1/runs", map[string]any{
		"text": "hello",
		"parts": []any{
			map[string]any{"type": "text", "text": "hello"},
			map[string]any{"type": "file", "filename": "note.txt", "mime": "text/plain", "url": "data:text/plain;base64,aGk="},
		},
		"agent": "code",
		"model": map[string]any{"providerID": "test", "id": "model-1"},
		"variant": "fast",
	})
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("run status=%d", res.StatusCode)
	}
	_ = res.Body.Close()
	if adapter.runID != "s1" || adapter.runInput.Agent != "code" || adapter.runInput.Model == nil || adapter.runInput.Model.ID != "model-1" || len(adapter.runInput.Parts) != 2 {
		t.Fatalf("run input not projected: %#v", adapter.runInput)
	}

	res = sessionCommandRequest(t, http.MethodPost, server.URL+"/local/sessions/s1/abort", map[string]any{"scope": "tree"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("abort status=%d", res.StatusCode)
	}
	_ = res.Body.Close()
	if adapter.abortID != "s1" || adapter.abortInput.Scope != "tree" {
		t.Fatalf("abort input not projected: %#v", adapter.abortInput)
	}

	res = sessionCommandRequest(t, http.MethodDelete, server.URL+"/local/sessions/s1", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete status=%d", res.StatusCode)
	}
	_ = res.Body.Close()
	if adapter.deletedID != "s1" {
		t.Fatalf("delete did not reach adapter: %q", adapter.deletedID)
	}
}

func TestSessionCommandRoutesRejectInvalidRun(t *testing.T) {
	project := t.TempDir()
	state := &appState{project: project}
	engine := &commandTestEngine{adapter: &commandTestAdapter{}}
	backend, err := newRuntimeBackend(state, "http://127.0.0.1:1", runtimeCredentials{}, engine)
	if err != nil {
		t.Fatal(err)
	}
	read := newSessionReadContractWithBackend(state, backend)
	read.history = newProjectHistoryStoreForTest(t.TempDir() + "/projects.json")
	read.store = newSessionPersistenceStore(t.TempDir()+"/sessions", engine.ID())
	commands := newSessionCommandContract(state, backend, read)
	mux := http.NewServeMux()
	registerSessionCommandRoutes(mux, commands)
	server := httptest.NewServer(mux)
	defer server.Close()

	res := sessionCommandRequest(t, http.MethodPost, server.URL+"/local/sessions/s1/runs", map[string]any{})
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected invalid run to return 400, got %d", res.StatusCode)
	}
}
