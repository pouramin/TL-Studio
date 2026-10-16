package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type crashAfterToolModel struct{ calls int }

func (m *crashAfterToolModel) Complete(_ context.Context, _ nativeModelRequest, _ func(string)) (nativeModelResponse, error) {
	m.calls++
	if m.calls == 1 {
		return nativeModelResponse{ToolCalls: []nativeModelToolCall{{ID: "one", Name: "terminal.command", Arguments: []byte(`{"command":"echo ONCE>>side-effect.txt"}`)}}}, nil
	}
	fmt.Println("TOOL_RECORDED")
	select {}
}

func TestSQLiteAgentCrashDoesNotReplayTool(t *testing.T) {
	if root := os.Getenv("TL_AGENT_CRASH_CHILD"); root != "" {
		store := newSessionPersistenceStore(filepath.Join(root, "sessions"), "test")
		session, err := store.createNativeSession(root, sessionCreateInput{})
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("SESSION " + session.ID)
		processes := newProcessManager(func() string { return root })
		runtime := newNativeAgentRuntime(nativeTestResolver{provider: tlProviderDefinition{ID: "test", Protocol: "openai-compatible"}, model: tlProviderModel{ID: "fixture", ToolCall: true}}, &crashAfterToolModel{}, newNativeToolExecutor(processes, nativeAllowAuthorizer{}), store, newLiveEventBus())
		if err = runtime.Start(root, session.ID, sessionRunInput{Text: "Create the requested side effect", Model: &sessionModelRef{ProviderID: "test", ID: "fixture"}}); err != nil {
			t.Fatal(err)
		}
		select {}
	}
	t.Setenv("TL_STUDIO_SQLITE_SESSIONS", "1")
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSQLiteAgentCrashDoesNotReplayTool$")
	cmd.Env = append(os.Environ(), "TL_AGENT_CRASH_CHILD="+root)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(pipe)
		id := ""
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "SESSION ") {
				id = strings.TrimPrefix(line, "SESSION ")
			}
			if line == "TOOL_RECORDED" {
				ready <- id
				return
			}
		}
		ready <- ""
	}()
	var id string
	select {
	case id = <-ready:
		if id == "" {
			t.Fatal("child failed before tool result")
		}
	case <-time.After(45 * time.Second):
		t.Fatal("tool child timed out")
	}
	cmd.Process.Kill()
	cmd.Wait()
	store := newSessionPersistenceStore(filepath.Join(root, "sessions"), "test")
	msgs, ok, err := store.getMessages(id)
	if err != nil || !ok {
		t.Fatal("recovery failed", err)
	}
	recovered := 0
	for _, m := range msgs {
		if m.Error != nil && m.Error.Type == "interrupted" {
			recovered++
		}
	}
	if recovered != 1 {
		t.Fatalf("expected one interruption record: %+v", msgs)
	}
	store.Close()
	store = newSessionPersistenceStore(filepath.Join(root, "sessions"), "test")
	defer store.Close()
	msgs, _, err = store.getMessages(id)
	if err != nil {
		t.Fatal(err)
	}
	again := 0
	for _, m := range msgs {
		if m.Error != nil && m.Error.Type == "interrupted" {
			again++
		}
	}
	if again != 1 {
		t.Fatal("restart repeated recovery message")
	}
	data, err := os.ReadFile(filepath.Join(root, "side-effect.txt"))
	if err != nil || strings.Count(string(data), "ONCE") != 1 {
		t.Fatalf("tool was replayed or lost: %q %v", data, err)
	}
}
