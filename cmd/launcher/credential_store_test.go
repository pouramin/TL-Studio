package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestPrivateFileCredentialStoreKeepsSecretOutOfProviderRegistry(t *testing.T) {
	t.Setenv("TL_STUDIO_STATE_DIR", t.TempDir())
	store := privateFileCredentialStore{}

	if err := store.Put("example-provider", "super-secret"); err != nil {
		t.Fatal(err)
	}
	value, err := store.Get("example-provider")
	if err != nil || value != "super-secret" {
		t.Fatalf("credential round trip mismatch: value=%q err=%v", value, err)
	}

	info, err := os.Stat(providerCredentialPath("example-provider"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("fallback credential file is too permissive: %o", info.Mode().Perm())
	}
	raw, err := os.ReadFile(providerCredentialPath("example-provider"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "super-secret") {
		t.Fatal("fallback credential file contains the plaintext secret")
	}

	if err := store.Delete("example-provider"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("example-provider"); !errors.Is(err, errCredentialNotFound) {
		t.Fatalf("deleted credential unexpectedly remained: %v", err)
	}
}
