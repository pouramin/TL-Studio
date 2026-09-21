//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const cryptProtectUIForbidden = 0x1

type windowsDataBlob struct {
	cbData uint32
	pbData *byte
}

var (
	crypt32DLL             = syscall.NewLazyDLL("crypt32.dll")
	kernel32DLL            = syscall.NewLazyDLL("kernel32.dll")
	cryptProtectDataProc   = crypt32DLL.NewProc("CryptProtectData")
	cryptUnprotectDataProc = crypt32DLL.NewProc("CryptUnprotectData")
	localFreeProc          = kernel32DLL.NewProc("LocalFree")
)

type windowsCredentialStore struct{}

func newProviderCredentialStore() providerCredentialStore {
	return windowsCredentialStore{}
}

func (windowsCredentialStore) Backend() string { return "windows-dpapi" }

func bytesToWindowsBlob(data []byte) windowsDataBlob {
	if len(data) == 0 {
		return windowsDataBlob{}
	}
	return windowsDataBlob{cbData: uint32(len(data)), pbData: &data[0]}
}

func copyWindowsBlob(blob windowsDataBlob) []byte {
	if blob.cbData == 0 || blob.pbData == nil {
		return nil
	}
	source := unsafe.Slice(blob.pbData, int(blob.cbData))
	result := make([]byte, len(source))
	copy(result, source)
	return result
}

func protectWindowsCredential(data []byte) ([]byte, error) {
	input := bytesToWindowsBlob(data)
	var output windowsDataBlob
	description, _ := syscall.UTF16PtrFromString("TL Studio provider credential")
	result, _, callErr := cryptProtectDataProc.Call(
		uintptr(unsafe.Pointer(&input)),
		uintptr(unsafe.Pointer(description)),
		0,
		0,
		0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&output)),
	)
	if result == 0 {
		if callErr != syscall.Errno(0) {
			return nil, callErr
		}
		return nil, errors.New("CryptProtectData failed")
	}
	defer localFreeProc.Call(uintptr(unsafe.Pointer(output.pbData)))
	return copyWindowsBlob(output), nil
}

func unprotectWindowsCredential(data []byte) ([]byte, error) {
	input := bytesToWindowsBlob(data)
	var output windowsDataBlob
	result, _, callErr := cryptUnprotectDataProc.Call(
		uintptr(unsafe.Pointer(&input)),
		0,
		0,
		0,
		0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&output)),
	)
	if result == 0 {
		if callErr != syscall.Errno(0) {
			return nil, callErr
		}
		return nil, errors.New("CryptUnprotectData failed")
	}
	defer localFreeProc.Call(uintptr(unsafe.Pointer(output.pbData)))
	return copyWindowsBlob(output), nil
}

func (windowsCredentialStore) Put(providerID, secret string) error {
	providerID = strings.TrimSpace(providerID)
	secret = strings.TrimSpace(secret)
	if !validProviderID(providerID) || secret == "" {
		return errors.New("valid provider id and non-empty credential are required")
	}
	protected, err := protectWindowsCredential([]byte(secret))
	if err != nil {
		return err
	}
	path := providerCredentialPath(providerID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, protected, 0o600)
}

func (windowsCredentialStore) Get(providerID string) (string, error) {
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
	plain, err := unprotectWindowsCredential(data)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(plain))
	if value == "" {
		return "", errCredentialNotFound
	}
	return value, nil
}

func (windowsCredentialStore) Delete(providerID string) error {
	err := os.Remove(providerCredentialPath(strings.TrimSpace(providerID)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
