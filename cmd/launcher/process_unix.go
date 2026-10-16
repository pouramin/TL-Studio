//go:build !windows

package main

import (
	"os/exec"
	"syscall"
	"time"
)

func configureManagedCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func startOwnedCommand(cmd *exec.Cmd) (func() error, func(), error) {
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	return func() error { return terminateManagedProcess(cmd) }, func() {}, nil
}

func terminateManagedProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		return cmd.Process.Kill()
	}
	go func() {
		time.Sleep(2 * time.Second)
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}()
	return nil
}
