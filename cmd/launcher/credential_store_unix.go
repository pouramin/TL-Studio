//go:build !windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

const macCredentialService = "TL Studio Provider Credential"

type unixCredentialStore struct {
	fallback privateFileCredentialStore
}

func newProviderCredentialStore() providerCredentialStore {
	return unixCredentialStore{}
}

func (unixCredentialStore) Backend() string {
	if runtime.GOOS == "darwin" {
		return "macos-keychain"
	}
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("secret-tool"); err == nil {
			return "linux-secret-service"
		}
	}
	return "private-file"
}

func macCredentialAccount(providerID string) string {
	return "provider:" + strings.TrimSpace(providerID)
}

func (s unixCredentialStore) Put(providerID, secret string) error {
	providerID = strings.TrimSpace(providerID)
	secret = strings.TrimSpace(secret)
	if !validProviderID(providerID) || secret == "" {
		return errors.New("valid provider id and non-empty credential are required")
	}

	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("security"); err == nil {
			cmd := exec.Command(
				"security", "add-generic-password", "-U",
				"-a", macCredentialAccount(providerID),
				"-s", macCredentialService,
				"-w", secret,
			)
			if output, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("store provider credential in macOS Keychain: %w: %s", err, strings.TrimSpace(string(output)))
			}
			_ = s.fallback.Delete(providerID)
			return nil
		}
	case "linux":
		if _, err := exec.LookPath("secret-tool"); err == nil {
			cmd := exec.Command(
				"secret-tool", "store",
				"--label=TL Studio Provider Credential",
				"application", "tl-studio",
				"provider", providerID,
			)
			cmd.Stdin = strings.NewReader(secret)
			if output, err := cmd.CombinedOutput(); err == nil {
				_ = s.fallback.Delete(providerID)
				return nil
			} else {
				_ = output
			}
		}
	}

	return s.fallback.Put(providerID, secret)
}

func (s unixCredentialStore) Get(providerID string) (string, error) {
	providerID = strings.TrimSpace(providerID)
	if !validProviderID(providerID) {
		return "", errCredentialNotFound
	}

	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("security"); err == nil {
			cmd := exec.Command(
				"security", "find-generic-password",
				"-a", macCredentialAccount(providerID),
				"-s", macCredentialService,
				"-w",
			)
			output, err := cmd.Output()
			if err == nil {
				value := strings.TrimSpace(string(output))
				if value != "" {
					return value, nil
				}
			}
		}
	case "linux":
		if _, err := exec.LookPath("secret-tool"); err == nil {
			cmd := exec.Command(
				"secret-tool", "lookup",
				"application", "tl-studio",
				"provider", providerID,
			)
			output, err := cmd.Output()
			if err == nil {
				value := strings.TrimSpace(string(bytes.TrimSpace(output)))
				if value != "" {
					return value, nil
				}
			}
		}
	}

	return s.fallback.Get(providerID)
}

func (s unixCredentialStore) Delete(providerID string) error {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return nil
	}
	var platformErr error

	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("security"); err == nil {
			cmd := exec.Command(
				"security", "delete-generic-password",
				"-a", macCredentialAccount(providerID),
				"-s", macCredentialService,
			)
			if output, err := cmd.CombinedOutput(); err != nil {
				text := strings.ToLower(string(output))
				if !strings.Contains(text, "could not be found") && !strings.Contains(text, "item not found") {
					platformErr = fmt.Errorf("delete provider credential from macOS Keychain: %w", err)
				}
			}
		}
	case "linux":
		if _, err := exec.LookPath("secret-tool"); err == nil {
			cmd := exec.Command(
				"secret-tool", "clear",
				"application", "tl-studio",
				"provider", providerID,
			)
			if output, err := cmd.CombinedOutput(); err != nil {
				_ = output
			}
		}
	}

	fallbackErr := s.fallback.Delete(providerID)
	if platformErr != nil {
		return platformErr
	}
	return fallbackErr
}
