package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestKiloQuestionAdapterTranslatesRuntimeQuestions(t *testing.T) {
	project := t.TempDir()
	replySeen := false
	rejectSeen := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "runtime" || pass != "secret" {
			t.Fatalf("runtime auth mismatch: %q %q %v", user, pass, ok)
		}
		header, err := url.QueryUnescape(r.Header.Get("x-kilo-directory"))
		if err != nil {
			t.Fatal(err)
		}
		if header != project {
			t.Fatalf("runtime project header=%q want %q", header, project)
		}
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/question":
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{
					"id": "q1", "sessionID": "s1",
					"questions": []any{
						map[string]any{
							"header": "Pick",
							"question": "Choose",
							"multiple": true,
							"custom": false,
							"default": "A",
							"options": []any{
								map[string]any{"label": "A", "description": "first"},
								map[string]any{"label": "B"},
							},
						},
					},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/question/q1/reply":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if _, ok := payload["answers"].([]any); !ok {
				t.Fatalf("runtime question reply missing answers: %#v", payload)
			}
			replySeen = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/question/q1/reject":
			rejectSeen = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected runtime question request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	state := &appState{project: project}
	engine := kiloRuntimeEngine{}
	backend, err := newRuntimeBackend(
		state,
		server.URL,
		runtimeCredentials{Username: "runtime", Password: "secret"},
		engine,
	)
	if err != nil {
		t.Fatal(err)
	}
	adapter := engine.Questions()

	items, err := adapter.ListQuestions(context.Background(), backend, project)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "q1" || items[0].SessionID != "s1" {
		t.Fatalf("question mapping mismatch: %#v", items)
	}
	prompt := items[0].Questions[0]
	if prompt.Question != "Choose" || !prompt.Multiple || prompt.Custom || prompt.Default != "A" || len(prompt.Options) != 2 {
		t.Fatalf("question prompt mapping mismatch: %#v", prompt)
	}

	if err := adapter.ReplyQuestion(context.Background(), backend, project, "q1", [][]string{{"A", "B"}}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.RejectQuestion(context.Background(), backend, project, "q1"); err != nil {
		t.Fatal(err)
	}
	if !replySeen || !rejectSeen {
		t.Fatalf("question actions were not translated: reply=%v reject=%v", replySeen, rejectSeen)
	}
}
