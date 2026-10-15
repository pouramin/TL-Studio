package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Hold an OS-owned lock for the entire application lifetime. Never delete the
// lock pathname: unlinking a locked file permits another independent inode lock.
func acquireStateDirectoryLock(directory string) (*os.File, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(directory, "instance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockStateFile(file); err != nil {
		file.Close()
		return nil, fmt.Errorf("TL Studio is already using this state directory; close the other instance or use a separate TL_STUDIO_STATE_DIR: %w", err)
	}
	return file, nil
}
