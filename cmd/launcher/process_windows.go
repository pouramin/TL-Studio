//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func configureManagedCommand(cmd *exec.Cmd) {}

// Suspend the initial shell until it belongs to our job. Assigning an already
// running shell races with child creation and can leave descendants behind.
func startOwnedCommand(cmd *exec.Cmd) (func() error, func(), error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create command job: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return nil, nil, fmt.Errorf("configure command job: %w", err)
	}
	attrs := syscall.SysProcAttr{}
	if cmd.SysProcAttr != nil {
		attrs = *cmd.SysProcAttr
	}
	attrs.CreationFlags |= windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW
	attrs.HideWindow = true
	cmd.SysProcAttr = &attrs
	// A shell that exits while a descendant holds its output pipe must not keep
	// Wait blocked forever. Closing the job below also ends those descendants.
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		windows.CloseHandle(job)
		return nil, nil, err
	}
	fail := func(err error) (func() error, func(), error) {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		windows.CloseHandle(job)
		return nil, nil, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return fail(fmt.Errorf("open command process: %w", err))
	}
	err = windows.AssignProcessToJobObject(job, process)
	windows.CloseHandle(process)
	if err != nil {
		return fail(fmt.Errorf("assign command job: %w", err))
	}
	if err := resumeOwnedCommand(uint32(cmd.Process.Pid)); err != nil {
		return fail(fmt.Errorf("resume command: %w", err))
	}
	var mu sync.Mutex
	terminate := func() error {
		mu.Lock()
		defer mu.Unlock()
		if job == 0 {
			return nil
		}
		return windows.TerminateJobObject(job, 1)
	}
	release := func() {
		mu.Lock()
		defer mu.Unlock()
		if job != 0 {
			windows.CloseHandle(job)
			job = 0
		}
	}
	return terminate, release, nil
}

// os/exec closes the initial thread handle during Start. The process is still
// suspended, so its only thread can be found and resumed without a spawn race.
func resumeOwnedCommand(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{}
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return err
		}
		previous, err := windows.ResumeThread(thread)
		windows.CloseHandle(thread)
		if err != nil {
			return err
		}
		if previous == 1 {
			return nil
		}
	}
	return fmt.Errorf("suspended initial thread for process %d not found: %w", pid, err)
}

func terminateManagedProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if err := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
