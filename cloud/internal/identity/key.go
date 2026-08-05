package identity

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const identityKeySize = 32

func LoadOrCreateKey(dataDir string) ([identityKeySize]byte, error) {
	var key [identityKeySize]byte
	path := filepath.Join(dataDir, "identity.key")
	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return createKey(path)
	}
	if err != nil {
		return key, fmt.Errorf("open identity key: %w", err)
	}
	defer file.Close()
	if _, err := io.ReadFull(file, key[:]); err != nil {
		return key, errors.New("identity key must contain exactly 32 bytes")
	}
	var extra [1]byte
	if count, _ := file.Read(extra[:]); count != 0 {
		return key, errors.New("identity key must contain exactly 32 bytes")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return key, fmt.Errorf("restrict identity key permissions: %w", err)
	}
	return key, nil
}

func createKey(path string) ([identityKeySize]byte, error) {
	var key [identityKeySize]byte
	if _, err := rand.Read(key[:]); err != nil {
		return key, fmt.Errorf("generate identity key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return LoadOrCreateKey(filepath.Dir(path))
	}
	if err != nil {
		return key, fmt.Errorf("create identity key: %w", err)
	}
	if _, err := file.Write(key[:]); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return key, fmt.Errorf("write identity key: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return key, fmt.Errorf("sync identity key: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return key, fmt.Errorf("close identity key: %w", err)
	}
	return key, nil
}
