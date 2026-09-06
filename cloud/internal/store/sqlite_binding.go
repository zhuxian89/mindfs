package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	maxBindChallenges             = 10000
	maxNewBindChallengesPerMinute = 120
	bindChallengeRetention        = 24 * time.Hour
)

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
		if err := checkNewBindChallengeLimits(ctx, tx, now); err != nil {
			return BindChallenge{}, false, err
		}
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

func checkNewBindChallengeLimits(ctx context.Context, tx *sql.Tx, now time.Time) error {
	var total int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM (SELECT 1 FROM bind_challenges LIMIT ?)", maxBindChallenges).Scan(&total); err != nil {
		return err
	}
	if total >= maxBindChallenges {
		return ErrRateLimited
	}
	var recent int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM bind_challenges WHERE created_at >= ?", toMillis(now.Add(-time.Minute))).Scan(&recent); err != nil {
		return err
	}
	if recent >= maxNewBindChallengesPerMinute {
		return ErrRateLimited
	}
	return nil
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
		if challenge.ClaimedByUserID != node.OwnerUserID {
			return BindChallenge{}, Node{}, ErrClaimed
		}
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
	if node.OwnerUserID == "" {
		return BindChallenge{}, Node{}, ErrConflict
	}

	const insertNode = "INSERT INTO nodes " +
		"(id, device_id, owner_user_id, name, status, access_mode, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)"
	if _, err := tx.ExecContext(
		ctx,
		insertNode,
		node.ID,
		node.DeviceID,
		node.OwnerUserID,
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
	const updateChallenge = "UPDATE bind_challenges SET status = ?, node_id = ?, claimed_by_user_id = ?, confirmed_at = ? WHERE code_hash = ?"
	if _, err := tx.ExecContext(
		ctx,
		updateChallenge,
		BindConfirmed,
		node.ID,
		node.OwnerUserID,
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
	challenge.ClaimedByUserID = node.OwnerUserID
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

func getChallenge(ctx context.Context, q queryer, codeHash []byte) (BindChallenge, error) {
	const query = "SELECT code_hash, device_id, claimed_by_user_id, requested_node_name, root_hint, status, node_id, " +
		"token_derivation_version, expires_at, created_at, confirmed_at " +
		"FROM bind_challenges WHERE code_hash = ?"
	var challenge BindChallenge
	var status string
	var expiresAt, createdAt int64
	var confirmedAt sql.NullInt64
	err := q.QueryRowContext(ctx, query, codeHash).Scan(
		&challenge.CodeHash,
		&challenge.DeviceID,
		&challenge.ClaimedByUserID,
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
