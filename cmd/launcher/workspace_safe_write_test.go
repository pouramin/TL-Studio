package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestSafeWorkspaceAgentWriteRequiresReadBeforeOverwrite(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, "note.txt")
	if err := os.WriteFile(path, []byte("initial"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, _, err := nativeWriteFile(project, map[string]any{"path": "note.txt", "content": "unverified"}); err == nil || !strings.Contains(err.Error(), "expectedSha256") {
		t.Fatalf("overwrite without read revision was allowed: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "initial" {
		t.Fatalf("unsafe overwrite changed original: %q", got)
	}
	if _, _, err := nativeWriteFile(project, map[string]any{"path": "note.txt", "content": "updated", "expectedSha256": fileSHA256([]byte("initial"))}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "updated" {
		t.Fatalf("update did not commit: %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat replaced file: %v", err)
	}
	// POSIX modes are not represented identically on Windows/NTFS.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
		t.Fatalf("replacement did not preserve file mode: %v", info.Mode())
	}
	if _, _, err := nativeWriteFile(project, map[string]any{"path": "note.txt", "content": "stale", "expectedSha256": fileSHA256([]byte("initial"))}); !errors.Is(err, errWorkspaceFileConflict) {
		t.Fatalf("stale write should be rejected as conflict: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "updated" {
		t.Fatalf("stale write clobbered latest contents: %q", got)
	}
}

func TestSafeWorkspaceAgentEditRejectsStaleAndMissingHashes(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, "note.txt")
	if err := os.WriteFile(path, []byte("the quick fox"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := nativeEditFile(project, map[string]any{"path": "note.txt", "old": "quick", "new": "slow"}); err == nil {
		t.Fatal("edit without a read revision must not proceed")
	}
	previous := fileSHA256([]byte("the quick fox"))
	if _, _, err := nativeEditFile(project, map[string]any{"path": "note.txt", "old": "quick", "new": "slow", "expectedSha256": previous}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "the slow fox" {
		t.Fatalf("unexpected edited file: %q", got)
	}
	if _, _, err := nativeEditFile(project, map[string]any{"path": "note.txt", "old": "slow", "new": "fast", "expectedSha256": previous}); !errors.Is(err, errWorkspaceFileConflict) {
		t.Fatalf("stale edit must report conflict: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "the slow fox" {
		t.Fatalf("stale edit mutated file: %q", got)
	}
}

func TestSafeWorkspaceAtomicCreationDoesNotReplaceConcurrentFile(t *testing.T) {
	project := t.TempDir()
	target := filepath.Join(project, "new.txt")
	if err := os.WriteFile(target, []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := atomicWorkspaceFileWrite(target, []byte("agent"), 0o600, true, ""); !errors.Is(err, errWorkspaceFileConflict) {
		t.Fatalf("createOnly should refuse a concurrent destination: %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "external" {
		t.Fatalf("concurrent destination overwritten: %q", got)
	}
	names, err := filepath.Glob(filepath.Join(project, ".tl-studio-write-*"))
	if err != nil || len(names) != 0 {
		t.Fatalf("staging files leaked after conflict: %q %v", names, err)
	}
}

func TestSafeWorkspaceAgentCreatesNewFileWithoutRevision(t *testing.T) {
	project := t.TempDir()
	out, _, err := nativeWriteFile(project, map[string]any{"path": "created.txt", "content": "from agent"})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := out.(map[string]any)
	if !ok || result["created"] != true {
		t.Fatalf("unexpected creation metadata: %#v", out)
	}
	if got, err := os.ReadFile(filepath.Join(project, "created.txt")); err != nil || string(got) != "from agent" {
		t.Fatalf("new file missing or wrong: %q %v", got, err)
	}
}

func TestSafeWorkspaceAgentRejectsSymlinkDestination(t *testing.T) {
	project := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project, "linked.txt")); err != nil {
		t.Skipf("symlinks not available: %v", err)
	}
	if _, _, err := nativeWriteFile(project, map[string]any{"path": "linked.txt", "content": "overwrite", "expectedSha256": fileSHA256([]byte("outside"))}); err == nil {
		t.Fatal("symlink write was allowed")
	}
	if got, _ := os.ReadFile(outside); string(got) != "outside" {
		t.Fatalf("symlink escaped project: %q", got)
	}
}

func TestSafeWorkspaceConcurrentAgentUpdatesOneWinner(t *testing.T) {
	project := t.TempDir()
	file := filepath.Join(project, "concurrent.txt")
	if err := os.WriteFile(file, []byte("base"), 0o600); err != nil {
		t.Fatal(err)
	}
	sha := fileSHA256([]byte("base"))
	const writers = 12
	var wg sync.WaitGroup
	results := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, err := nativeWriteFile(project, map[string]any{"path": "concurrent.txt", "content": strings.Repeat("x", i+1), "expectedSha256": sha})
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, errWorkspaceFileConflict) {
			conflicts++
		} else {
			t.Fatalf("unexpected concurrent writer error: %v", err)
		}
	}
	if successes != 1 || conflicts != writers-1 {
		t.Fatalf("stale overwrite protection failed: successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestSafeWorkspaceRejectsMalformedExpectedHash(t *testing.T) {
	project := t.TempDir()
	file := filepath.Join(project, "malformed.txt")
	if err := os.WriteFile(file, []byte("base"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := nativeWriteFile(project, map[string]any{"path": "malformed.txt", "content": "change", "expectedSha256": "xyz"}); err == nil {
		t.Fatal("invalid revision token should fail")
	}
	if got, _ := os.ReadFile(file); string(got) != "base" {
		t.Fatalf("invalid token changed file: %q", got)
	}
}

func TestSafeWorkspaceToolSchemaDescribesExpectedRevisions(t *testing.T) {
	for _, id := range []string{"files.write", "files.edit"} {
		schema := nativeToolInputSchema(id)
		properties, ok := schema["properties"].(map[string]any)
		if !ok || properties["expectedSha256"] == nil {
			t.Fatalf("%s does not expose expectedSha256 for model", id)
		}
	}
}
