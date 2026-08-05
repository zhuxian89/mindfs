package ops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mindfs-cloud/internal/config"
	"mindfs-cloud/internal/store"
)

func TestBackupCreatesVerifiedSnapshotAndDoesNotOverwrite(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir()}
	database, err := store.OpenSQLite(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	email := "backup@qq.com"
	codeHash := sha256.Sum256([]byte("verification-code"))
	if err := database.SaveVerificationCode(context.Background(), store.VerificationCode{
		Email:             email,
		Purpose:           "register",
		Nonce:             []byte("backup-nonce-123"),
		CodeHash:          codeHash[:],
		SourceHash:        []byte("source"),
		ExpiresAt:         now.Add(time.Minute),
		ResendAvailableAt: now.Add(time.Minute),
		AttemptsRemaining: 5,
		CreatedAt:         now,
	}, now); err != nil {
		t.Fatal(err)
	}
	sessionHash := sha256.Sum256([]byte("session"))
	if _, err := database.RegisterUser(context.Background(), store.User{
		ID:           "usr_backup",
		Email:        email,
		PasswordHash: "$argon2id$test",
		CreatedAt:    now,
	}, codeHash[:], store.UserSession{
		SessionHash: sessionHash[:],
		UserID:      "usr_backup",
		ExpiresAt:   now.Add(time.Hour),
		CreatedAt:   now,
		LastSeenAt:  now,
	}, now); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "nested", "backup.db")
	result, err := Backup(context.Background(), cfg, destination)
	if err != nil {
		t.Fatal(err)
	}
	if result.Destination != destination || result.SizeBytes <= 0 {
		t.Fatalf("backup result = %#v", result)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode = %v", info.Mode().Perm())
	}
	backup, err := sql.Open("sqlite", destination)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := backup.QueryRow("SELECT COUNT(*) FROM user_sessions").Scan(&count); err != nil {
		t.Fatal(err)
	}
	_ = backup.Close()
	if count != 1 {
		t.Fatalf("backup user session count = %d", count)
	}
	before, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Backup(context.Background(), cfg, destination); err == nil {
		t.Fatal("backup overwrote existing destination")
	}
	after, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("existing backup changed after rejected overwrite")
	}
	_ = database.Close()
}
