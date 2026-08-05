package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"mindfs-cloud/internal/store"
)

var (
	ErrEmailTaken         = errors.New("email taken")
	ErrCodeInvalid        = errors.New("verification code invalid")
	ErrRateLimited        = errors.New("rate limited")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrAuthRequired       = errors.New("authentication required")
	ErrMailUnavailable    = errors.New("mail unavailable")
)

const (
	verificationCodeTTL     = 10 * time.Minute
	verificationResendDelay = time.Minute
	verificationAttempts    = 5
	UserSessionTTL          = 12 * time.Hour
	hourlyEmailLimit        = 10
	hourlySourceLimit       = 30
)

type passwordHasher interface {
	Hash(string) (string, error)
	Verify(string, string) (bool, error)
}

type Service struct {
	store     store.Store
	passwords passwordHasher
	mail      MailSender
	key       [32]byte
	now       func() time.Time
	dummyHash string
}

func NewService(st store.Store, passwords passwordHasher, mail MailSender, key [32]byte) (*Service, error) {
	dummyHash, err := passwords.Hash("mindfs-invalid-password")
	if err != nil {
		return nil, err
	}
	return &Service{
		store:     st,
		passwords: passwords,
		mail:      mail,
		key:       key,
		now:       func() time.Time { return time.Now().UTC() },
		dummyHash: dummyHash,
	}, nil
}

func (s *Service) RequestRegistrationCode(ctx context.Context, email, source string) (int, error) {
	email, err := NormalizeQQEmail(email)
	if err != nil {
		return 0, ErrEmailNotAllowed
	}
	if user, err := s.store.GetUserByEmail(ctx, email); err == nil && user.Status == "active" {
		return 0, ErrEmailTaken
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return 0, err
	}
	return s.requestCode(ctx, email, source, PurposeRegister)
}

func (s *Service) Register(ctx context.Context, email, password, code string) (store.User, string, error) {
	email, err := NormalizeQQEmail(email)
	if err != nil {
		return store.User{}, "", ErrEmailNotAllowed
	}
	if err := ValidatePassword(password); err != nil {
		return store.User{}, "", ErrInvalidPassword
	}
	codeHash, err := s.validCodeHash(ctx, email, PurposeRegister, code)
	if err != nil {
		return store.User{}, "", err
	}
	passwordHash, err := s.passwords.Hash(password)
	if err != nil {
		return store.User{}, "", err
	}
	now := s.now()
	sessionToken, session, err := newSession(now)
	if err != nil {
		return store.User{}, "", err
	}
	userID, err := randomToken("usr_", 18)
	if err != nil {
		return store.User{}, "", err
	}
	user, err := s.store.RegisterUser(ctx, store.User{
		ID:           userID,
		Email:        email,
		PasswordHash: passwordHash,
		CreatedAt:    now,
	}, codeHash, session, now)
	if errors.Is(err, store.ErrConflict) {
		return store.User{}, "", ErrEmailTaken
	}
	if errors.Is(err, store.ErrCodeInvalid) {
		return store.User{}, "", ErrCodeInvalid
	}
	if err != nil {
		return store.User{}, "", err
	}
	return user, sessionToken, nil
}

func (s *Service) Login(ctx context.Context, email, password, source string) (store.User, string, error) {
	normalized, normalizeErr := NormalizeQQEmail(email)
	rateEmail := strings.ToLower(strings.TrimSpace(email))
	if normalizeErr == nil {
		rateEmail = normalized
	}
	if err := s.takeLimits(ctx, "login", rateEmail, source); err != nil {
		return store.User{}, "", err
	}
	user, lookupErr := s.store.GetUserByEmail(ctx, normalized)
	encoded := s.dummyHash
	if lookupErr == nil && user.Status == "active" {
		encoded = user.PasswordHash
	}
	matched, verifyErr := s.passwords.Verify(encoded, password)
	if verifyErr != nil {
		return store.User{}, "", verifyErr
	}
	if lookupErr != nil && !errors.Is(lookupErr, store.ErrNotFound) {
		return store.User{}, "", lookupErr
	}
	if normalizeErr != nil || lookupErr != nil || user.Status != "active" || !matched {
		return store.User{}, "", ErrInvalidCredentials
	}
	now := s.now()
	sessionToken, session, err := newSession(now)
	if err != nil {
		return store.User{}, "", err
	}
	user, err = s.store.CreateUserSession(ctx, normalized, session, now)
	if err != nil {
		return store.User{}, "", err
	}
	return user, sessionToken, nil
}

func (s *Service) RequestPasswordResetCode(ctx context.Context, email, source string) (int, error) {
	email, err := NormalizeQQEmail(email)
	if err != nil {
		return 0, ErrEmailNotAllowed
	}
	if err := s.takeLimits(ctx, "password-reset", email, source); err != nil {
		return 0, err
	}
	user, err := s.store.GetUserByEmail(ctx, email)
	if errors.Is(err, store.ErrNotFound) || (err == nil && user.Status != "active") {
		return s.issueUnsentCode(ctx, email, source, PurposePasswordReset)
	}
	if err != nil {
		return 0, err
	}
	return s.issueCode(ctx, email, source, PurposePasswordReset)
}

func (s *Service) ResetPassword(ctx context.Context, email, code, newPassword string) error {
	email, err := NormalizeQQEmail(email)
	if err != nil {
		return ErrCodeInvalid
	}
	if err := ValidatePassword(newPassword); err != nil {
		return ErrInvalidPassword
	}
	codeHash, err := s.validCodeHash(ctx, email, PurposePasswordReset, code)
	if err != nil {
		return err
	}
	passwordHash, err := s.passwords.Hash(newPassword)
	if err != nil {
		return err
	}
	if err := s.store.ResetUserPassword(ctx, email, codeHash, passwordHash, s.now()); errors.Is(err, store.ErrCodeInvalid) {
		return ErrCodeInvalid
	} else {
		return err
	}
}

func (s *Service) ChangePassword(ctx context.Context, user store.User, currentPassword, newPassword string) (string, error) {
	matched, err := s.passwords.Verify(user.PasswordHash, currentPassword)
	if err != nil {
		return "", err
	}
	if !matched {
		return "", ErrInvalidCredentials
	}
	if err := ValidatePassword(newPassword); err != nil {
		return "", ErrInvalidPassword
	}
	passwordHash, err := s.passwords.Hash(newPassword)
	if err != nil {
		return "", err
	}
	now := s.now()
	sessionToken, session, err := newSession(now)
	if err != nil {
		return "", err
	}
	if err := s.store.ChangeUserPassword(ctx, user.ID, passwordHash, session, now); err != nil {
		return "", err
	}
	return sessionToken, nil
}

func (s *Service) Authenticate(ctx context.Context, sessionToken string) (store.User, store.UserSession, error) {
	if sessionToken == "" {
		return store.User{}, store.UserSession{}, ErrAuthRequired
	}
	user, session, err := s.store.GetUserBySession(ctx, hashToken(sessionToken), s.now())
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrExpired) {
		return store.User{}, store.UserSession{}, ErrAuthRequired
	}
	return user, session, err
}

func (s *Service) Logout(ctx context.Context, sessionToken string) error {
	if sessionToken == "" {
		return nil
	}
	return s.store.DeleteUserSession(ctx, hashToken(sessionToken))
}

func (s *Service) requestCode(ctx context.Context, email, source string, purpose VerificationPurpose) (int, error) {
	if err := s.takeLimits(ctx, string(purpose), email, source); err != nil {
		return 0, err
	}
	return s.issueCode(ctx, email, source, purpose)
}

func (s *Service) issueCode(ctx context.Context, email, source string, purpose VerificationPurpose) (int, error) {
	code, record, err := s.newVerificationCode(email, source, purpose)
	if err != nil {
		return 0, err
	}
	if err := s.saveVerificationCode(ctx, record); err != nil {
		return 0, err
	}
	if err := s.mail.SendVerificationCode(ctx, email, purpose, code); err != nil {
		_ = s.store.DeleteVerificationCode(ctx, email, string(purpose), record.CodeHash)
		return 0, ErrMailUnavailable
	}
	return int(verificationResendDelay.Seconds()), nil
}

func (s *Service) issueUnsentCode(ctx context.Context, email, source string, purpose VerificationPurpose) (int, error) {
	_, record, err := s.newVerificationCode(email, source, purpose)
	if err != nil {
		return 0, err
	}
	if err := s.saveVerificationCode(ctx, record); err != nil {
		return 0, err
	}
	return int(verificationResendDelay.Seconds()), nil
}

func (s *Service) newVerificationCode(email, source string, purpose VerificationPurpose) (string, store.VerificationCode, error) {
	code, err := randomVerificationCode()
	if err != nil {
		return "", store.VerificationCode{}, err
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", store.VerificationCode{}, err
	}
	now := s.now()
	codeHash := s.verificationHash(purpose, email, code, nonce)
	return code, store.VerificationCode{
		Email:             email,
		Purpose:           string(purpose),
		Nonce:             nonce,
		CodeHash:          codeHash,
		SourceHash:        s.subjectHash("source", source),
		ExpiresAt:         now.Add(verificationCodeTTL),
		ResendAvailableAt: now.Add(verificationResendDelay),
		AttemptsRemaining: verificationAttempts,
		CreatedAt:         now,
	}, nil
}

func (s *Service) saveVerificationCode(ctx context.Context, record store.VerificationCode) error {
	if err := s.store.SaveVerificationCode(ctx, record, record.CreatedAt); errors.Is(err, store.ErrRateLimited) {
		return ErrRateLimited
	} else if err != nil {
		return err
	}
	return nil
}

func (s *Service) validCodeHash(ctx context.Context, email string, purpose VerificationPurpose, code string) ([]byte, error) {
	record, err := s.store.GetVerificationCode(ctx, email, string(purpose))
	if err != nil || record.ConsumedAt != nil || !s.now().Before(record.ExpiresAt) || record.AttemptsRemaining <= 0 {
		return nil, ErrCodeInvalid
	}
	provided := s.verificationHash(purpose, email, code, record.Nonce)
	if subtle.ConstantTimeCompare(provided, record.CodeHash) != 1 {
		decrementErr := s.store.DecrementVerificationAttempts(ctx, email, string(purpose), record.CodeHash, s.now())
		if decrementErr != nil && !errors.Is(decrementErr, store.ErrCodeInvalid) {
			return nil, decrementErr
		}
		return nil, ErrCodeInvalid
	}
	return provided, nil
}

func (s *Service) takeLimits(ctx context.Context, scope, email, source string) error {
	now := s.now()
	if err := s.store.TakeRateLimit(ctx, scope+"-email", s.subjectHash("email", email), now, time.Hour, hourlyEmailLimit); errors.Is(err, store.ErrRateLimited) {
		return ErrRateLimited
	} else if err != nil {
		return err
	}
	if err := s.store.TakeRateLimit(ctx, scope+"-source", s.subjectHash("source", source), now, time.Hour, hourlySourceLimit); errors.Is(err, store.ErrRateLimited) {
		return ErrRateLimited
	} else {
		return err
	}
}

func (s *Service) verificationHash(purpose VerificationPurpose, email, code string, nonce []byte) []byte {
	return s.hmac("verification", string(purpose), email, code, base64.RawURLEncoding.EncodeToString(nonce))
}

func (s *Service) subjectHash(kind, value string) []byte {
	return s.hmac("subject", kind, value)
}

func (s *Service) hmac(parts ...string) []byte {
	mac := hmac.New(sha256.New, s.key[:])
	for _, part := range parts {
		_, _ = mac.Write([]byte(part))
		_, _ = mac.Write([]byte{0})
	}
	return mac.Sum(nil)
}

func randomVerificationCode() (string, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}

func newSession(now time.Time) (string, store.UserSession, error) {
	token, err := randomToken("us_", 32)
	if err != nil {
		return "", store.UserSession{}, err
	}
	return token, store.UserSession{
		SessionHash: hashToken(token),
		ExpiresAt:   now.Add(UserSessionTTL),
		CreatedAt:   now,
		LastSeenAt:  now,
	}, nil
}

func randomToken(prefix string, size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashToken(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}
