package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const databaseFilename = "mindfs-cloud.db"

func BackupSQLite(ctx context.Context, dataDir, destination string) (int64, error) {
	source, err := filepath.Abs(filepath.Join(dataDir, databaseFilename))
	if err != nil {
		return 0, err
	}
	destination, err = filepath.Abs(strings.TrimSpace(destination))
	if err != nil {
		return 0, err
	}
	if destination == source {
		return 0, errors.New("backup destination must differ from source database")
	}
	if _, err := os.Stat(destination); err == nil {
		return 0, errors.New("backup destination already exists")
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return 0, fmt.Errorf("create backup directory: %w", err)
	}
	database, err := sql.Open("sqlite", source)
	if err != nil {
		return 0, err
	}
	database.SetMaxOpenConns(1)
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		return 0, fmt.Errorf("open source database: %w", err)
	}
	if _, err := database.ExecContext(ctx, "VACUUM INTO ?", destination); err != nil {
		return 0, fmt.Errorf("create sqlite backup: %w", err)
	}
	if err := verifySQLiteBackup(ctx, destination); err != nil {
		_ = os.Remove(destination)
		return 0, err
	}
	if err := os.Chmod(destination, 0o600); err != nil {
		_ = os.Remove(destination)
		return 0, fmt.Errorf("secure sqlite backup: %w", err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func verifySQLiteBackup(ctx context.Context, destination string) error {
	database, err := sql.Open("sqlite", destination)
	if err != nil {
		return err
	}
	defer database.Close()
	var result string
	if err := database.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("verify sqlite backup: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("verify sqlite backup: %s", result)
	}
	return nil
}
