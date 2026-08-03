CREATE TABLE IF NOT EXISTS admin_sessions (
    session_hash BLOB PRIMARY KEY,
    csrf_hash BLOB NOT NULL,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS bind_challenges (
    code_hash BLOB PRIMARY KEY,
    device_id TEXT NOT NULL,
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
