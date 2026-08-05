package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (s *SQLiteStore) GetNode(ctx context.Context, nodeID string) (Node, error) {
	return getNode(ctx, s.db, nodeID)
}

func (s *SQLiteStore) ListNodesByOwner(ctx context.Context, ownerUserID string) ([]Node, error) {
	const query = "SELECT id, device_id, owner_user_id, name, status, access_mode, created_at, last_seen_at " +
		"FROM nodes WHERE owner_user_id = ?"
	rows, err := s.db.QueryContext(ctx, query, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	nodes := make([]Node, 0)
	for rows.Next() {
		var node Node
		var createdAt int64
		var lastSeenAt sql.NullInt64
		if err := rows.Scan(
			&node.ID,
			&node.DeviceID,
			&node.OwnerUserID,
			&node.Name,
			&node.Status,
			&node.AccessMode,
			&createdAt,
			&lastSeenAt,
		); err != nil {
			return nil, err
		}
		node.CreatedAt = fromMillis(createdAt)
		node.LastSeenAt = nullableTime(lastSeenAt)
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nodes, nil
}

func (s *SQLiteStore) RenameNodeByOwner(ctx context.Context, ownerUserID, nodeID, name string) (Node, error) {
	result, err := s.db.ExecContext(ctx, "UPDATE nodes SET name = ? WHERE id = ? AND owner_user_id = ?", name, nodeID, ownerUserID)
	if err != nil {
		return Node{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Node{}, err
	}
	if affected == 0 {
		return Node{}, ErrNotFound
	}
	return getOwnedNode(ctx, s.db, ownerUserID, nodeID)
}

func (s *SQLiteStore) DeleteNodeByOwner(ctx context.Context, ownerUserID, nodeID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := getOwnedNode(ctx, tx, ownerUserID, nodeID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM device_tokens WHERE node_id = ?", nodeID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM nodes WHERE id = ? AND owner_user_id = ?", nodeID, ownerUserID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (s *SQLiteStore) AuthenticateDeviceToken(ctx context.Context, tokenHash []byte, now time.Time) (Node, error) {
	const query = "SELECT n.id, n.device_id, n.owner_user_id, n.name, n.status, n.access_mode, n.created_at, n.last_seen_at " +
		"FROM device_tokens t JOIN nodes n ON n.id = t.node_id " +
		"WHERE t.token_hash = ? AND t.status = 'active' AND n.status = 'active'"
	var node Node
	var createdAt int64
	var lastSeenAt sql.NullInt64
	err := s.db.QueryRowContext(ctx, query, tokenHash).Scan(
		&node.ID,
		&node.DeviceID,
		&node.OwnerUserID,
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

func getNode(ctx context.Context, q queryer, nodeID string) (Node, error) {
	const query = "SELECT id, device_id, owner_user_id, name, status, access_mode, created_at, last_seen_at " +
		"FROM nodes WHERE id = ?"
	return scanNode(q.QueryRowContext(ctx, query, nodeID))
}

func getOwnedNode(ctx context.Context, q queryer, ownerUserID, nodeID string) (Node, error) {
	const query = "SELECT id, device_id, owner_user_id, name, status, access_mode, created_at, last_seen_at " +
		"FROM nodes WHERE id = ? AND owner_user_id = ?"
	return scanNode(q.QueryRowContext(ctx, query, nodeID, ownerUserID))
}

func scanNode(row *sql.Row) (Node, error) {
	var node Node
	var createdAt int64
	var lastSeenAt sql.NullInt64
	err := row.Scan(
		&node.ID,
		&node.DeviceID,
		&node.OwnerUserID,
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
