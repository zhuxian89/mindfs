package binding

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"time"

	"mindfs-cloud/internal/store"
)

var (
	ErrAuthRequired = errors.New("auth required")
	ErrAccessDenied = errors.New("access denied")
)

type AdminAuth struct {
	store      store.Store
	username   string
	password   string
	sessionTTL time.Duration
	now        func() time.Time
}

func NewAdminAuth(st store.Store, username, password string, sessionTTL time.Duration) *AdminAuth {
	return &AdminAuth{
		store:      st,
		username:   username,
		password:   password,
		sessionTTL: sessionTTL,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

func (a *AdminAuth) Login(ctx context.Context, username, password string) (sessionToken, csrfToken string, err error) {
	expectedUser := sha256.Sum256([]byte(a.username))
	providedUser := sha256.Sum256([]byte(username))
	expectedPassword := sha256.Sum256([]byte(a.password))
	providedPassword := sha256.Sum256([]byte(password))
	if subtle.ConstantTimeCompare(expectedUser[:], providedUser[:]) != 1 ||
		subtle.ConstantTimeCompare(expectedPassword[:], providedPassword[:]) != 1 {
		return "", "", ErrAuthRequired
	}
	sessionToken, err = randomToken("as_", 32)
	if err != nil {
		return "", "", err
	}
	csrfToken, err = randomToken("csrf_", 32)
	if err != nil {
		return "", "", err
	}
	now := a.now()
	if err := a.store.SaveAdminSession(ctx, store.AdminSession{
		SessionHash: HashToken(sessionToken),
		CSRFHash:    HashToken(csrfToken),
		ExpiresAt:   now.Add(a.sessionTTL),
		CreatedAt:   now,
		LastSeenAt:  now,
	}); err != nil {
		return "", "", err
	}
	return sessionToken, csrfToken, nil
}

func (a *AdminAuth) Authenticate(ctx context.Context, sessionToken string) (store.AdminSession, error) {
	if sessionToken == "" {
		return store.AdminSession{}, ErrAuthRequired
	}
	session, err := a.store.GetAdminSession(ctx, HashToken(sessionToken), a.now())
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrExpired) {
		return store.AdminSession{}, ErrAuthRequired
	}
	return session, err
}

func (a *AdminAuth) AuthorizeCSRF(session store.AdminSession, csrfToken string) error {
	if csrfToken == "" || subtle.ConstantTimeCompare(session.CSRFHash, HashToken(csrfToken)) != 1 {
		return ErrAccessDenied
	}
	return nil
}

func randomToken(prefix string, size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}
