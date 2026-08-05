package identity

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeQQEmail(t *testing.T) {
	got, err := NormalizeQQEmail("User@QQ.COM")
	if err != nil || got != "user@qq.com" {
		t.Fatalf("NormalizeQQEmail() = %q, %v", got, err)
	}
	for _, value := range []string{"user@gmail.com", "user@vip.qq.com", "Name <user@qq.com>", "@qq.com"} {
		if _, err := NormalizeQQEmail(value); err == nil {
			t.Fatalf("NormalizeQQEmail(%q) succeeded", value)
		}
	}
}

func TestLoadOrCreateKeyPersistsRestrictedKey(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadOrCreateKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first[:], second[:]) {
		t.Fatal("identity key changed across reload")
	}
	info, err := os.Stat(filepath.Join(dir, "identity.key"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("identity key mode = %o", info.Mode().Perm())
	}
}

func TestPasswordHasher(t *testing.T) {
	hasher := NewPasswordHasherWithParams(PasswordParams{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	encoded, err := hasher.Hash("relay-password")
	if err != nil {
		t.Fatal(err)
	}
	if encoded == "relay-password" {
		t.Fatal("password stored as plaintext")
	}
	matched, err := hasher.Verify(encoded, "relay-password")
	if err != nil || !matched {
		t.Fatalf("Verify(correct) = %v, %v", matched, err)
	}
	matched, err = hasher.Verify(encoded, "wrong-password")
	if err != nil || matched {
		t.Fatalf("Verify(wrong) = %v, %v", matched, err)
	}
	for _, password := range []string{"short", string(make([]byte, 129))} {
		if err := ValidatePassword(password); err == nil {
			t.Fatalf("ValidatePassword(%d chars) succeeded", len(password))
		}
	}
}

func TestNewSMTPSenderRequiresQQImplicitTLS(t *testing.T) {
	valid := SMTPConfig{Host: "smtp.qq.com", Port: 465, TLS: true, From: "sender@qq.com", Username: "sender@qq.com", Password: "secret"}
	if _, err := NewSMTPSender(valid); err != nil {
		t.Fatalf("NewSMTPSender(valid) error = %v", err)
	}
	invalid := valid
	invalid.TLS = false
	if _, err := NewSMTPSender(invalid); err == nil {
		t.Fatal("NewSMTPSender accepted plaintext SMTP")
	}
}

func TestSecretFormattingIsRedacted(t *testing.T) {
	secret := Secret("smtp-authorization-code")
	for _, formatted := range []string{fmt.Sprint(secret), fmt.Sprintf("%#v", secret)} {
		if formatted != "[REDACTED]" {
			t.Fatalf("formatted secret = %q", formatted)
		}
	}
}
