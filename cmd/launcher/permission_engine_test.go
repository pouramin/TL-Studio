package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPermissionPolicyStorePersistsProjectScopedRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "permissions.json")
	store := newPermissionPolicyStore(path)

	added, err := store.addAllowRules("/tmp/project-a", "edit", []string{"src/*", "src/*"})
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 {
		t.Fatalf("expected one deduplicated rule, got %d", len(added))
	}

	reloaded := newPermissionPolicyStore(path)
	rules, err := reloaded.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected one persisted rule, got %d", len(rules))
	}
	if rules[0].Permission != "edit" || rules[0].Matcher != "src/*" || rules[0].Decision != "allow" {
		t.Fatalf("unexpected persisted rule: %#v", rules[0])
	}

	covered, err := reloaded.covers("/tmp/project-a", "edit", []string{"src/*"})
	if err != nil {
		t.Fatal(err)
	}
	if !covered {
		t.Fatal("expected project rule to cover matching permission")
	}
	covered, err = reloaded.covers("/tmp/project-b", "edit", []string{"src/*"})
	if err != nil {
		t.Fatal(err)
	}
	if covered {
		t.Fatal("permission rule must not leak across projects")
	}
}

func TestPermissionEngineOwnsAlwaysPolicyAndKeepsSensitiveRequestsInteractive(t *testing.T) {
	var mu sync.Mutex
	pending := []map[string]any{
		{
			"id":         "req-1",
			"sessionID":  "session-1",
			"permission": "edit",
			"always":     []any{"src/*"},
			"patterns":   []any{"src/main.go"},
			"metadata":   map[string]any{},
		},
	}
	replies := []map[string]any{}

	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/permission":
			writeJSON(w, http.StatusOK, pending)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/permission/") && strings.HasSuffix(r.URL.Path, "/reply"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode runtime reply: %v", err)
			}
			replies = append(replies, body)
			pending = nil
			writeJSON(w, http.StatusOK, true)
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtime.Close()

	state := &appState{project: filepath.Join(t.TempDir(), "project")}
	engine, err := newPermissionEngine(state, runtime.URL, "runtime", "secret")
	if err != nil {
		t.Fatal(err)
	}
	engine.store = newPermissionPolicyStore(filepath.Join(t.TempDir(), "permissions.json"))

	result, err := engine.reply(context.Background(), "req-1", "session-1", "always", "")
	if err != nil {
		t.Fatal(err)
	}
	if result["remembered"] != 1 {
		t.Fatalf("expected remembered rule result, got %#v", result)
	}
	if len(replies) != 1 || replies[0]["reply"] != "once" || replies[0]["interactive"] != true {
		t.Fatalf("explicit Always must be translated to one interactive runtime approval, got %#v", replies)
	}

	mu.Lock()
	pending = []map[string]any{
		{
			"id":         "req-2",
			"sessionID":  "session-1",
			"permission": "edit",
			"always":     []any{"src/*"},
			"patterns":   []any{"src/other.go"},
			"metadata":   map[string]any{},
		},
	}
	mu.Unlock()

	visible, err := engine.listPending(context.Background(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 0 {
		t.Fatalf("remembered non-sensitive permission should auto-approve, got %#v", visible)
	}
	if len(replies) != 2 || replies[1]["reply"] != "once" || replies[1]["interactive"] != false {
		t.Fatalf("policy auto-approval must be non-interactive runtime once, got %#v", replies)
	}

	mu.Lock()
	pending = []map[string]any{
		{
			"id":         "req-3",
			"sessionID":  "session-1",
			"permission": "edit",
			"always":     []any{"src/*"},
			"metadata":   map[string]any{"skillShell": true},
		},
	}
	mu.Unlock()

	visible, err = engine.listPending(context.Background(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || permissionID(visible[0]) != "req-3" {
		t.Fatalf("sensitive permissions must remain interactive, got %#v", visible)
	}
	if len(replies) != 2 {
		t.Fatalf("sensitive permission was auto-approved: %#v", replies)
	}
}
