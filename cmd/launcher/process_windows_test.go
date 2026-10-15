//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestOwnedCommandImmediateClose(t *testing.T) {
	cwd := t.TempDir()
	for i := 0; i < 20; i++ {
		manager := newProcessManager(func() string { return cwd })
		child, err := manager.start("ping -n 31 127.0.0.1 > nul")
		if err != nil {
			t.Fatal(err)
		}
		manager.Close()
		select {
		case <-child.done:
		default:
			t.Fatalf("iteration %d: command survived Close", i)
		}
	}
}

func TestOwnedCommandEndsDescendantAfterParentExit(t *testing.T) {
	cwd := t.TempDir()
	pidPath := filepath.Join(cwd, "child.pid")
	t.Setenv("TL_STUDIO_COMMAND_CHILD_PID", pidPath)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestOwnedCommandParentHelper$")
	cmd.Dir = cwd
	// Use writers so os/exec must drain inherited output pipes after parent exit.
	output := &managedProcess{}
	cmd.Stdout = processOutputWriter{process: output}
	cmd.Stderr = processOutputWriter{process: output}
	terminate, release, err := startOwnedCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	defer terminate()
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		release()
		close(done)
	}()
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(pidPath)
		pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		if pid > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatalf("descendant did not start: %s", output.snapshot().Output)
	}
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(process)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("exited shell retained inherited output pipes")
	}
	status, err := windows.WaitForSingleObject(process, 5000)
	if err != nil || status != windows.WAIT_OBJECT_0 {
		t.Fatalf("descendant survived shell exit: wait=%d err=%v", status, err)
	}
}

func TestOwnedCommandParentHelper(t *testing.T) {
	if os.Getenv("TL_STUDIO_COMMAND_CHILD_PID") == "" {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestOwnedCommandChildHelper$")
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestOwnedCommandChildHelper(t *testing.T) {
	pidPath := os.Getenv("TL_STUDIO_COMMAND_CHILD_PID")
	if pidPath == "" {
		return
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Second)
}
