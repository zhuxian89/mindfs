CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    password_changed_at INTEGER,
    last_login_at INTEGER
);

CREATE TABLE IF NOT EXISTS email_verification_codes (
    email TEXT NOT NULL,
    purpose TEXT NOT NULL,
    nonce BLOB NOT NULL,
    code_hash BLOB NOT NULL,
    source_hash BLOB NOT NULL,
    expires_at INTEGER NOT NULL,
    resend_available_at INTEGER NOT NULL,
    attempts_remaining INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    consumed_at INTEGER,
    PRIMARY KEY(email, purpose)
);

CREATE TABLE IF NOT EXISTS user_sessions (
    session_hash BLOB PRIMARY KEY,
    user_id TEXT NOT NULL,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    FOREIGN KEY(user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS auth_rate_limits (
    scope TEXT NOT NULL,
    subject_hash BLOB NOT NULL,
    window_started_at INTEGER NOT NULL,
    count INTEGER NOT NULL,
    PRIMARY KEY(scope, subject_hash, window_started_at)
);

CREATE TABLE IF NOT EXISTS bind_challenges (
    code_hash BLOB PRIMARY KEY,
    device_id TEXT NOT NULL,
    claimed_by_user_id TEXT NOT NULL DEFAULT '',
    requested_node_name TEXT NOT NULL DEFAULT '',
    root_hint TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    node_id TEXT NOT NULL DEFAULT '',
    token_derivation_version INTEGER NOT NULL DEFAULT 1,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    confirmed_at INTEGER
);

CREATE TABLE IF NOT EXISTS nodes (
    id TEXT PRIMARY KEY,
    device_id TEXT NOT NULL,
    owner_user_id TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    status TEXT NOT NULL,
    access_mode TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    last_seen_at INTEGER
);

CREATE TABLE IF NOT EXISTS device_tokens (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL,
    token_hash BLOB NOT NULL UNIQUE,
    status TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    last_used_at INTEGER,
    FOREIGN KEY(node_id) REFERENCES nodes(id)
);

CREATE INDEX IF NOT EXISTS idx_bind_challenges_node_id ON bind_challenges(node_id);
CREATE INDEX IF NOT EXISTS idx_device_tokens_node_id ON device_tokens(node_id);
CREATE INDEX IF NOT EXISTS idx_user_sessions_user_id ON user_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_email_verification_codes_expires_at ON email_verification_codes(expires_at);
CREATE INDEX IF NOT EXISTS idx_auth_rate_limits_window ON auth_rate_limits(window_started_at);
