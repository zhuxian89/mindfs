package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

type SQLiteStore struct {
	db *sql.DB
}

func OpenSQLite(dataDir string) (*SQLiteStore, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "mindfs-cloud.db"))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &SQLiteStore{db: db}
	if err := s.Ping(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.ExecContext(context.Background(), schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate sqlite: %w", err)
	}
	if err := s.migrateColumns(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate sqlite columns: %w", err)
	}
	return s, nil
}

func (s *SQLiteStore) migrateColumns(ctx context.Context) error {
	for _, migration := range []struct {
		table  string
		column string
		query  string
	}{
		{table: "nodes", column: "owner_user_id", query: "ALTER TABLE nodes ADD COLUMN owner_user_id TEXT NOT NULL DEFAULT ''"},
		{table: "bind_challenges", column: "claimed_by_user_id", query: "ALTER TABLE bind_challenges ADD COLUMN claimed_by_user_id TEXT NOT NULL DEFAULT ''"},
		{table: "email_verification_codes", column: "nonce", query: "ALTER TABLE email_verification_codes ADD COLUMN nonce BLOB NOT NULL DEFAULT X''"},
	} {
		exists, err := s.columnExists(ctx, migration.table, migration.column)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := s.db.ExecContext(ctx, migration.query); err != nil {
				return err
			}
		}
	}
	_, err := s.db.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_nodes_owner_user_id ON nodes(owner_user_id)")
	return err
}

func (s *SQLiteStore) columnExists(ctx context.Context, table, column string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *SQLiteStore) ClaimOwnerlessNodes(ctx context.Context, email string, now time.Time) (User, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, false, err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM nodes WHERE owner_user_id = ''").Scan(&count); err != nil {
		return User{}, false, err
	}
	if count == 0 {
		return User{}, false, nil
	}
	user, err := getUserByEmail(ctx, tx, email)
	if errors.Is(err, ErrNotFound) {
		sum := sha256.Sum256([]byte(email))
		user = User{
			ID:        "usr_bootstrap_" + hex.EncodeToString(sum[:8]),
			Email:     email,
			Status:    "pending_verification",
			CreatedAt: now,
		}
		if _, err := tx.ExecContext(
			ctx,
			"INSERT INTO users (id, email, password_hash, status, created_at) VALUES (?, ?, '', ?, ?)",
			user.ID,
			user.Email,
			user.Status,
			toMillis(user.CreatedAt),
		); err != nil {
			return User{}, false, err
		}
	} else if err != nil {
		return User{}, false, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE nodes SET owner_user_id = ? WHERE owner_user_id = ''", user.ID); err != nil {
		return User{}, false, err
	}
	if _, err := tx.ExecContext(
		ctx,
		"UPDATE bind_challenges SET claimed_by_user_id = ? WHERE claimed_by_user_id = '' AND node_id IN (SELECT id FROM nodes WHERE owner_user_id = ?)",
		user.ID,
		user.ID,
	); err != nil {
		return User{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, false, err
	}
	return user, true, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func toMillis(value time.Time) int64 {
	return value.UTC().UnixMilli()
}

func fromMillis(value int64) time.Time {
	return time.UnixMilli(value).UTC()
}

func nullableTime(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed := fromMillis(value.Int64)
	return &parsed
}
