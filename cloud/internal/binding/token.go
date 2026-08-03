package binding

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"mindfs-cloud/internal/store"
)

const tokenContext = "mindfs-device-token:v1"

type DeviceTokenService interface {
	Derive(store.BindChallenge) (string, []byte, error)
	Authenticate(context.Context, string) (store.Node, error)
}

type deviceTokenService struct {
	key   [32]byte
	store store.Store
}

func NewDeviceTokenService(key [32]byte, st store.Store) DeviceTokenService {
	return &deviceTokenService{key: key, store: st}
}

func (s *deviceTokenService) Authenticate(ctx context.Context, plainToken string) (store.Node, error) {
	if s.store == nil || len(plainToken) < 4 || plainToken[:3] != "dt_" {
		return store.Node{}, store.ErrNotFound
	}
	return s.store.AuthenticateDeviceToken(ctx, HashToken(plainToken), time.Now().UTC())
}

func (s *deviceTokenService) Derive(challenge store.BindChallenge) (string, []byte, error) {
	if challenge.TokenDerivationVersion != 1 {
		return "", nil, errors.New("unsupported token derivation version")
	}
	if len(challenge.CodeHash) == 0 || challenge.DeviceID == "" || challenge.NodeID == "" {
		return "", nil, errors.New("incomplete bind challenge")
	}

	mac := hmac.New(sha256.New, s.key[:])
	_, _ = mac.Write([]byte(tokenContext))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(challenge.CodeHash)
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(challenge.DeviceID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(challenge.NodeID))
	plain := "dt_" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	hash := sha256.Sum256([]byte(plain))
	return plain, hash[:], nil
}

func HashToken(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}
