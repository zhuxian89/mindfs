package store

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
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
	return s, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *SQLiteStore) ObserveChallenge(
	ctx context.Context,
	codeHash []byte,
	deviceID string,
	now time.Time,
	expiresAt time.Time,
) (BindChallenge, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BindChallenge{}, false, err
	}
	defer tx.Rollback()

	challenge, err := getChallenge(ctx, tx, codeHash)
	if errors.Is(err, ErrNotFound) {
		challenge = BindChallenge{
			CodeHash:               bytes.Clone(codeHash),
			DeviceID:               deviceID,
			Status:                 BindPending,
			TokenDerivationVersion: 1,
			ExpiresAt:              expiresAt,
			CreatedAt:              now,
		}
		const insert = "INSERT INTO bind_challenges " +
			"(code_hash, device_id, status, token_derivation_version, expires_at, created_at) " +
			"VALUES (?, ?, ?, ?, ?, ?)"
		_, err = tx.ExecContext(
			ctx,
			insert,
			challenge.CodeHash,
			challenge.DeviceID,
			challenge.Status,
			challenge.TokenDerivationVersion,
			toMillis(challenge.ExpiresAt),
			toMillis(challenge.CreatedAt),
		)
		if err != nil {
			return BindChallenge{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return BindChallenge{}, false, err
		}
		return challenge, false, nil
	}
	if err != nil {
		return BindChallenge{}, false, err
	}
	if challenge.DeviceID != deviceID {
		return challenge, true, nil
	}
	if now.After(challenge.ExpiresAt) && challenge.Status != BindClaimed && challenge.Status != BindRevoked {
		if _, err := tx.ExecContext(
			ctx,
			"UPDATE bind_challenges SET status = ? WHERE code_hash = ?",
			BindExpired,
			codeHash,
		); err != nil {
			return BindChallenge{}, false, err
		}
		challenge.Status = BindExpired
	}
	if err := tx.Commit(); err != nil {
		return BindChallenge{}, false, err
	}
	return challenge, false, nil
}

func (s *SQLiteStore) GetChallenge(ctx context.Context, codeHash []byte) (BindChallenge, error) {
	return getChallenge(ctx, s.db, codeHash)
}

func (s *SQLiteStore) ConfirmChallenge(
	ctx context.Context,
	codeHash []byte,
	node Node,
	token DeviceTokenRecord,
	now time.Time,
) (BindChallenge, Node, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BindChallenge{}, Node{}, err
	}
	defer tx.Rollback()

	challenge, err := getChallenge(ctx, tx, codeHash)
	if err != nil {
		return BindChallenge{}, Node{}, err
	}
	switch challenge.Status {
	case BindConfirmed:
		existing, err := getNode(ctx, tx, challenge.NodeID)
		return challenge, existing, err
	case BindClaimed:
		return BindChallenge{}, Node{}, ErrClaimed
	case BindExpired:
		return BindChallenge{}, Node{}, ErrExpired
	case BindRevoked:
		return BindChallenge{}, Node{}, ErrConflict
	case BindPending:
	default:
		return BindChallenge{}, Node{}, ErrConflict
	}
	if now.After(challenge.ExpiresAt) {
		if _, err := tx.ExecContext(
			ctx,
			"UPDATE bind_challenges SET status = ? WHERE code_hash = ?",
			BindExpired,
			codeHash,
		); err != nil {
			return BindChallenge{}, Node{}, err
		}
		if err := tx.Commit(); err != nil {
			return BindChallenge{}, Node{}, err
		}
		return BindChallenge{}, Node{}, ErrExpired
	}

	const insertNode = "INSERT INTO nodes " +
		"(id, device_id, name, status, access_mode, created_at) VALUES (?, ?, ?, ?, ?, ?)"
	if _, err := tx.ExecContext(
		ctx,
		insertNode,
		node.ID,
		node.DeviceID,
		node.Name,
		node.Status,
		node.AccessMode,
		toMillis(node.CreatedAt),
	); err != nil {
		return BindChallenge{}, Node{}, err
	}
	const insertToken = "INSERT INTO device_tokens " +
		"(id, node_id, token_hash, status, created_at) VALUES (?, ?, ?, ?, ?)"
	if _, err := tx.ExecContext(
		ctx,
		insertToken,
		token.ID,
		token.NodeID,
		token.TokenHash,
		token.Status,
		toMillis(token.CreatedAt),
	); err != nil {
		return BindChallenge{}, Node{}, err
	}
	const updateChallenge = "UPDATE bind_challenges SET status = ?, node_id = ?, confirmed_at = ? WHERE code_hash = ?"
	if _, err := tx.ExecContext(
		ctx,
		updateChallenge,
		BindConfirmed,
		node.ID,
		toMillis(now),
		codeHash,
	); err != nil {
		return BindChallenge{}, Node{}, err
	}
	if err := tx.Commit(); err != nil {
		return BindChallenge{}, Node{}, err
	}

	challenge.Status = BindConfirmed
	challenge.NodeID = node.ID
	challenge.ConfirmedAt = &now
	return challenge, node, nil
}

func (s *SQLiteStore) RevokeChallenge(ctx context.Context, codeHash []byte) error {
	const query = "UPDATE bind_challenges SET status = ? WHERE code_hash = ? AND status = ?"
	result, err := s.db.ExecContext(ctx, query, BindRevoked, codeHash, BindPending)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrConflict
	}
	return nil
}

func (s *SQLiteStore) GetNode(ctx context.Context, nodeID string) (Node, error) {
	return getNode(ctx, s.db, nodeID)
}

func (s *SQLiteStore) AuthenticateDeviceToken(ctx context.Context, tokenHash []byte, now time.Time) (Node, error) {
	const query = "SELECT n.id, n.device_id, n.name, n.status, n.access_mode, n.created_at, n.last_seen_at " +
		"FROM device_tokens t JOIN nodes n ON n.id = t.node_id " +
		"WHERE t.token_hash = ? AND t.status = 'active' AND n.status = 'active'"
	var node Node
	var createdAt int64
	var lastSeenAt sql.NullInt64
	err := s.db.QueryRowContext(ctx, query, tokenHash).Scan(
		&node.ID,
		&node.DeviceID,
		&node.Name,
		&node.Status,
		&node.AccessMode,
		&createdAt,
		&lastSeenAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrNotFound
	}
	if err != nil {
		return Node{}, err
	}
	node.CreatedAt = fromMillis(createdAt)
	node.LastSeenAt = nullableTime(lastSeenAt)

	nowMillis := toMillis(now)
	if _, err := s.db.ExecContext(
		ctx,
		"UPDATE device_tokens SET last_used_at = ? WHERE token_hash = ?",
		nowMillis,
		tokenHash,
	); err != nil {
		return Node{}, err
	}
	if _, err := s.db.ExecContext(
		ctx,
		"UPDATE nodes SET last_seen_at = ? WHERE id = ?",
		nowMillis,
		node.ID,
	); err != nil {
		return Node{}, err
	}
	node.LastSeenAt = &now
	return node, nil
}

func (s *SQLiteStore) SaveAdminSession(ctx context.Context, session AdminSession) error {
	const query = "INSERT INTO admin_sessions " +
		"(session_hash, csrf_hash, expires_at, created_at, last_seen_at) VALUES (?, ?, ?, ?, ?)"
	_, err := s.db.ExecContext(
		ctx,
		query,
		session.SessionHash,
		session.CSRFHash,
		toMillis(session.ExpiresAt),
		toMillis(session.CreatedAt),
		toMillis(session.LastSeenAt),
	)
	return err
}

func (s *SQLiteStore) GetAdminSession(ctx context.Context, sessionHash []byte, now time.Time) (AdminSession, error) {
	const query = "SELECT session_hash, csrf_hash, expires_at, created_at, last_seen_at " +
		"FROM admin_sessions WHERE session_hash = ?"
	var session AdminSession
	var expiresAt, createdAt, lastSeenAt int64
	err := s.db.QueryRowContext(ctx, query, sessionHash).Scan(
		&session.SessionHash,
		&session.CSRFHash,
		&expiresAt,
		&createdAt,
		&lastSeenAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminSession{}, ErrNotFound
	}
	if err != nil {
		return AdminSession{}, err
	}
	session.ExpiresAt = fromMillis(expiresAt)
	session.CreatedAt = fromMillis(createdAt)
	session.LastSeenAt = fromMillis(lastSeenAt)
	if !now.Before(session.ExpiresAt) {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM admin_sessions WHERE session_hash = ?", sessionHash)
		return AdminSession{}, ErrExpired
	}
	if _, err := s.db.ExecContext(
		ctx,
		"UPDATE admin_sessions SET last_seen_at = ? WHERE session_hash = ?",
		toMillis(now),
		sessionHash,
	); err != nil {
		return AdminSession{}, err
	}
	session.LastSeenAt = now
	return session, nil
}

func (s *SQLiteStore) DeleteExpired(ctx context.Context, now time.Time) error {
	nowMillis := toMillis(now)
	if _, err := s.db.ExecContext(
		ctx,
		"DELETE FROM admin_sessions WHERE expires_at <= ?",
		nowMillis,
	); err != nil {
		return err
	}
	const query = "UPDATE bind_challenges SET status = ? " +
		"WHERE expires_at <= ? AND status IN (?, ?)"
	_, err := s.db.ExecContext(ctx, query, BindExpired, nowMillis, BindPending, BindConfirmed)
	return err
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getChallenge(ctx context.Context, q queryer, codeHash []byte) (BindChallenge, error) {
	const query = "SELECT code_hash, device_id, requested_node_name, root_hint, status, node_id, " +
		"token_derivation_version, expires_at, created_at, confirmed_at " +
		"FROM bind_challenges WHERE code_hash = ?"
	var challenge BindChallenge
	var status string
	var expiresAt, createdAt int64
	var confirmedAt sql.NullInt64
	err := q.QueryRowContext(ctx, query, codeHash).Scan(
		&challenge.CodeHash,
		&challenge.DeviceID,
		&challenge.RequestedNodeName,
		&challenge.RootHint,
		&status,
		&challenge.NodeID,
		&challenge.TokenDerivationVersion,
		&expiresAt,
		&createdAt,
		&confirmedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return BindChallenge{}, ErrNotFound
	}
	if err != nil {
		return BindChallenge{}, err
	}
	challenge.Status = BindStatus(status)
	challenge.ExpiresAt = fromMillis(expiresAt)
	challenge.CreatedAt = fromMillis(createdAt)
	challenge.ConfirmedAt = nullableTime(confirmedAt)
	return challenge, nil
}

func getNode(ctx context.Context, q queryer, nodeID string) (Node, error) {
	const query = "SELECT id, device_id, name, status, access_mode, created_at, last_seen_at " +
		"FROM nodes WHERE id = ?"
	var node Node
	var createdAt int64
	var lastSeenAt sql.NullInt64
	err := q.QueryRowContext(ctx, query, nodeID).Scan(
		&node.ID,
		&node.DeviceID,
		&node.Name,
		&node.Status,
		&node.AccessMode,
		&createdAt,
		&lastSeenAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrNotFound
	}
	if err != nil {
		return Node{}, err
	}
	node.CreatedAt = fromMillis(createdAt)
	node.LastSeenAt = nullableTime(lastSeenAt)
	return node, nil
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
