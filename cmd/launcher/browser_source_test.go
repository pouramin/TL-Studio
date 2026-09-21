package main

import (
	"os"
	"path/filepath"
	"testing"
)

func readBrowserSource(t *testing.T, name string) string {
	t.Helper()
	root := releaseRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "cmd", "launcher", "ui", name))
	if err != nil {
		t.Fatalf("read Browser source %s: %v", name, err)
	}
	return string(data)
}
