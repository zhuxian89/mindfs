package ops

import (
	"context"
	"time"

	"mindfs-cloud/internal/config"
	"mindfs-cloud/internal/store"
)

func Migrate(cfg config.Config) error {
	database, err := store.OpenSQLite(cfg.DataDir)
	if err != nil {
		return err
	}
	if _, _, err := database.ClaimOwnerlessNodes(context.Background(), cfg.BootstrapEmail, time.Now().UTC()); err != nil {
		_ = database.Close()
		return err
	}
	return database.Close()
}
