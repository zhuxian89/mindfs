package store

import (
	"context"
	"time"
)

func (s *SQLiteStore) DeleteExpired(ctx context.Context, now time.Time) error {
	nowMillis := toMillis(now)
	if _, err := s.db.ExecContext(ctx, "DELETE FROM user_sessions WHERE expires_at <= ?", nowMillis); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(
		ctx,
		"DELETE FROM email_verification_codes WHERE expires_at <= ? OR consumed_at IS NOT NULL",
		nowMillis,
	); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(
		ctx,
		"DELETE FROM auth_rate_limits WHERE window_started_at < ?",
		toMillis(now.Add(-2*time.Hour)),
	); err != nil {
		return err
	}
	const query = "UPDATE bind_challenges SET status = ? " +
		"WHERE expires_at <= ? AND status IN (?, ?)"
	_, err := s.db.ExecContext(ctx, query, BindExpired, nowMillis, BindPending, BindConfirmed)
	return err
}
