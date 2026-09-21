package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var errCredentialNotFound = errors.New("credential not found")

const privateCredentialMagic = "TLSCRED1"

var privateCredentialKeyMu sync.Mutex

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

func providerCredentialDirectory() string {
	return filepath.Join(tlStudioStateDirectory(), "credentials")
}

func providerCredentialPath(providerID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(providerID)))
	return filepath.Join(providerCredentialDirectory(), hex.EncodeToString(sum[:])+".secret")
}

func privateCredentialKeyPath() string {
	return filepath.Join(providerCredentialDirectory(), "master.key")
}

type privateFileCredentialStore struct{}

func (privateFileCredentialStore) Backend() string { return "encrypted-private-file" }

func loadOrCreatePrivateCredentialKey() ([]byte, error) {
	privateCredentialKeyMu.Lock()
	defer privateCredentialKeyMu.Unlock()

	path := privateCredentialKeyPath()
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != 32 {
			return nil, errors.New("invalid TL Studio credential master key")
		}
		return data, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		if len(data) != 32 {
			return nil, errors.New("invalid TL Studio credential master key")
		}
		return data, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := file.Write(key); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return key, nil
}

func privateCredentialAEAD() (cipher.AEAD, error) {
	key, err := loadOrCreatePrivateCredentialKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func encryptPrivateCredential(secret string) ([]byte, error) {
	aead, err := privateCredentialAEAD()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	sealed := aead.Seal(nil, nonce, []byte(secret), nil)
	result := make([]byte, 0, len(privateCredentialMagic)+len(nonce)+len(sealed))
	result = append(result, []byte(privateCredentialMagic)...)
	result = append(result, nonce...)
	result = append(result, sealed...)
	return result, nil
}

func decryptPrivateCredential(data []byte) (string, error) {
	if len(data) < len(privateCredentialMagic) || string(data[:len(privateCredentialMagic)]) != privateCredentialMagic {
		return "", errors.New("invalid TL Studio credential file")
	}
	aead, err := privateCredentialAEAD()
	if err != nil {
		return "", err
	}
	payload := data[len(privateCredentialMagic):]
	if len(payload) < aead.NonceSize() {
		return "", errors.New("invalid TL Studio credential payload")
	}
	nonce := payload[:aead.NonceSize()]
	ciphertext := payload[aead.NonceSize():]
	plain, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", errors.New("could not decrypt TL Studio credential")
	}
	return strings.TrimSpace(string(plain)), nil
}

func (privateFileCredentialStore) Put(providerID, secret string) error {
	providerID = strings.TrimSpace(providerID)
	secret = strings.TrimSpace(secret)
	if !validProviderID(providerID) || secret == "" {
		return errors.New("valid provider id and non-empty credential are required")
	}
	encrypted, err := encryptPrivateCredential(secret)
	if err != nil {
		return err
	}
	path := providerCredentialPath(providerID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, encrypted, 0o600)
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
	value, err := decryptPrivateCredential(data)
	if err != nil {
		return "", err
	}
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
