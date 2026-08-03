package binding

import (
	"bytes"
	"testing"

	"mindfs-cloud/internal/store"
)

func TestDeviceTokenDerivationIsDeterministic(t *testing.T) {
	var key [32]byte
	copy(key[:], []byte("01234567890123456789012345678901"))
	service := NewDeviceTokenService(key, nil)
	challenge := store.BindChallenge{
		CodeHash:               []byte("code-hash"),
		DeviceID:               "device-1",
		NodeID:                 "nnode1",
		TokenDerivationVersion: 1,
	}
	plain1, hash1, err := service.Derive(challenge)
	if err != nil {
		t.Fatal(err)
	}
	plain2, hash2, err := service.Derive(challenge)
	if err != nil {
		t.Fatal(err)
	}
	if plain1 != plain2 || !bytes.Equal(hash1, hash2) {
		t.Fatalf("derivation was not deterministic")
	}
	if len(plain1) < 4 || plain1[:3] != "dt_" {
		t.Fatalf("token = %q", plain1)
	}
	if !bytes.Equal(hash1, HashToken(plain1)) {
		t.Fatal("stored hash does not match token hash")
	}
}
