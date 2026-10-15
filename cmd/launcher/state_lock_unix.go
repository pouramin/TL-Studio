//go:build !windows

package main

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"syscall"
)

func lockStateFile(file *os.File) error { return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
func processOwnerAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
func workspaceReparsePoint(info os.FileInfo) bool { return false }
