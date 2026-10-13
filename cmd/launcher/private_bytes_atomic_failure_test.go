package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateBytesAtomicWritePreservesPriorCredentialOnFailedReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.bin")
	old := []byte{0, 1, 2, 0xff, 0x34}
	replacement := []byte{0x22, 0xa1, 0x8f, 0x00}
	if err := os.WriteFile(path, old, 0o600); err != nil { t.Fatal(err) }
	failure := errors.New("injected replacement failure")
	err := writePrivateBytesAtomicWithIO(path, replacement, privateJSONAtomicIO{
		syncFile: (*os.File).Sync,
		rename: func(_, _ string) error { return failure },
	})
	if !errors.Is(err, failure) { t.Fatalf("expected rename failure: %v", err) }
	stored, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(stored, old) {
		t.Fatalf("credential blob was overwritten after failed rename: %x %v", stored, err)
	}
	if err := writePrivateBytesAtomic(path,replacement); err != nil { t.Fatal(err) }
	stored, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(stored,replacement) {
		t.Fatalf("credential blob was not replaced atomically: %x %v",stored,err)
	}
}
