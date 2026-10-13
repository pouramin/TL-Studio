package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func newLocalFilesServer(t *testing.T, project string) *httptest.Server {
	t.Helper()
	state := &appState{project: project}
	mux := http.NewServeMux()
	registerLocalFileRoutes(mux, state)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func localJSONRequest(t *testing.T, server *httptest.Server, method, path string, body any) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, server.URL+path, reader)
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
	data, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return res, data
}

func readPreviewForTest(t *testing.T, server *httptest.Server, path string) localFilePreview {
	t.Helper()
	res, data := localJSONRequest(t, server, http.MethodGet, "/local/file?path="+url.QueryEscape(path), nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("read %q returned %d: %s", path, res.StatusCode, data)
	}
	var preview localFilePreview
	if err := json.Unmarshal(data, &preview); err != nil {
		t.Fatal(err)
	}
	return preview
}

func TestLocalFileWriteUsesOptimisticHashConflictProtection(t *testing.T) {
	project := t.TempDir()
	filePath := filepath.Join(project, "note.txt")
	if err := os.WriteFile(filePath, []byte("one\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	server := newLocalFilesServer(t, project)

	opened := readPreviewForTest(t, server, "note.txt")
	res, data := localJSONRequest(t, server, http.MethodPut, "/local/file", map[string]any{
		"path":           "note.txt",
		"content":        "two\n",
		"expectedSha256": opened.SHA256,
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("save returned %d: %s", res.StatusCode, data)
	}
	var saved localFilePreview
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Content != "two\n" || saved.SHA256 == "" || saved.SHA256 == opened.SHA256 {
		t.Fatalf("unexpected saved preview: %#v", saved)
	}

	if err := os.WriteFile(filePath, []byte("external\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	res, data = localJSONRequest(t, server, http.MethodPut, "/local/file", map[string]any{
		"path":           "note.txt",
		"content":        "stale-editor\n",
		"expectedSha256": saved.SHA256,
	})
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("stale save returned %d, want 409: %s", res.StatusCode, data)
	}
	current, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != "external\n" {
		t.Fatalf("conflicting save overwrote disk content: %q", current)
	}

	res, data = localJSONRequest(t, server, http.MethodPut, "/local/file", map[string]any{
		"path":           "note.txt",
		"content":        "forced\n",
		"expectedSha256": saved.SHA256,
		"force":          true,
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("forced save returned %d: %s", res.StatusCode, data)
	}
	current, err = os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != "forced\n" {
		t.Fatalf("forced save did not update disk: %q", current)
	}
}

// Missing or malformed revision tokens must never bypass optimistic locking.
func TestLocalFileWriteRejectsUnversionedAndMalformedSaves(t *testing.T) {
	project := t.TempDir()
	filePath := filepath.Join(project, "note.txt")
	if err := os.WriteFile(filePath, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := newLocalFilesServer(t, project)
	for _, hash := range []string{"", "not-a-sha256"} {
		res, data := localJSONRequest(t, server, http.MethodPut, "/local/file", map[string]any{
			"path": "note.txt", "content": "overwritten", "expectedSha256": hash,
		})
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("unversioned save with hash %q returned %d, want 400: %s", hash, res.StatusCode, data)
		}
		content, err := os.ReadFile(filePath)
		if err != nil || string(content) != "original" {
			t.Fatalf("failed validation mutated file: %q (%v)", content, err)
		}
	}
}

func TestLocalEntryCRUD(t *testing.T) {
	project := t.TempDir()
	server := newLocalFilesServer(t, project)

	res, data := localJSONRequest(t, server, http.MethodPost, "/local/entry", map[string]any{
		"path": "src",
		"type": "directory",
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create directory returned %d: %s", res.StatusCode, data)
	}

	res, data = localJSONRequest(t, server, http.MethodPost, "/local/entry", map[string]any{
		"path": "src/app.js",
		"type": "file",
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create file returned %d: %s", res.StatusCode, data)
	}

	res, data = localJSONRequest(t, server, http.MethodPatch, "/local/entry", map[string]any{
		"path":    "src/app.js",
		"newPath": "src/main.js",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("rename returned %d: %s", res.StatusCode, data)
	}
	if _, err := os.Stat(filepath.Join(project, "src", "main.js")); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, "src", "app.js")); !os.IsNotExist(err) {
		t.Fatalf("old file still exists after rename: %v", err)
	}

	res, data = localJSONRequest(t, server, http.MethodDelete, "/local/entry?path="+url.QueryEscape("src/main.js"), nil)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete file returned %d: %s", res.StatusCode, data)
	}

	res, data = localJSONRequest(t, server, http.MethodPost, "/local/entry", map[string]any{
		"path": "src/child.txt",
		"type": "file",
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create child returned %d: %s", res.StatusCode, data)
	}
	res, data = localJSONRequest(t, server, http.MethodDelete, "/local/entry?path=src", nil)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("non-recursive non-empty delete returned %d, want 409: %s", res.StatusCode, data)
	}
	res, data = localJSONRequest(t, server, http.MethodDelete, "/local/entry?path=src&recursive=true", nil)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("recursive delete returned %d: %s", res.StatusCode, data)
	}
	if _, err := os.Stat(filepath.Join(project, "src")); !os.IsNotExist(err) {
		t.Fatalf("directory still exists after recursive delete: %v", err)
	}
}

func TestLocalMutationRoutesRejectTraversalAndGitMetadata(t *testing.T) {
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	server := newLocalFilesServer(t, project)

	for _, path := range []string{"../outside.txt", ".git/config"} {
		res, data := localJSONRequest(t, server, http.MethodPost, "/local/entry", map[string]any{
			"path": path,
			"type": "file",
		})
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("unsafe create %q returned %d, want 400: %s", path, res.StatusCode, data)
		}
	}
}

func TestLocalMutationRejectsSymlinkParentEscape(t *testing.T) {
	project := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(project, "escape")
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink unavailable on Windows runner: %v", err)
		}
		t.Fatal(err)
	}
	server := newLocalFilesServer(t, project)
	res, data := localJSONRequest(t, server, http.MethodPost, "/local/entry", map[string]any{
		"path": "escape/new.txt",
		"type": "file",
	})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("symlink-parent escape returned %d, want 400: %s", res.StatusCode, data)
	}
	if _, err := os.Stat(filepath.Join(outside, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("unsafe mutation created outside file: %v", err)
	}
}

func TestLocalFileWriteRejectsSymlinkEntry(t *testing.T) {
	project := t.TempDir()
	target := filepath.Join(project, "target.txt")
	if err := os.WriteFile(target, []byte("safe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(project, "link.txt")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink unavailable on Windows runner: %v", err)
		}
		t.Fatal(err)
	}
	server := newLocalFilesServer(t, project)
	res, data := localJSONRequest(t, server, http.MethodPut, "/local/file", map[string]any{
		"path":    "link.txt",
		"content": "unsafe\n",
		"force":   true,
	})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("symlink write returned %d, want 400: %s", res.StatusCode, data)
	}
	current, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != "safe\n" {
		t.Fatalf("symlink write changed target: %q", current)
	}
}
