package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRevealCommandForPathAcrossSupportedPlatforms(t *testing.T) {
	target := filepath.Join("C:", "workspace", "file.txt")

	windows, err := revealCommandForPath("windows", target, false)
	if err != nil {
		t.Fatal(err)
	}
	if windows.Name != "explorer.exe" || len(windows.Args) != 1 || !strings.HasPrefix(windows.Args[0], "/select,") || !strings.Contains(windows.Args[0], "file.txt") {
		t.Fatalf("unexpected Windows reveal command: %#v", windows)
	}

	mac, err := revealCommandForPath("darwin", "/workspace/file.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if mac.Name != "open" || len(mac.Args) != 2 || mac.Args[0] != "-R" || mac.Args[1] != "/workspace/file.txt" {
		t.Fatalf("unexpected macOS reveal command: %#v", mac)
	}

	linux, err := revealCommandForPath("linux", "/workspace/src/file.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if linux.Name != "xdg-open" || len(linux.Args) != 1 || linux.Args[0] != "/workspace/src" {
		t.Fatalf("unexpected Linux reveal command: %#v", linux)
	}

	if _, err := revealCommandForPath("plan9", "/workspace/file.txt", false); err == nil {
		t.Fatal("unsupported OS should fail closed")
	}
}

func TestRevealRouteIsProjectScopedAndLaunchesResolvedFile(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, "folder", "hello.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	original := launchRevealCommand
	var launched revealCommand
	launchRevealCommand = func(command revealCommand) error {
		launched = command
		return nil
	}
	t.Cleanup(func() { launchRevealCommand = original })

	state := &appState{project: project}
	mux := http.NewServeMux()
	registerLocalFileRoutes(mux, state)
	server := httptest.NewServer(mux)
	defer server.Close()

	response, err := http.Post(server.URL+"/local/reveal?path=folder%2Fhello.txt", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("reveal status=%d", response.StatusCode)
	}
	if launched.Name == "" || len(launched.Args) == 0 {
		t.Fatalf("reveal launcher was not invoked: %#v", launched)
	}
	joined := strings.Join(launched.Args, " ")
	if !strings.Contains(strings.ToLower(joined), "hello.txt") && !strings.Contains(strings.ToLower(joined), strings.ToLower(filepath.Dir(path))) {
		t.Fatalf("reveal command does not target the selected file/folder: %#v", launched)
	}

	outside := filepath.Join(filepath.Dir(project), "outside.txt")
	if err := os.WriteFile(outside, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })

	rejected, err := http.Post(server.URL+"/local/reveal?path=..%2Foutside.txt", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	rejected.Body.Close()
	if rejected.StatusCode != http.StatusBadRequest {
		t.Fatalf("path traversal reveal status=%d", rejected.StatusCode)
	}
}
