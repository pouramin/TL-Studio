package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestProjectRuntimeEventToTLStudioSemantics(t *testing.T) {
	tests := []struct {
		name      string
		raw       map[string]any
		wantType  string
		wantKind  string
		wantSID   string
		wantMID   string
		wantPath  string
		wantOK    bool
	}{
		{
			name: "connected",
			raw: map[string]any{"type": "server.connected"},
			wantType: "stream.ready",
			wantOK: true,
		},
		{
			name: "session status",
			raw: map[string]any{"type": "session.status", "properties": map[string]any{"sessionID": "s1"}},
			wantType: "session.changed",
			wantSID: "s1",
			wantOK: true,
		},
		{
			name: "message part wrapper",
			raw: map[string]any{"payload": map[string]any{
				"type": "message.part.updated",
				"properties": map[string]any{"part": map[string]any{"sessionID": "s2", "messageID": "m2"}},
			}},
			wantType: "message.changed",
			wantSID: "s2",
			wantMID: "m2",
			wantOK: true,
		},
		{
			name: "permission",
			raw: map[string]any{"type": "permission.asked", "properties": map[string]any{"sessionID": "s3"}},
			wantType: "attention.changed",
			wantKind: "permission",
			wantSID: "s3",
			wantOK: true,
		},
		{
			name: "question",
			raw: map[string]any{"type": "question.replied", "properties": map[string]any{"sessionID": "s4"}},
			wantType: "attention.changed",
			wantKind: "question",
			wantSID: "s4",
			wantOK: true,
		},
		{
			name: "workspace",
			raw: map[string]any{"type": "file.edited", "properties": map[string]any{"path": "src/main.ts", "sessionID": "s5"}},
			wantType: "workspace.changed",
			wantSID: "s5",
			wantPath: "src/main.ts",
			wantOK: true,
		},
		{
			name: "unknown ignored",
			raw: map[string]any{"type": "experimental.internal"},
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := projectRuntimeEvent(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want %v, event=%#v", ok, tc.wantOK, got)
			}
			if !ok {
				return
			}
			if got.Version != liveEventContractVersion || got.Type != tc.wantType || got.AttentionKind != tc.wantKind || got.SessionID != tc.wantSID || got.MessageID != tc.wantMID || got.Path != tc.wantPath {
				t.Fatalf("unexpected projected event: %#v", got)
			}
		})
	}
}

func TestLiveEventRouteProjectsRuntimeSSE(t *testing.T) {
	project := t.TempDir()
	var sawAuth bool
	var sawDirectory bool

	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/global/event" {
			t.Fatalf("unexpected runtime path: %s", r.URL.Path)
		}
		user, pass, ok := r.BasicAuth()
		sawAuth = ok && user == "runtime-user" && pass == "runtime-pass"
		queryDirectory, _ := url.QueryUnescape(r.URL.Query().Get("directory"))
		headerDirectory, _ := url.QueryUnescape(r.Header.Get("x-kilo-directory"))
		sawDirectory = queryDirectory == project && headerDirectory == project

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"payload\":{\"type\":\"message.updated\",\"properties\":{\"info\":{\"sessionID\":\"session-1\",\"id\":\"message-1\"}}}}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	defer runtimeServer.Close()

	state := &appState{project: project}
	contract, err := newLiveEventContract(state, runtimeServer.URL, "runtime-user", "runtime-pass")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerLiveEventRoutes(mux, contract)
	server := httptest.NewServer(mux)
	defer server.Close()

	response, err := http.Get(server.URL + "/local/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("event route status=%d", response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("event route content type=%q", got)
	}

	scanner := bufio.NewScanner(response.Body)
	var payload string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			payload = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			break
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if payload == "" {
		t.Fatal("projected event payload missing")
	}

	var event liveEventView
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "message.changed" || event.SessionID != "session-1" || event.MessageID != "message-1" {
		t.Fatalf("unexpected projected event: %#v", event)
	}
	if strings.Contains(payload, "message.updated") || strings.Contains(payload, "properties") {
		t.Fatalf("raw runtime event details leaked into Browser contract: %s", payload)
	}
	if !sawAuth || !sawDirectory {
		t.Fatalf("runtime event projection did not preserve auth/project routing: auth=%v directory=%v", sawAuth, sawDirectory)
	}
}
