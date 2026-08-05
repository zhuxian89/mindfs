package identity

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"mindfs-cloud/internal/store"
)

type sentCode struct {
	email   string
	purpose VerificationPurpose
	code    string
}

type fakeMailSender struct {
	mu   sync.Mutex
	sent []sentCode
	fail bool
}

func (f *fakeMailSender) SendVerificationCode(_ context.Context, email string, purpose VerificationPurpose, code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("mail failed")
	}
	f.sent = append(f.sent, sentCode{email: email, purpose: purpose, code: code})
	return nil
}

func (f *fakeMailSender) latest(t *testing.T) sentCode {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		t.Fatal("no verification email sent")
	}
	return f.sent[len(f.sent)-1]
}

func TestRegistrationThenPasswordLogin(t *testing.T) {
	service, mail, database := newTestService(t)
	ctx := context.Background()
	if _, err := service.RequestRegistrationCode(ctx, "User@QQ.COM", "source-a"); err != nil {
		t.Fatal(err)
	}
	code := mail.latest(t)
	user, sessionToken, err := service.Register(ctx, "user@qq.com", "relay-password", code.code)
	if err != nil {
		t.Fatal(err)
	}
	if user.Status != "active" || sessionToken == "" || user.PasswordHash == "relay-password" {
		t.Fatalf("registered user = %#v token=%q", user, sessionToken)
	}
	if _, _, err := service.Authenticate(ctx, sessionToken); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	loggedIn, loginToken, err := service.Login(ctx, "user@qq.com", "relay-password", "source-b")
	if err != nil || loggedIn.ID != user.ID || loginToken == "" {
		t.Fatalf("Login() = %#v %q %v", loggedIn, loginToken, err)
	}
	stored, err := database.GetUserByEmail(ctx, "user@qq.com")
	if err != nil || stored.PasswordHash == "relay-password" {
		t.Fatalf("stored user = %#v err=%v", stored, err)
	}
}

func TestRegistrationRejectsNonQQBeforeMail(t *testing.T) {
	service, mail, _ := newTestService(t)
	if _, err := service.RequestRegistrationCode(context.Background(), "user@gmail.com", "source"); !errors.Is(err, ErrEmailNotAllowed) {
		t.Fatalf("RequestRegistrationCode() error = %v", err)
	}
	if len(mail.sent) != 0 {
		t.Fatalf("mail sent = %#v", mail.sent)
	}
}

func TestVerificationPurposeAndReplay(t *testing.T) {
	service, mail, _ := newTestService(t)
	ctx := context.Background()
	if _, err := service.RequestRegistrationCode(ctx, "user@qq.com", "source"); err != nil {
		t.Fatal(err)
	}
	code := mail.latest(t).code
	if err := service.ResetPassword(ctx, "user@qq.com", code, "new-password"); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("ResetPassword(register code) error = %v", err)
	}
	if _, _, err := service.Register(ctx, "user@qq.com", "relay-password", code); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Register(ctx, "user@qq.com", "another-password", code); !errors.Is(err, ErrCodeInvalid) && !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("Register(replay) error = %v", err)
	}
}

func TestVerificationCodeUsesRandomNonce(t *testing.T) {
	service, mail, database := newTestService(t)
	ctx := context.Background()
	if _, err := service.RequestRegistrationCode(ctx, "user@qq.com", "source"); err != nil {
		t.Fatal(err)
	}
	record, err := database.GetVerificationCode(ctx, "user@qq.com", string(PurposeRegister))
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Nonce) != 16 {
		t.Fatalf("nonce length = %d", len(record.Nonce))
	}
	want := service.verificationHash(PurposeRegister, "user@qq.com", mail.latest(t).code, record.Nonce)
	if !bytes.Equal(record.CodeHash, want) {
		t.Fatal("verification hash does not include stored nonce")
	}
}

func TestPasswordResetRevokesSessions(t *testing.T) {
	service, mail, _ := newTestService(t)
	ctx := context.Background()
	_, _ = service.RequestRegistrationCode(ctx, "user@qq.com", "source-register")
	_, firstSession, err := service.Register(ctx, "user@qq.com", "relay-password", mail.latest(t).code)
	if err != nil {
		t.Fatal(err)
	}
	_, secondSession, err := service.Login(ctx, "user@qq.com", "relay-password", "source-login")
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Date(2026, 8, 5, 10, 2, 0, 0, time.UTC) }
	if _, err := service.RequestPasswordResetCode(ctx, "user@qq.com", "source-reset"); err != nil {
		t.Fatal(err)
	}
	if err := service.ResetPassword(ctx, "user@qq.com", mail.latest(t).code, "new-password"); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{firstSession, secondSession} {
		if _, _, err := service.Authenticate(ctx, token); !errors.Is(err, ErrAuthRequired) {
			t.Fatalf("old session Authenticate() error = %v", err)
		}
	}
	if _, _, err := service.Login(ctx, "user@qq.com", "relay-password", "source-old"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password Login() error = %v", err)
	}
	if _, _, err := service.Login(ctx, "user@qq.com", "new-password", "source-new"); err != nil {
		t.Fatalf("new password Login() error = %v", err)
	}
}

func TestChangePasswordRotatesCurrentAndRevokesOtherSessions(t *testing.T) {
	service, mail, _ := newTestService(t)
	ctx := context.Background()
	_, _ = service.RequestRegistrationCode(ctx, "user@qq.com", "register")
	user, firstSession, err := service.Register(ctx, "user@qq.com", "relay-password", mail.latest(t).code)
	if err != nil {
		t.Fatal(err)
	}
	_, secondSession, err := service.Login(ctx, "user@qq.com", "relay-password", "login")
	if err != nil {
		t.Fatal(err)
	}
	newSession, err := service.ChangePassword(ctx, user, "relay-password", "changed-password")
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{firstSession, secondSession} {
		if _, _, err := service.Authenticate(ctx, token); !errors.Is(err, ErrAuthRequired) {
			t.Fatalf("old session Authenticate() error = %v", err)
		}
	}
	if _, _, err := service.Authenticate(ctx, newSession); err != nil {
		t.Fatalf("new session Authenticate() error = %v", err)
	}
	if _, _, err := service.Login(ctx, "user@qq.com", "changed-password", "changed"); err != nil {
		t.Fatalf("changed password Login() error = %v", err)
	}
}

func TestVerificationCodeAttemptsAndCooldown(t *testing.T) {
	service, mail, _ := newTestService(t)
	ctx := context.Background()
	if _, err := service.RequestRegistrationCode(ctx, "user@qq.com", "source"); err != nil {
		t.Fatal(err)
	}
	code := mail.latest(t).code
	if _, err := service.RequestRegistrationCode(ctx, "user@qq.com", "source"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("second RequestRegistrationCode() error = %v", err)
	}
	for index := 0; index < verificationAttempts; index++ {
		if _, _, err := service.Register(ctx, "user@qq.com", "relay-password", "000000"); !errors.Is(err, ErrCodeInvalid) {
			t.Fatalf("wrong code attempt %d error = %v", index+1, err)
		}
	}
	if _, _, err := service.Register(ctx, "user@qq.com", "relay-password", code); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("correct code after attempts error = %v", err)
	}
}

func TestConcurrentRegistrationConsumesCodeOnce(t *testing.T) {
	service, mail, _ := newTestService(t)
	ctx := context.Background()
	if _, err := service.RequestRegistrationCode(ctx, "user@qq.com", "source"); err != nil {
		t.Fatal(err)
	}
	code := mail.latest(t).code
	errorsCh := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := service.Register(ctx, "user@qq.com", "relay-password", code)
			errorsCh <- err
		}()
	}
	wg.Wait()
	close(errorsCh)
	successes := 0
	for err := range errorsCh {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, ErrCodeInvalid) && !errors.Is(err, ErrEmailTaken) {
			t.Fatalf("concurrent Register() error = %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful registrations = %d", successes)
	}
}

func TestUnknownPasswordResetDoesNotSendMail(t *testing.T) {
	service, mail, _ := newTestService(t)
	if _, err := service.RequestPasswordResetCode(context.Background(), "missing@qq.com", "source"); err != nil {
		t.Fatal(err)
	}
	if len(mail.sent) != 0 {
		t.Fatalf("mail sent = %#v", mail.sent)
	}
	if _, err := service.RequestPasswordResetCode(context.Background(), "missing@qq.com", "source"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("second missing reset request error = %v", err)
	}
}

func TestMailFailureDeletesCode(t *testing.T) {
	service, mail, database := newTestService(t)
	mail.fail = true
	if _, err := service.RequestRegistrationCode(context.Background(), "user@qq.com", "source"); !errors.Is(err, ErrMailUnavailable) {
		t.Fatalf("RequestRegistrationCode() error = %v", err)
	}
	if _, err := database.GetVerificationCode(context.Background(), "user@qq.com", string(PurposeRegister)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetVerificationCode() error = %v", err)
	}
}

func newTestService(t *testing.T) (*Service, *fakeMailSender, *store.SQLiteStore) {
	t.Helper()
	database, err := store.OpenSQLite(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	mail := &fakeMailSender{}
	hasher := NewPasswordHasherWithParams(PasswordParams{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	service, err := NewService(database, hasher, mail, [32]byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return base }
	return service, mail, database
}
