//go:build windows

package sessiondb

// Windows does not expose portable directory fsync through os.File.Sync.
// Staged file bytes are flushed before publication; sudden-power-loss directory
// durability still depends on the local filesystem and hardware.
func syncDirectory(string) error { return nil }
