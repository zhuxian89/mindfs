package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		args    []string
		command string
		wantErr bool
	}{
		{command: "serve"},
		{args: []string{"serve"}, command: "serve"},
		{args: []string{"validate"}, command: "validate"},
		{args: []string{"migrate"}, command: "migrate"},
		{args: []string{"healthcheck"}, command: "healthcheck"},
		{args: []string{"backup", "/tmp/backup.db"}, command: "backup"},
		{args: []string{"backup"}, wantErr: true},
		{args: []string{"unknown"}, wantErr: true},
	}
	for _, test := range tests {
		command, _, err := parseCommand(test.args)
		if test.wantErr {
			if err == nil {
				t.Fatalf("parseCommand(%v) succeeded", test.args)
			}
			continue
		}
		if err != nil || command != test.command {
			t.Fatalf("parseCommand(%v) = %q, %v", test.args, command, err)
		}
	}
}

func TestValidateAndMigrateCommands(t *testing.T) {
	dataDir := setCommandConfig(t)
	var output bytes.Buffer
	if err := run([]string{"validate"}, &output); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != "configuration valid" {
		t.Fatalf("validate output = %q", output.String())
	}
	output.Reset()
	if err := run([]string{"migrate"}, &output); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(dataDir, "mindfs-cloud.db")) {
		t.Fatal("migrate did not create database")
	}
	if err := run([]string{"migrate"}, &output); err != nil {
		t.Fatalf("second migrate failed: %v", err)
	}
	output.Reset()
	backupPath := filepath.Join(t.TempDir(), "backup.db")
	if err := run([]string{"backup", backupPath}, &output); err != nil {
		t.Fatal(err)
	}
	if !fileExists(backupPath) || !strings.Contains(output.String(), "backup complete") {
		t.Fatalf("backup output = %q", output.String())
	}
}

func setCommandConfig(t *testing.T) string {
	t.Helper()
	assetsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(assetsDir, "index.html"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(assetsDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	t.Setenv("MINDFS_CLOUD_ADDR", "127.0.0.1:0")
	t.Setenv("MINDFS_CLOUD_PUBLIC_URL", "http://127.0.0.1:8080")
	t.Setenv("MINDFS_CLOUD_DATA_DIR", dataDir)
	t.Setenv("MINDFS_CLOUD_ASSETS_DIR", assetsDir)
	t.Setenv("MINDFS_CLOUD_ADMIN_USERNAME", "admin")
	t.Setenv("MINDFS_CLOUD_ADMIN_PASSWORD", "secret")
	t.Setenv("MINDFS_CLOUD_TOKEN_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	return dataDir
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
