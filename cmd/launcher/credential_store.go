package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var errCredentialNotFound = errors.New("credential not found")

type providerCredentialStore interface {
	Put(providerID, secret string) error
	Get(providerID string) (string, error)
	Delete(providerID string) error
	Backend() string
}

func tlStudioStateDirectory() string {
	if dir := strings.TrimSpace(os.Getenv("TL_STUDIO_STATE_DIR")); dir != "" {
		return dir
	}
	base, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "TL Studio")
}

func providerCredentialPath(providerID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(providerID)))
	return filepath.Join(tlStudioStateDirectory(), "credentials", hex.EncodeToString(sum[:])+".secret")
}

type privateFileCredentialStore struct{}

func (privateFileCredentialStore) Backend() string { return "private-file" }

func (privateFileCredentialStore) Put(providerID, secret string) error {
	providerID = strings.TrimSpace(providerID)
	secret = strings.TrimSpace(secret)
	if !validProviderID(providerID) || secret == "" {
		return errors.New("valid provider id and non-empty credential are required")
	}
	path := providerCredentialPath(providerID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(secret), 0o600)
}

func (privateFileCredentialStore) Get(providerID string) (string, error) {
	providerID = strings.TrimSpace(providerID)
	if !validProviderID(providerID) {
		return "", errCredentialNotFound
	}
	data, err := os.ReadFile(providerCredentialPath(providerID))
	if errors.Is(err, os.ErrNotExist) {
		return "", errCredentialNotFound
	}
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", errCredentialNotFound
	}
	return value, nil
}

func (privateFileCredentialStore) Delete(providerID string) error {
	providerID = strings.TrimSpace(providerID)
	if !validProviderID(providerID) {
		return nil
	}
	err := os.Remove(providerCredentialPath(providerID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
