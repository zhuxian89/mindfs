package ops

import (
	"mindfs-cloud/internal/config"
	"mindfs-cloud/internal/store"
)

func Migrate(cfg config.Config) error {
	database, err := store.OpenSQLite(cfg.DataDir)
	if err != nil {
		return err
	}
	return database.Close()
}
