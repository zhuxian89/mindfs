package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (s *SQLiteStore) TakeRateLimit(
	ctx context.Context,
	scope string,
	subjectHash []byte,
	now time.Time,
	window time.Duration,
	limit int,
) error {
	windowStart := now.Truncate(window)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	err = tx.QueryRowContext(
		ctx,
		"SELECT count FROM auth_rate_limits WHERE scope = ? AND subject_hash = ? AND window_started_at = ?",
		scope,
		subjectHash,
		toMillis(windowStart),
	).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(
			ctx,
			"INSERT INTO auth_rate_limits (scope, subject_hash, window_started_at, count) VALUES (?, ?, ?, 1)",
			scope,
			subjectHash,
			toMillis(windowStart),
		)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	if count >= limit {
		return ErrRateLimited
	}
	if _, err := tx.ExecContext(
		ctx,
		"UPDATE auth_rate_limits SET count = count + 1 WHERE scope = ? AND subject_hash = ? AND window_started_at = ?",
		scope,
		subjectHash,
		toMillis(windowStart),
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) SaveVerificationCode(ctx context.Context, code VerificationCode, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var resendAt int64
	err = tx.QueryRowContext(
		ctx,
		"SELECT resend_available_at FROM email_verification_codes WHERE email = ? AND purpose = ?",
		code.Email,
		code.Purpose,
	).Scan(&resendAt)
	if err == nil && now.Before(fromMillis(resendAt)) {
		return ErrRateLimited
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	const query = "INSERT INTO email_verification_codes " +
		"(email, purpose, nonce, code_hash, source_hash, expires_at, resend_available_at, attempts_remaining, created_at, consumed_at) " +
		"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL) " +
		"ON CONFLICT(email, purpose) DO UPDATE SET nonce=excluded.nonce, code_hash=excluded.code_hash, source_hash=excluded.source_hash, " +
		"expires_at=excluded.expires_at, resend_available_at=excluded.resend_available_at, " +
		"attempts_remaining=excluded.attempts_remaining, created_at=excluded.created_at, consumed_at=NULL"
	if _, err := tx.ExecContext(
		ctx,
		query,
		code.Email,
		code.Purpose,
		code.Nonce,
		code.CodeHash,
		code.SourceHash,
		toMillis(code.ExpiresAt),
		toMillis(code.ResendAvailableAt),
		code.AttemptsRemaining,
		toMillis(code.CreatedAt),
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) GetVerificationCode(ctx context.Context, email, purpose string) (VerificationCode, error) {
	const query = "SELECT email, purpose, nonce, code_hash, source_hash, expires_at, resend_available_at, " +
		"attempts_remaining, created_at, consumed_at FROM email_verification_codes WHERE email = ? AND purpose = ?"
	var code VerificationCode
	var expiresAt, resendAt, createdAt int64
	var consumedAt sql.NullInt64
	err := s.db.QueryRowContext(ctx, query, email, purpose).Scan(
		&code.Email,
		&code.Purpose,
		&code.Nonce,
		&code.CodeHash,
		&code.SourceHash,
		&expiresAt,
		&resendAt,
		&code.AttemptsRemaining,
		&createdAt,
		&consumedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return VerificationCode{}, ErrNotFound
	}
	if err != nil {
		return VerificationCode{}, err
	}
	code.ExpiresAt = fromMillis(expiresAt)
	code.ResendAvailableAt = fromMillis(resendAt)
	code.CreatedAt = fromMillis(createdAt)
	code.ConsumedAt = nullableTime(consumedAt)
	return code, nil
}

func (s *SQLiteStore) DecrementVerificationAttempts(ctx context.Context, email, purpose string, codeHash []byte, now time.Time) error {
	result, err := s.db.ExecContext(
		ctx,
		"UPDATE email_verification_codes SET attempts_remaining = attempts_remaining - 1 "+
			"WHERE email = ? AND purpose = ? AND code_hash = ? AND consumed_at IS NULL AND expires_at > ? AND attempts_remaining > 0",
		email,
		purpose,
		codeHash,
		toMillis(now),
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrCodeInvalid
	}
	return nil
}

func (s *SQLiteStore) DeleteVerificationCode(ctx context.Context, email, purpose string, codeHash []byte) error {
	_, err := s.db.ExecContext(
		ctx,
		"DELETE FROM email_verification_codes WHERE email = ? AND purpose = ? AND code_hash = ?",
		email,
		purpose,
		codeHash,
	)
	return err
}

func (s *SQLiteStore) RegisterUser(
	ctx context.Context,
	proposed User,
	codeHash []byte,
	session UserSession,
	now time.Time,
) (User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	if err := consumeVerificationCode(ctx, tx, proposed.Email, "register", codeHash, now); err != nil {
		return User{}, err
	}
	existing, err := getUserByEmail(ctx, tx, proposed.Email)
	switch {
	case errors.Is(err, ErrNotFound):
		if _, err := tx.ExecContext(
			ctx,
			"INSERT INTO users (id, email, password_hash, status, created_at, password_changed_at, last_login_at) "+
				"VALUES (?, ?, ?, 'active', ?, ?, ?)",
			proposed.ID,
			proposed.Email,
			proposed.PasswordHash,
			toMillis(proposed.CreatedAt),
			toMillis(now),
			toMillis(now),
		); err != nil {
			return User{}, err
		}
		proposed.Status = "active"
		proposed.PasswordChangedAt = timePointer(now)
		proposed.LastLoginAt = timePointer(now)
		existing = proposed
	case err != nil:
		return User{}, err
	case existing.Status == "pending_verification":
		if _, err := tx.ExecContext(
			ctx,
			"UPDATE users SET password_hash = ?, status = 'active', password_changed_at = ?, last_login_at = ? WHERE id = ?",
			proposed.PasswordHash,
			toMillis(now),
			toMillis(now),
			existing.ID,
		); err != nil {
			return User{}, err
		}
		existing.PasswordHash = proposed.PasswordHash
		existing.Status = "active"
		existing.PasswordChangedAt = timePointer(now)
		existing.LastLoginAt = timePointer(now)
	default:
		return User{}, ErrConflict
	}
	session.UserID = existing.ID
	if err := insertUserSession(ctx, tx, session); err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return existing, nil
}

func (s *SQLiteStore) CreateUserSession(ctx context.Context, email string, session UserSession, now time.Time) (User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	user, err := getUserByEmail(ctx, tx, email)
	if err != nil || user.Status != "active" {
		if err == nil {
			err = ErrNotFound
		}
		return User{}, err
	}
	session.UserID = user.ID
	if err := insertUserSession(ctx, tx, session); err != nil {
		return User{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE users SET last_login_at = ? WHERE id = ?", toMillis(now), user.ID); err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	user.LastLoginAt = timePointer(now)
	return user, nil
}

func (s *SQLiteStore) ResetUserPassword(ctx context.Context, email string, codeHash []byte, passwordHash string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := consumeVerificationCode(ctx, tx, email, "password_reset", codeHash, now); err != nil {
		return err
	}
	result, err := tx.ExecContext(
		ctx,
		"UPDATE users SET password_hash = ?, password_changed_at = ? WHERE email = ? AND status = 'active'",
		passwordHash,
		toMillis(now),
		email,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrCodeInvalid
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM user_sessions WHERE user_id = (SELECT id FROM users WHERE email = ?)", email); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) ChangeUserPassword(
	ctx context.Context,
	userID string,
	passwordHash string,
	session UserSession,
	now time.Time,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(
		ctx,
		"UPDATE users SET password_hash = ?, password_changed_at = ? WHERE id = ? AND status = 'active'",
		passwordHash,
		toMillis(now),
		userID,
	)
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
	if _, err := tx.ExecContext(ctx, "DELETE FROM user_sessions WHERE user_id = ?", userID); err != nil {
		return err
	}
	session.UserID = userID
	if err := insertUserSession(ctx, tx, session); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) GetUserByEmail(ctx context.Context, email string) (User, error) {
	return getUserByEmail(ctx, s.db, email)
}

func (s *SQLiteStore) GetUserBySession(ctx context.Context, sessionHash []byte, now time.Time) (User, UserSession, error) {
	const query = "SELECT u.id, u.email, u.password_hash, u.status, u.created_at, u.password_changed_at, u.last_login_at, " +
		"s.session_hash, s.user_id, s.expires_at, s.created_at, s.last_seen_at " +
		"FROM user_sessions s JOIN users u ON u.id = s.user_id WHERE s.session_hash = ?"
	var user User
	var session UserSession
	var userCreated, sessionExpires, sessionCreated, sessionLastSeen int64
	var passwordChanged, lastLogin sql.NullInt64
	err := s.db.QueryRowContext(ctx, query, sessionHash).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.Status,
		&userCreated,
		&passwordChanged,
		&lastLogin,
		&session.SessionHash,
		&session.UserID,
		&sessionExpires,
		&sessionCreated,
		&sessionLastSeen,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, UserSession{}, ErrNotFound
	}
	if err != nil {
		return User{}, UserSession{}, err
	}
	user.CreatedAt = fromMillis(userCreated)
	user.PasswordChangedAt = nullableTime(passwordChanged)
	user.LastLoginAt = nullableTime(lastLogin)
	session.ExpiresAt = fromMillis(sessionExpires)
	session.CreatedAt = fromMillis(sessionCreated)
	session.LastSeenAt = fromMillis(sessionLastSeen)
	if user.Status != "active" || !now.Before(session.ExpiresAt) {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_sessions WHERE session_hash = ?", sessionHash)
		return User{}, UserSession{}, ErrExpired
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE user_sessions SET last_seen_at = ? WHERE session_hash = ?", toMillis(now), sessionHash); err != nil {
		return User{}, UserSession{}, err
	}
	session.LastSeenAt = now
	return user, session, nil
}

func (s *SQLiteStore) DeleteUserSession(ctx context.Context, sessionHash []byte) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM user_sessions WHERE session_hash = ?", sessionHash)
	return err
}

func consumeVerificationCode(ctx context.Context, tx *sql.Tx, email, purpose string, codeHash []byte, now time.Time) error {
	result, err := tx.ExecContext(
		ctx,
		"UPDATE email_verification_codes SET consumed_at = ? "+
			"WHERE email = ? AND purpose = ? AND code_hash = ? AND consumed_at IS NULL AND expires_at > ? AND attempts_remaining > 0",
		toMillis(now),
		email,
		purpose,
		codeHash,
		toMillis(now),
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrCodeInvalid
	}
	return nil
}

func insertUserSession(ctx context.Context, tx *sql.Tx, session UserSession) error {
	_, err := tx.ExecContext(
		ctx,
		"INSERT INTO user_sessions (session_hash, user_id, expires_at, created_at, last_seen_at) VALUES (?, ?, ?, ?, ?)",
		session.SessionHash,
		session.UserID,
		toMillis(session.ExpiresAt),
		toMillis(session.CreatedAt),
		toMillis(session.LastSeenAt),
	)
	return err
}

func getUserByEmail(ctx context.Context, q queryer, email string) (User, error) {
	const query = "SELECT id, email, password_hash, status, created_at, password_changed_at, last_login_at FROM users WHERE email = ?"
	var user User
	var createdAt int64
	var passwordChanged, lastLogin sql.NullInt64
	err := q.QueryRowContext(ctx, query, email).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.Status,
		&createdAt,
		&passwordChanged,
		&lastLogin,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	user.CreatedAt = fromMillis(createdAt)
	user.PasswordChangedAt = nullableTime(passwordChanged)
	user.LastLoginAt = nullableTime(lastLogin)
	return user, nil
}

func timePointer(value time.Time) *time.Time {
	copy := value
	return &copy
}
