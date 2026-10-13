package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMessageRenderLifecycleRegressions(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not available; message-render lifecycle regressions skipped")
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve test file path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	command := exec.Command(node, filepath.Join(repoRoot, "scripts", "check-message-render-lifecycle.cjs"))
	command.Dir = repoRoot
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("message-render lifecycle regression failed: %v\n%s", err, output)
	}
}
