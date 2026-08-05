package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRequiresTokenKey(t *testing.T) {
	t.Setenv("MINDFS_CLOUD_PUBLIC_URL", "http://localhost:8080")
	t.Setenv("MINDFS_CLOUD_DATA_DIR", t.TempDir())
	t.Setenv("MINDFS_CLOUD_TOKEN_KEY", "")
	setAssetsDir(t)
	setSMTPEnv(t)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "MINDFS_CLOUD_TOKEN_KEY") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsMalformedTokenKeys(t *testing.T) {
	for _, value := range []string{
		base64.RawURLEncoding.EncodeToString(make([]byte, 31)),
		base64.URLEncoding.EncodeToString(make([]byte, 32)),
		"not+base64url",
	} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("MINDFS_CLOUD_PUBLIC_URL", "http://localhost:8080")
			t.Setenv("MINDFS_CLOUD_DATA_DIR", t.TempDir())
			t.Setenv("MINDFS_CLOUD_TOKEN_KEY", value)
			setAssetsDir(t)
			setSMTPEnv(t)
			if _, err := Load(); err == nil {
				t.Fatal("Load() accepted malformed token key")
			}
		})
	}
}

func TestLoadAcceptsValidConfig(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	t.Setenv("MINDFS_CLOUD_PUBLIC_URL", "https://relay.example.com/")
	t.Setenv("MINDFS_CLOUD_DATA_DIR", t.TempDir())
	t.Setenv("MINDFS_CLOUD_TOKEN_KEY", base64.RawURLEncoding.EncodeToString(key))
	assetsDir := setAssetsDir(t)
	setSMTPEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.PublicURL.String() != "https://relay.example.com" {
		t.Fatalf("PublicURL = %q", cfg.PublicURL)
	}
	if cfg.MaxWSMessageBytes != 32<<20 {
		t.Fatalf("MaxWSMessageBytes = %d", cfg.MaxWSMessageBytes)
	}
	if cfg.AssetsDir != assetsDir {
		t.Fatalf("AssetsDir = %q want %q", cfg.AssetsDir, assetsDir)
	}
	if cfg.BootstrapEmail != "sender@qq.com" || cfg.SMTP.Host != "smtp.qq.com" {
		t.Fatalf("SMTP config = %#v bootstrap=%q", cfg.SMTP, cfg.BootstrapEmail)
	}
}

func TestLoadRejectsPublicURLCredentials(t *testing.T) {
	t.Setenv("MINDFS_CLOUD_PUBLIC_URL", "https://user:password@relay.example.com")
	t.Setenv("MINDFS_CLOUD_DATA_DIR", t.TempDir())
	t.Setenv("MINDFS_CLOUD_TOKEN_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	setAssetsDir(t)
	setSMTPEnv(t)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "must not contain credentials") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsIncompleteAssetsDir(t *testing.T) {
	t.Setenv("MINDFS_CLOUD_PUBLIC_URL", "http://localhost:8080")
	t.Setenv("MINDFS_CLOUD_DATA_DIR", t.TempDir())
	t.Setenv("MINDFS_CLOUD_TOKEN_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("MINDFS_CLOUD_ASSETS_DIR", t.TempDir())
	setSMTPEnv(t)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "missing index.html") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsInvalidQQSMTP(t *testing.T) {
	key := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("MINDFS_CLOUD_PUBLIC_URL", "http://localhost:8080")
	t.Setenv("MINDFS_CLOUD_DATA_DIR", t.TempDir())
	t.Setenv("MINDFS_CLOUD_TOKEN_KEY", key)
	setAssetsDir(t)
	setSMTPEnv(t)
	t.Setenv("MINDFS_CLOUD_SMTP_HOST", "smtp.example.com")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "QQ SMTP") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestCheckAssetsDirValidatesIndexReferences(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	index := `<script type="module" src="./assets/index-current.js"></script><link rel="stylesheet" href="/mindfs-assets/index-current.css">`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "index-current.js"), []byte("js"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckAssetsDir(dir); err == nil || !strings.Contains(err.Error(), "index-current.css") {
		t.Fatalf("CheckAssetsDir() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "index-current.css"), []byte("css"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckAssetsDir(dir); err != nil {
		t.Fatalf("CheckAssetsDir() error = %v", err)
	}
}

func setAssetsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MINDFS_CLOUD_ASSETS_DIR", dir)
	return dir
}

func setSMTPEnv(t *testing.T) {
	t.Helper()
	t.Setenv("MINDFS_CLOUD_SMTP_HOST", "smtp.qq.com")
	t.Setenv("MINDFS_CLOUD_SMTP_PORT", "465")
	t.Setenv("MINDFS_CLOUD_SMTP_TLS", "true")
	t.Setenv("MINDFS_CLOUD_SMTP_FROM", "sender@qq.com")
	t.Setenv("MINDFS_CLOUD_SMTP_USERNAME", "sender@qq.com")
	t.Setenv("MINDFS_CLOUD_SMTP_PASSWORD", "smtp-secret")
}
