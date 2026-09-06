package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

type auditPausedHasher struct {
	passwordHasher
	verified chan struct{}
	resume   chan struct{}
}

func (h *auditPausedHasher) Verify(encoded, password string) (bool, error) {
	matched, err := h.passwordHasher.Verify(encoded, password)
	close(h.verified)
	<-h.resume
	return matched, err
}

func TestPasswordResetRejectsInflightOldPasswordLogin(t *testing.T) {
	service, mail, _ := newTestService(t)
	ctx := context.Background()
	if _, err := service.RequestRegistrationCode(ctx, "user@qq.com", "test-source"); err != nil {
		t.Fatal(err)
	}
	_, oldSession, err := service.Register(ctx, "user@qq.com", "old-password", mail.latest(t).code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RequestPasswordResetCode(ctx, "user@qq.com", "test-source"); err != nil {
		t.Fatal(err)
	}
	code := mail.latest(t).code
	paused := &auditPausedHasher{passwordHasher: service.passwords, verified: make(chan struct{}), resume: make(chan struct{})}
	service.passwords = paused
	type result struct {
		token string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		_, token, err := service.Login(ctx, "user@qq.com", "old-password", "login-source")
		done <- result{token, err}
	}()
	select {
	case <-paused.verified:
	case <-time.After(time.Second):
		t.Fatal("login did not reach verified barrier")
	}
	if err := service.ResetPassword(ctx, "user@qq.com", code, "new-password"); err != nil {
		close(paused.resume)
		t.Fatal(err)
	}
	if _, _, err := service.Authenticate(ctx, oldSession); err == nil {
		close(paused.resume)
		t.Fatal("reset did not revoke existing session")
	}
	close(paused.resume)
	outcome := <-done
	if !errors.Is(outcome.err, ErrInvalidCredentials) || outcome.token != "" {
		t.Fatalf("stale login accepted: error=%v", outcome.err)
	}
}

func TestChangePasswordCannotOverwriteConcurrentReset(t *testing.T) {
	service, mail, _ := newTestService(t)
	ctx := context.Background()
	if _, err := service.RequestRegistrationCode(ctx, "user@qq.com", "register"); err != nil {
		t.Fatal(err)
	}
	user, _, err := service.Register(ctx, "user@qq.com", "old-password", mail.latest(t).code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RequestPasswordResetCode(ctx, "user@qq.com", "reset"); err != nil {
		t.Fatal(err)
	}
	if err := service.ResetPassword(ctx, "user@qq.com", mail.latest(t).code, "reset-password"); err != nil {
		t.Fatal(err)
	}
	// An already authenticated request can still hold the old User snapshot.
	if _, err := service.ChangePassword(ctx, user, "old-password", "overwrite-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("stale password change accepted: %v", err)
	}
	if _, _, err := service.Login(ctx, "user@qq.com", "reset-password", "login"); err != nil {
		t.Fatalf("reset password lost: %v", err)
	}
}

type blockingBudgetHasher struct {
	passwordHasher
	entered chan struct{}
	release chan struct{}
}

func (h *blockingBudgetHasher) Verify(encoded, password string) (bool, error) {
	h.entered <- struct{}{}
	<-h.release
	return h.passwordHasher.Verify(encoded, password)
}

func TestPasswordBudgetBoundsMemoryAndReleasesSlots(t *testing.T) {
	service, _, _ := newTestService(t)
	h := &blockingBudgetHasher{passwordHasher: service.passwords, entered: make(chan struct{}, 2), release: make(chan struct{})}
	service.passwords = h
	ctx := context.Background()
	done := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := service.verifyPassword(ctx, service.dummyHash, "wrong-password")
			done <- err
		}()
	}
	for range 2 {
		select {
		case <-h.entered:
		case <-time.After(time.Second):
			close(h.release)
			t.Fatal("password operations did not start")
		}
	}
	_, verifyErr := service.verifyPassword(ctx, service.dummyHash, "wrong-password")
	_, hashErr := service.hashPassword(ctx, "new-password")
	close(h.release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if !errors.Is(verifyErr, ErrRateLimited) || !errors.Is(hashErr, ErrRateLimited) {
		t.Fatalf("budget bypass: verify=%v hash=%v", verifyErr, hashErr)
	}
	if _, err := service.hashPassword(ctx, "new-password"); err != nil {
		t.Fatalf("slot leaked: %v", err)
	}
}
