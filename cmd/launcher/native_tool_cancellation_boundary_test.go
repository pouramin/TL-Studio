package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type cancelAfterToolApproval struct { cancel context.CancelFunc }

func (a cancelAfterToolApproval) AuthorizeNativeTool(_ context.Context, _ string, _ string, _ toolDescriptor, _ map[string]any) error {
	a.cancel()
	return nil
}

func TestNativeToolExecutorDoesNotStartMutationAfterApprovalCancellation(t *testing.T) {
	project := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	executor := newNativeToolExecutor(newProcessManager(func() string {return project}), cancelAfterToolApproval{cancel:cancel})
	output := executor.Execute(ctx, "session-cancelled", project, nativeToolCall{
		ID:"files.write",
		CallID:"cancel-test",
		Arguments:json.RawMessage(`{"path":"must-not-exist.txt","content":"never write this"}`),
	})
	if !strings.Contains(output.Error,context.Canceled.Error()) {
		t.Fatalf("mutation was not canceled after permission approval: %#v",output)
	}
	if _, err := os.Stat(filepath.Join(project,"must-not-exist.txt")); !os.IsNotExist(err) {
		t.Fatalf("filesystem mutation began after cancellation: %v",err)
	}
}

func TestNativeToolExecutorDoesNotStartMutationWithAlreadyCancelledContext(t *testing.T) {
	project := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	executor := newNativeToolExecutor(newProcessManager(func() string {return project}), nil)
	output := executor.Execute(ctx, "session-cancelled", project, nativeToolCall{
		ID:"files.write",
		CallID:"cancel-test",
		Arguments:json.RawMessage(`{"path":"must-not-exist.txt","content":"never write this"}`),
	})
	if !strings.Contains(output.Error,context.Canceled.Error()) {
		t.Fatalf("previously canceled mutation was not blocked: %#v",output)
	}
	if _, err := os.Stat(filepath.Join(project,"must-not-exist.txt")); !os.IsNotExist(err) {
		t.Fatalf("previously canceled context mutated workspace: %v",err)
	}
}
