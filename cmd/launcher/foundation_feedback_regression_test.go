package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFoundationFeedbackDeleteRequiresMatchingRevision(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, "to-delete.txt")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := nativeDeleteFile(project, map[string]any{"path": "to-delete.txt"}); err == nil {
		t.Fatal("delete without read revision was allowed")
	}
	if _, _, err := nativeDeleteFile(project, map[string]any{"path": "to-delete.txt", "expectedSha256": fileSHA256([]byte("stale"))}); !errors.Is(err, errWorkspaceFileConflict) {
		t.Fatalf("stale delete must reject: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "old" {
		t.Fatalf("unsafe delete mutated file: %q %v", got, err)
	}
	output, changes, err := nativeDeleteFile(project, map[string]any{"path": "to-delete.txt", "expectedSha256": fileSHA256([]byte("old"))})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("deletion should be recorded as workspace change: %#v", changes)
	}
	if output.(map[string]any)["deleted"] != true {
		t.Fatalf("deleted flag missing: %#v", output)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file still exists: %v", err)
	}
}
func TestFoundationFeedbackDeleteRejectsFoldersAndTraversal(t *testing.T) {
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := nativeDeleteFile(project, map[string]any{"path": "folder", "expectedSha256": fileSHA256(nil)}); err == nil {
		t.Fatal("directory deletion allowed")
	}
	if _, _, err := nativeDeleteFile(project, map[string]any{"path": "../escape.txt", "expectedSha256": fileSHA256(nil)}); err == nil {
		t.Fatal("path escape allowed")
	}
	if _, _, err := nativeDeleteFile(project, map[string]any{"path": ".git/config", "expectedSha256": fileSHA256(nil)}); err == nil {
		t.Fatal("git metadata deletion allowed")
	}
}
func TestFoundationFeedbackDeleteIsAdvertisedAndPermissionGated(t *testing.T) {
	exe := false
	for _, id := range nativeExecutableToolIDs() {
		if id == "files.delete" {
			exe = true
		}
	}
	if !exe {
		t.Fatal("delete executable is not listed")
	}
	d, ok := toolDescriptorForID("files.delete")
	if !ok || !d.Capabilities.Write || d.PermissionClass != "write" {
		t.Fatalf("unsafe file delete descriptor: %#v", d)
	}
	schema := nativeToolInputSchema("files.delete")
	required := schema["required"].([]string)
	if len(required) != 2 || required[1] != "expectedSha256" {
		t.Fatalf("missing revision schema: %#v", schema)
	}
}
func TestFoundationFeedbackQuotaCooldownResetsOnlyOnCredentialChange(t *testing.T) {
	router := newLayaRouterService(nil, nil)
	router.observeCredential("openrouter", "account-A")
	selection := nativeRouteSelection{ProviderID: "openrouter", ModelID: "model:free", Group: "free"}
	if !router.markRouteFailure(selection, errors.New("status 429: openrouter_free_tier_daily free-models-per-day")) {
		t.Fatal("expected quota recognition")
	}
	if _, ok := router.candidateCooldown("openrouter", "other:free", "free"); !ok {
		t.Fatal("quota cooldown did not cover free models")
	}
	router.observeCredential("openrouter", "account-A")
	if _, ok := router.candidateCooldown("openrouter", "other:free", "free"); !ok {
		t.Fatal("same account bypassed rate limit")
	}
	router.observeCredential("openrouter", "account-B")
	if _, ok := router.candidateCooldown("openrouter", "other:free", "free"); ok {
		t.Fatal("new API key retained old account's cooldown")
	}
}
func TestFoundationFeedbackProvider429DoesNotLeakAccountMetadata(t *testing.T) {
	response := &http.Response{
		StatusCode: 429,
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"Rate limit exceeded: free-models-per-day","metadata":{"limit_source":"openrouter_free_tier_daily"}},"user_id":"private-test-id"}`)),
	}
	err := modelHTTPError(response, nativeModelRequest{})
	if err == nil || !strings.Contains(err.Error(), "free-models-per-day") {
		t.Fatalf("quota not classified: %v", err)
	}
	if strings.Contains(err.Error(), "private-test-id") {
		t.Fatalf("provider metadata leaked: %v", err)
	}
}
func TestFoundationFeedbackRunScopedFilePermissions(t *testing.T) {
	project := t.TempDir()
	engine := &permissionEngine{state: &appState{project: project}, store: newPermissionPolicyStore(filepath.Join(t.TempDir(), "policy.json")), nativePending: map[string]*nativePermissionWaiter{}}
	write, _ := toolDescriptorForID("files.write")
	edit, _ := toolDescriptorForID("files.edit")
	deletion, _ := toolDescriptorForID("files.delete")
	done := make(chan error, 1)
	go func() {
		done <- engine.AuthorizeNativeTool(context.Background(), "run-A", project, write, map[string]any{"path": "a.txt", "content": "first"})
	}()
	var pending map[string]any
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		items := engine.nativePendingSnapshot("run-A")
		if len(items) > 0 {
			pending = items[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pending == nil {
		t.Fatal("first write permission did not appear")
	}
	_, handled, err := engine.replyNativePermission(permissionID(pending), "run-A", "task")
	if !handled || err != nil {
		t.Fatalf("run approval failed: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for _, d := range []toolDescriptor{write, edit, deletion} {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		err := engine.AuthorizeNativeTool(ctx, "run-A", project, d, map[string]any{"path": "other.txt"})
		cancel()
		if err != nil {
			t.Fatalf("approved file group prompted again for %s: %v", d.ID, err)
		}
	}
	engine.clearNativeRunGrants("run-A")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if err := engine.AuthorizeNativeTool(ctx, "run-A", project, edit, map[string]any{"path": "fresh.txt"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("run scope leaked beyond completion: %v", err)
	}
}
func TestFoundationFeedbackReadOnlyIntentStillBlocksMutation(t *testing.T) {
	for _, text := range []string{"Review my project, do not modify any files", "فقط بررسی کن و تغییری نده"} {
		if nativePromptRequiresExecution(text) {
			t.Fatalf("read-only request unexpectedly classified as write: %q", text)
		}
	}
	for _, text := range []string{"Redesign my website", "Please finish the redesign", "No, the redesign didn't happen. Continue and fix it"} {
		if !nativePromptRequiresExecution(text) {
			t.Fatalf("follow-up edit request was classified read-only: %q", text)
		}
	}
}
