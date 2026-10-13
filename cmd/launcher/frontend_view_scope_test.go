package main

import (
    "os/exec"
    "path/filepath"
    "runtime"
    "testing"
)

func TestFrontendViewScopeIsolation(t *testing.T) {
    node, err := exec.LookPath("node")
    if err != nil { t.Skip("node is not available") }
    _, current, _, ok := runtime.Caller(0)
    if !ok { t.Fatal("cannot resolve test source") }
    root := filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))
    command := exec.Command(node, filepath.Join(root, "scripts", "check-frontend-view-state.cjs"))
    command.Dir = root
    output, err := command.CombinedOutput()
    if err != nil { t.Fatalf("frontend view state test failed: %v\n%s", err, output) }
}
