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
	database, err := openSQLiteConnections(source, true)
	if err != nil {
		return 0, fmt.Errorf("open source database: %w", err)
	}
	defer database.Close()

	// Keep partial snapshots private; publish only a verified database, and
	// never replace a destination another backup created in the meantime.
	temp, err := os.CreateTemp(filepath.Dir(destination), ".relay-backup-*")
	if err != nil {
		return 0, err
	}
	stage := temp.Name()
	defer os.Remove(stage)
	if err := temp.Close(); err != nil {
		return 0, err
	}
	if _, err := database.ExecContext(ctx, "VACUUM INTO ?", stage); err != nil {
		return 0, fmt.Errorf("create sqlite backup: %w", err)
	}
	if err := verifySQLiteBackup(ctx, stage); err != nil {
		return 0, err
	}
	info, err := os.Stat(stage)
	if err != nil {
		return 0, err
	}
	if err := os.Link(stage, destination); err != nil {
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
	// An empty SQLite database also passes integrity_check.
	if _, err := database.ExecContext(ctx, "SELECT id FROM nodes LIMIT 0"); err != nil {
		return fmt.Errorf("verify relay schema: %w", err)
	}
	return nil
}
