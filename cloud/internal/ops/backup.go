package ops

import (
	"context"
	"path/filepath"

	"mindfs-cloud/internal/config"
	"mindfs-cloud/internal/store"
)

type BackupResult struct {
	Source      string
	Destination string
	SizeBytes   int64
}

func Backup(ctx context.Context, cfg config.Config, destination string) (BackupResult, error) {
	database, err := store.OpenSQLite(cfg.DataDir)
	if err != nil {
		return BackupResult{}, err
	}
	if err := database.Close(); err != nil {
		return BackupResult{}, err
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return BackupResult{}, err
	}
	size, err := store.BackupSQLite(ctx, cfg.DataDir, absolute)
	if err != nil {
		return BackupResult{}, err
	}
	return BackupResult{
		Source:      filepath.Join(cfg.DataDir, "mindfs-cloud.db"),
		Destination: absolute,
		SizeBytes:   size,
	}, nil
}
