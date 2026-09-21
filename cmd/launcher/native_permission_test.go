package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestNativePermissionUsesProjectScopedRememberedRule(t *testing.T) {
	project := t.TempDir()
	engine := &permissionEngine{
		state:         &appState{project: project},
		store:         newPermissionPolicyStore(filepath.Join(t.TempDir(), "permissions.json")),
		nativePending: map[string]*nativePermissionWaiter{},
	}
	descriptor, ok := toolDescriptorForID("files.write")
	if !ok {
		t.Fatal("files.write descriptor missing")
	}
	input := map[string]any{"path": "notes.txt", "content": "hello"}

	done := make(chan error, 1)
	go func() {
		done <- engine.AuthorizeNativeTool(context.Background(), "session-1", project, descriptor, input)
	}()

	var request map[string]any
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending := engine.nativePendingSnapshot("session-1")
		if len(pending) == 1 {
			request = pending[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if request == nil {
		t.Fatal("native write permission request was not exposed")
	}
	requestID := permissionID(request)
	if request["project"] != project {
		t.Fatalf("permission request lost project scope: %#v", request)
	}
	result, handled, err := engine.replyNativePermission(requestID, "session-1", "always")
	if err != nil || !handled {
		t.Fatalf("reply failed: handled=%v result=%#v err=%v", handled, result, err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("native permission waiter did not resume")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := engine.AuthorizeNativeTool(ctx, "session-2", project, descriptor, input); err != nil {
		t.Fatalf("remembered project rule should auto-allow matching write: %v", err)
	}
}

func TestNativePermissionDoesNotRememberTerminalExecution(t *testing.T) {
	project := t.TempDir()
	engine := &permissionEngine{
		state:         &appState{project: project},
		store:         newPermissionPolicyStore(filepath.Join(t.TempDir(), "permissions.json")),
		nativePending: map[string]*nativePermissionWaiter{},
	}
	descriptor, ok := toolDescriptorForID("terminal.command")
	if !ok {
		t.Fatal("terminal.command descriptor missing")
	}
	done := make(chan error, 1)
	go func() {
		done <- engine.AuthorizeNativeTool(context.Background(), "session-shell", project, descriptor, map[string]any{"command": "echo ok"})
	}()

	var request map[string]any
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending := engine.nativePendingSnapshot("session-shell")
		if len(pending) == 1 {
			request = pending[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if request == nil {
		t.Fatal("terminal permission request was not exposed")
	}
	if _, handled, err := engine.replyNativePermission(permissionID(request), "session-shell", "always"); !handled || err == nil {
		t.Fatalf("sensitive terminal permission must reject always: handled=%v err=%v", handled, err)
	}
	if _, handled, err := engine.replyNativePermission(permissionID(request), "session-shell", "once"); !handled || err != nil {
		t.Fatalf("one-time terminal approval failed: handled=%v err=%v", handled, err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("terminal permission waiter did not resume")
	}
}
