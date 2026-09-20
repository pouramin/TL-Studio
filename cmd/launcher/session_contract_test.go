package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func testSessionContract(t *testing.T, handler http.HandlerFunc) (*sessionReadContract, *appState, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	current := t.TempDir()
	state := &appState{project: current}
	contract, err := newSessionReadContract(state, server.URL, "runtime", "secret")
	if err != nil {
		t.Fatal(err)
	}
	contract.history = newProjectHistoryStoreForTest(filepath.Join(t.TempDir(), "projects.json"))
	contract.history.remember(current)
	return contract, state, server
}

func newProjectHistoryStoreForTest(path string) *projectHistoryStore {
	return &projectHistoryStore{filePath: path}
}

func requireRuntimeAuth(t *testing.T, r *http.Request) {
	t.Helper()
	user, pass, ok := r.BasicAuth()
	if !ok || user != "runtime" || pass != "secret" {
		t.Fatalf("bad runtime auth: %q %q %v", user, pass, ok)
	}
}

func TestSessionReadContractNormalizesMessagesAndToolActivity(t *testing.T) {
	contract, state, _ := testSessionContract(t, func(w http.ResponseWriter, r *http.Request) {
		requireRuntimeAuth(t, r)
		if r.URL.Path != "/session/s1/message" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		headerDirectory := stateProjectFromHeader(t, r)
		if r.URL.Query().Get("directory") == "" || r.URL.Query().Get("directory") != headerDirectory {
			t.Fatalf("directory query/header mismatch: %s %s", r.URL.Query().Get("directory"), r.Header.Get("x-kilo-directory"))
		}
		writeJSON(w, http.StatusOK, []any{
			map[string]any{
				"info": map[string]any{
					"id": "m1", "sessionID": "s1", "role": "assistant", "agent": "code",
					"time": map[string]any{"created": float64(1000), "completed": float64(2000)},
					"tokens": map[string]any{"input": float64(10), "output": float64(5), "cache": map[string]any{"read": float64(2)}},
				},
				"parts": []any{
					map[string]any{"type": "reasoning", "text": "checking", "time": map[string]any{"start": float64(1100), "end": float64(1200)}},
					map[string]any{
						"type": "tool", "tool": "write",
						"state": map[string]any{
							"status": "completed",
							"input": map[string]any{"filePath": "hello.txt"},
							"output": map[string]any{"ok": true},
							"metadata": map[string]any{"filediff": map[string]any{
								"file": "hello.txt", "additions": float64(1), "deletions": float64(0), "patch": "+ok",
							}},
						},
					},
					map[string]any{"type": "step-finish", "model": map[string]any{"providerID": "test", "modelID": "test-model"}, "tokens": map[string]any{"input": float64(10), "output": float64(5)}, "time": map[string]any{"elapsed": float64(900)}},
					map[string]any{"type": "text", "text": "done"},
				},
			},
		})
	})

	messages, err := contract.getMessages(context.Background(), "s1", state.projectPath(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected one message, got %d", len(messages))
	}
	message := messages[0]
	if message.Role != "assistant" || message.Text != "done" || message.Agent != "code" {
		t.Fatalf("unexpected semantic message: %#v", message)
	}
	if message.Usage.Input != 10 || message.Usage.Output != 5 || message.Usage.CacheRead != 2 {
		t.Fatalf("usage was not normalized: %#v", message.Usage)
	}
	if len(message.Activities) != 3 {
		t.Fatalf("expected 3 semantic activities, got %#v", message.Activities)
	}
	tool := message.Activities[1]
	if tool.Kind != "tool" || tool.ToolID != "files.write" || tool.RuntimeToolID != "write" || tool.ToolName != "Write file" {
		t.Fatalf("write tool was not mapped through TL Studio semantics: %#v", tool)
	}
	if len(message.Changes) != 1 || message.Changes[0].File != "hello.txt" {
		t.Fatalf("tool change was not projected: %#v", message.Changes)
	}
}

func TestSessionReadContractNormalizesStatus(t *testing.T) {
	contract, state, _ := testSessionContract(t, func(w http.ResponseWriter, r *http.Request) {
		requireRuntimeAuth(t, r)
		if r.URL.Path != "/session/status" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"s1": map[string]any{"type": "busy"},
			"s2": map[string]any{"type": "retry", "attempt": float64(2), "next": float64(9000), "message": "temporary"},
			"s3": map[string]any{"type": "idle"},
		})
	})

	statuses, err := contract.getStatuses(context.Background(), state.projectPath())
	if err != nil {
		t.Fatal(err)
	}
	if statuses["s1"].State != "running" || !statuses["s1"].Active {
		t.Fatalf("busy status mismatch: %#v", statuses["s1"])
	}
	if statuses["s2"].State != "retrying" || statuses["s2"].Attempt != 2 || !statuses["s2"].Active {
		t.Fatalf("retry status mismatch: %#v", statuses["s2"])
	}
	if statuses["s3"].State != "idle" || statuses["s3"].Active {
		t.Fatalf("idle status mismatch: %#v", statuses["s3"])
	}
}

func TestSessionReadContractAggregatesRecentProjects(t *testing.T) {
	var first, second string
	contract, state, _ := testSessionContract(t, func(w http.ResponseWriter, r *http.Request) {
		requireRuntimeAuth(t, r)
		if r.URL.Path != "/session" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		directory := r.URL.Query().Get("directory")
		switch directory {
		case first:
			writeJSON(w, http.StatusOK, []any{map[string]any{"id": "a", "title": "A", "time": map[string]any{"created": float64(10), "updated": float64(20)}}})
		case second:
			writeJSON(w, http.StatusOK, []any{map[string]any{"id": "b", "title": "B", "time": map[string]any{"created": float64(30), "updated": float64(40)}}})
		default:
			t.Fatalf("unexpected directory: %q", directory)
		}
	})
	first = state.projectPath()
	second = t.TempDir()
	contract.history.remember(second)

	sessions, err := contract.listSessions(context.Background(), 150)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0].ID != "b" || sessions[1].ID != "a" {
		t.Fatalf("unexpected merged sessions: %#v", sessions)
	}
	if sessions[0].Directory != second || sessions[1].Directory != first {
		t.Fatalf("directory fallback was not preserved: %#v", sessions)
	}
}

func TestSessionReadContractChangesFallsBackToSemanticMessages(t *testing.T) {
	contract, state, _ := testSessionContract(t, func(w http.ResponseWriter, r *http.Request) {
		requireRuntimeAuth(t, r)
		switch r.URL.Path {
		case "/session/s1/diff":
			writeJSON(w, http.StatusOK, []any{})
		case "/session/s1/message":
			writeJSON(w, http.StatusOK, []any{
				map[string]any{
					"info": map[string]any{"id": "m1", "role": "assistant"},
					"parts": []any{map[string]any{
						"type": "tool", "tool": "edit",
						"state": map[string]any{
							"status": "completed",
							"input": map[string]any{"filePath": "src/app.ts"},
							"metadata": map[string]any{"filediff": map[string]any{
								"file": "src/app.ts", "additions": float64(2), "deletions": float64(1), "patch": "@@",
							}},
						},
					}},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})

	changes, err := contract.getChanges(context.Background(), "s1", state.projectPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].File != "src/app.ts" || changes[0].Additions != 2 || changes[0].Deletions != 1 {
		t.Fatalf("unexpected semantic changes: %#v", changes)
	}
}

func TestSessionReadRoutesRejectUnknownDirectory(t *testing.T) {
	contract, _, _ := testSessionContract(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("runtime should not be called for an unknown project")
	})
	mux := http.NewServeMux()
	registerSessionReadRoutes(mux, contract)
	server := httptest.NewServer(mux)
	defer server.Close()

	requested := filepath.Join(t.TempDir(), "unknown")
	res, err := http.Get(server.URL + "/local/sessions/s1/messages?directory=" + url.QueryEscape(requested))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.StatusCode)
	}
}

func stateProjectFromHeader(t *testing.T, r *http.Request) string {
	t.Helper()
	header := r.Header.Get("x-kilo-directory")
	value, err := url.QueryUnescape(header)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(value)
}
