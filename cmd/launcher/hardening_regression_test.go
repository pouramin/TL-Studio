package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReviewResponsesExactlyOneCall(t *testing.T) {
	events := []any{
		map[string]any{"type": "response.output_item.added", "item": map[string]any{"type": "function_call", "id": "fc_1", "call_id": "call_1", "name": "tl_files__write", "arguments": ""}},
		map[string]any{"type": "response.function_call_arguments.delta", "item_id": "fc_1", "delta": "{\"path\":\"x.txt\"}"},
		map[string]any{"type": "response.completed", "response": map[string]any{"output": []any{map[string]any{"type": "function_call", "id": "fc_1", "call_id": "call_1", "name": "tl_files__write", "arguments": "{\"path\":\"x.txt\"}"}}}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			b, _ := json.Marshal(e)
			io.WriteString(w, "data: "+string(b)+"\n\n")
		}
	}))
	defer server.Close()
	response, err := newNativeModelClient().Complete(context.Background(), nativeModelRequest{
		Provider: tlProviderDefinition{Protocol: "openai-responses", BaseURL: server.URL}, Model: tlProviderModel{ID: "test"}, Tools: []nativeModelToolDefinition{nativeTestToolDefinition()},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.ToolCalls) != 1 {
		t.Fatalf("one streamed tool call became %d calls: %#v", len(response.ToolCalls), response.ToolCalls)
	}
}

func TestReviewIncompleteStreamsRejected(t *testing.T) {
	for protocol, body := range map[string]string{
		"openai-compatible":  "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n",
		"openai-responses":   "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n",
		"anthropic-messages": "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n",
	} {
		t.Run(protocol, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, body)
			}))
			defer server.Close()
			response, err := newNativeModelClient().Complete(context.Background(), nativeModelRequest{Provider: tlProviderDefinition{Protocol: protocol, BaseURL: server.URL}, Model: tlProviderModel{ID: "test"}}, nil)
			if err == nil {
				t.Fatalf("unterminated stream accepted as success: text=%q finish=%q", response.Text, response.FinishReason)
			}
		})
	}
}

func TestStateDirectoryAllowsOnlyOneOwner(t *testing.T) {
	root := t.TempDir()
	first, err := acquireStateDirectoryLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := acquireStateDirectoryLock(root); err == nil {
		second.Close()
		first.Close()
		t.Fatal("two owners acquired the same state directory")
	}
	first.Close()
	next, err := acquireStateDirectoryLock(root)
	if err != nil {
		t.Fatalf("lock was not released: %v", err)
	}
	next.Close()
}

func TestReviewCookiePortIsolation(t *testing.T) {
	secret, _ := randomSecret(32)
	gate, _ := newLocalAPIGate(secret)
	control := httptest.NewServer(localOnly(gate.handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))))
	defer control.Close()
	var stolenCookie string
	preview := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stolenCookie = r.Header.Get("Cookie")
		w.WriteHeader(200)
	}))
	defer preview.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	login, _ := http.NewRequest("POST", control.URL+"/local/auth/session", nil)
	login.Header.Set("X-TL-Studio-Launch-Token", secret)
	res, err := client.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	res, err = client.Get(preview.URL)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if stolenCookie != "" {
		req, _ := http.NewRequest("POST", control.URL+"/local/permissions/example/reply", nil)
		req.Header.Set("Cookie", stolenCookie)
		req.Header.Set("X-TL-Studio-Request", "1")
		replay, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		replay.Body.Close()
		t.Fatalf("auth cookie reached another port; replay status=%d", replay.StatusCode)
	}
}

func TestReviewGitAliasProtected(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	alias := filepath.Join(root, "alias")
	if err := os.Mkdir(gitDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(gitDir, alias); err != nil {
		if runtime.GOOS != "windows" {
			t.Skip(err)
		}
		cmd := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", alias, gitDir)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("cannot create test junction: %v %s", err, output)
		}
	}
	target, rel, err := resolveProjectMutationPath(root, "alias/config")
	if err == nil {
		t.Fatalf("protected .git alias accepted: target=%s relative=%s", target, rel)
	}
}

func TestReviewAttachmentsReachConversation(t *testing.T) {
	t.Setenv("TL_STUDIO_SQLITE_SESSIONS", "")
	project := t.TempDir()
	store := newSessionPersistenceStore(t.TempDir(), "native")
	s, err := store.createNativeSession(project, sessionCreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	err = store.recordAcceptedRun(s.ID, project, sessionRunInput{Text: "Read the attached file", MessageID: "user_1", Parts: []map[string]any{{"type": "file", "filename": "test.txt", "mime": "text/plain", "url": "data:text/plain;base64,UEFZTE9BRF9TRU5USU5FTA=="}}})
	if err != nil {
		t.Fatal(err)
	}
	messages, _, err := store.getMessages(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(nativeConversationFromMessages(messages))
	if !strings.Contains(string(encoded), "PAYLOAD_SENTINEL") && !strings.Contains(string(encoded), "UEFZTE9BRF9TRU5USU5FTA==") {
		t.Fatal("accepted attachment payload is absent from model conversation")
	}
}

func TestReviewDeletedSessionStaysDeleted(t *testing.T) {
	t.Setenv("TL_STUDIO_SQLITE_SESSIONS", "")
	project := t.TempDir()
	store := newSessionPersistenceStore(t.TempDir(), "native")
	s, err := store.createNativeSession(project, sessionCreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.remove(s.ID); err != nil {
		t.Fatal(err)
	}
	if err = store.putNativeFinalMessage(s.ID, project, sessionMessageView{ID: "late_result", Role: "assistant", Text: "completed"}); err != nil {
		return
	}
	_, exists, err := store.getSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("late active-run result recreated deleted session")
	}
}

func TestReviewUnknownMCPDoesNotSelfAuthorize(t *testing.T) {
	descriptor := classifyMCPTool(pluginConfig{ID: "unreviewed", Name: "Unreviewed"}, mcpTool{Name: "read_project", Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": true}})
	if descriptor.PermissionClass == "read" {
		t.Fatal("conflicting, server-supplied hints authorize a tool as read-only")
	}
}

func TestReviewSQLiteLiveRunNotRecoveredBySecondStore(t *testing.T) {
	t.Setenv("TL_STUDIO_SQLITE_SESSIONS", "1")
	root := t.TempDir()
	project := t.TempDir()
	a := newSessionPersistenceStore(root, "native")
	b := newSessionPersistenceStore(root, "native")
	s, err := a.createNativeSession(project, sessionCreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.sqlite.Close()
	if err = a.recordAcceptedRun(s.ID, project, sessionRunInput{Text: "still running", MessageID: "live_user"}); err != nil {
		t.Fatal(err)
	}
	if _, err = b.list(0); err != nil {
		t.Fatal(err)
	}
	defer b.sqlite.Close()
	messages, _, err := a.getMessages(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range messages {
		if m.Error != nil && m.Error.Type == "interrupted" {
			t.Fatal("second store declared the first store's live run interrupted")
		}
	}
}
