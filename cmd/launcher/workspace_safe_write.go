package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Serializes writes through TL Studio's built-in Editor and Agent. External
// programs do not use this mutex; callers must also provide an expected hash.
var workspaceFileMutationMu sync.Mutex

var errWorkspaceFileConflict = errors.New("file changed on disk since it was read")

func validateWorkspaceExpectedSHA(expected string, previous []byte, exists bool, required bool) error {
	expected = strings.TrimSpace(expected)
	if !exists {
		if expected != "" {
			return fmt.Errorf("%w: file no longer exists; read the project again", errWorkspaceFileConflict)
		}
		return nil
	}
	if required && expected == "" {
		return errors.New("expectedSha256 is required when changing an existing file; read it with files.read first")
	}
	if expected == "" {
		return nil
	}
	if len(expected) != 64 {
		return errors.New("expectedSha256 must be a 64-character SHA-256 digest from files.read")
	}
	for _, ch := range expected {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return errors.New("expectedSha256 must be hexadecimal")
		}
	}
	if !strings.EqualFold(expected, fileSHA256(previous)) {
		return fmt.Errorf("%w: inspect the latest file content before retrying", errWorkspaceFileConflict)
	}
	return nil
}

// Write a complete replacement into a sibling temporary file before changing
// the destination. Existing files are replaced with rename, so a write failure
// cannot leave a truncated original. New files use a no-clobber hard link.
// File-system rename/link atomicity is scoped to the target volume; this is not
// a transaction with arbitrary external processes.
func atomicWorkspaceFileWrite(target string, data []byte, mode os.FileMode, createOnly bool, expectedSHA string) (retErr error) {
	parent := filepath.Dir(target)
	staged, err := os.CreateTemp(parent, ".tl-studio-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	defer staged.Close()
	if err := staged.Chmod(mode); err != nil {
		return err
	}
	if _, err := staged.Write(data); err != nil {
		return err
	}
	if err := staged.Sync(); err != nil {
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}
	if createOnly {
		if err := os.Link(staged.Name(), target); err != nil {
			if errors.Is(err, os.ErrExist) {
				return fmt.Errorf("%w: another process created the file first", errWorkspaceFileConflict)
			}
			return err
		}
		return nil
	}
	if expectedSHA != "" {
		latestInfo, err := os.Lstat(target)
		if err != nil || !latestInfo.Mode().IsRegular() || latestInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: destination changed before commit", errWorkspaceFileConflict)
		}
		latest, err := os.ReadFile(target)
		if err != nil {
			return err
		}
		if !strings.EqualFold(fileSHA256(latest), expectedSHA) {
			return fmt.Errorf("%w: destination changed before commit", errWorkspaceFileConflict)
		}
	}
	return os.Rename(staged.Name(), target)
}
