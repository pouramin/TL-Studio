package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"runtime"
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
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
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


func TestProviderCredentialSlotIDsAreSeparated(t *testing.T) {
	apiID, err := providerCredentialSlotID("example", providerCredentialSlotAPI)
	if err != nil {
		t.Fatal(err)
	}
	accountID, err := providerCredentialSlotID("example", providerCredentialSlotAccount)
	if err != nil {
		t.Fatal(err)
	}
	if apiID != "example" {
		t.Fatalf("API credential slot must preserve existing provider ID, got %q", apiID)
	}
	if accountID != "example--account" {
		t.Fatalf("unexpected account credential slot ID %q", accountID)
	}
	if apiID == accountID {
		t.Fatal("API and account credentials must never share the same vault slot")
	}
}

func TestProviderCredentialSlotsPersistIndependently(t *testing.T) {
	t.Setenv("TL_STUDIO_STATE_DIR", t.TempDir())
	store := privateFileCredentialStore{}

	if err := putProviderCredentialSlot(store, "example", providerCredentialSlotAPI, "api-secret"); err != nil {
		t.Fatal(err)
	}
	if err := putProviderCredentialSlot(store, "example", providerCredentialSlotAccount, "account-secret"); err != nil {
		t.Fatal(err)
	}

	apiSecret, err := getProviderCredentialSlot(store, "example", providerCredentialSlotAPI)
	if err != nil {
		t.Fatal(err)
	}
	accountSecret, err := getProviderCredentialSlot(store, "example", providerCredentialSlotAccount)
	if err != nil {
		t.Fatal(err)
	}
	if apiSecret != "api-secret" || accountSecret != "account-secret" {
		t.Fatalf("unexpected slot values api=%q account=%q", apiSecret, accountSecret)
	}

	if err := deleteProviderCredentialSlot(store, "example", providerCredentialSlotAccount); err != nil {
		t.Fatal(err)
	}
	if _, err := getProviderCredentialSlot(store, "example", providerCredentialSlotAccount); !errors.Is(err, errCredentialNotFound) {
		t.Fatalf("expected deleted account slot, got %v", err)
	}
	apiSecret, err = getProviderCredentialSlot(store, "example", providerCredentialSlotAPI)
	if err != nil || apiSecret != "api-secret" {
		t.Fatalf("API slot must survive account logout, value=%q err=%v", apiSecret, err)
	}
}
