package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestConfirmChallengeCommitsAtomically(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	codeHash := []byte("code-hash")

	challenge, claimed, err := s.ObserveChallenge(ctx, codeHash, "device-1", now, now.Add(10*time.Minute))
	if err != nil || claimed {
		t.Fatalf("ObserveChallenge() = %#v, %v, %v", challenge, claimed, err)
	}
	node := Node{
		ID:          "nnode1",
		DeviceID:    "device-1",
		OwnerUserID: "usr_owner",
		Name:        "Office Mac",
		Status:      "active",
		AccessMode:  "node_auth",
		CreatedAt:   now,
	}
	token := DeviceTokenRecord{
		ID:        "tok_1",
		NodeID:    node.ID,
		TokenHash: []byte("token-hash"),
		Status:    "active",
		CreatedAt: now,
	}

	confirmed, gotNode, err := s.ConfirmChallenge(ctx, codeHash, node, token, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ConfirmChallenge() error = %v", err)
	}
	if confirmed.Status != BindConfirmed || confirmed.ClaimedByUserID != node.OwnerUserID || gotNode.ID != node.ID {
		t.Fatalf("ConfirmChallenge() = %#v, %#v", confirmed, gotNode)
	}
	stored, err := s.AuthenticateDeviceToken(ctx, token.TokenHash, now.Add(2*time.Minute))
	if err != nil || stored.ID != node.ID {
		t.Fatalf("AuthenticateDeviceToken() = %#v, %v", stored, err)
	}
}

func TestListNodesReturnsPersistedNodes(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	for index, name := range []string{"Office Mac", "Home PC"} {
		codeHash := []byte{byte(index + 1)}
		if _, _, err := s.ObserveChallenge(ctx, codeHash, "device-"+name, now, now.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		node := Node{
			ID:          "node-" + name,
			DeviceID:    "device-" + name,
			OwnerUserID: "usr_owner",
			Name:        name,
			Status:      "active",
			AccessMode:  "node_auth",
			CreatedAt:   now.Add(time.Duration(index) * time.Second),
		}
		token := DeviceTokenRecord{
			ID:        "token-" + name,
			NodeID:    node.ID,
			TokenHash: []byte("hash-" + name),
			Status:    "active",
			CreatedAt: node.CreatedAt,
		}
		if _, _, err := s.ConfirmChallenge(ctx, codeHash, node, token, now); err != nil {
			t.Fatal(err)
		}
	}

	nodes, err := s.ListNodesByOwner(ctx, "usr_owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes = %#v", nodes)
	}
}

func TestRenameNodePersistsName(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	codeHash := []byte("rename-node")
	if _, _, err := s.ObserveChallenge(ctx, codeHash, "device-rename", now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	node := Node{ID: "node-rename", DeviceID: "device-rename", OwnerUserID: "usr_owner", Name: "Before", Status: "active", AccessMode: "node_auth", CreatedAt: now}
	token := DeviceTokenRecord{ID: "token-rename", NodeID: node.ID, TokenHash: []byte("hash-rename"), Status: "active", CreatedAt: now}
	if _, _, err := s.ConfirmChallenge(ctx, codeHash, node, token, now); err != nil {
		t.Fatal(err)
	}

	if _, err := s.RenameNodeByOwner(ctx, "usr_other", node.ID, "Stolen"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other owner rename error = %v", err)
	}
	renamed, err := s.RenameNodeByOwner(ctx, node.OwnerUserID, node.ID, "After")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "After" {
		t.Fatalf("renamed = %#v", renamed)
	}
	if _, err := s.RenameNodeByOwner(ctx, node.OwnerUserID, "missing", "Name"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing rename error = %v", err)
	}
}

func TestDeleteNodeRevokesDeviceToken(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	codeHash := []byte("delete-node")
	if _, _, err := s.ObserveChallenge(ctx, codeHash, "device-delete", now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	node := Node{ID: "node-delete", DeviceID: "device-delete", OwnerUserID: "usr_owner", Name: "Delete", Status: "active", AccessMode: "node_auth", CreatedAt: now}
	tokenHash := []byte("hash-delete")
	token := DeviceTokenRecord{ID: "token-delete", NodeID: node.ID, TokenHash: tokenHash, Status: "active", CreatedAt: now}
	if _, _, err := s.ConfirmChallenge(ctx, codeHash, node, token, now); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteNodeByOwner(ctx, "usr_other", node.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other owner delete error = %v", err)
	}
	if _, err := s.AuthenticateDeviceToken(ctx, tokenHash, now); err != nil {
		t.Fatalf("token changed after unauthorized delete: %v", err)
	}
	if err := s.DeleteNodeByOwner(ctx, node.OwnerUserID, node.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetNode(ctx, node.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetNode() error = %v", err)
	}
	if _, err := s.AuthenticateDeviceToken(ctx, tokenHash, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AuthenticateDeviceToken() error = %v", err)
	}
	if err := s.DeleteNodeByOwner(ctx, node.OwnerUserID, node.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second DeleteNode() error = %v", err)
	}
}

func TestSQLiteSchemaContainsRelayAndIdentityTables(t *testing.T) {
	s := openTestStore(t)
	rows, err := s.db.Query("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(tables)
	want := []string{"auth_rate_limits", "bind_challenges", "device_tokens", "email_verification_codes", "nodes", "user_sessions", "users"}
	if len(tables) != len(want) {
		t.Fatalf("tables = %#v", tables)
	}
	for i := range want {
		if tables[i] != want[i] {
			t.Fatalf("tables = %#v want %#v", tables, want)
		}
	}
}

func TestAuthRateLimitPersistsAcrossStoreReopen(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	first, err := OpenSQLite(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.TakeRateLimit(ctx, "login-email", []byte("subject"), now, time.Hour, 1); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := OpenSQLite(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := second.TakeRateLimit(ctx, "login-email", []byte("subject"), now, time.Hour, 1); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("TakeRateLimit() error = %v", err)
	}
}

func TestConfirmExpiredChallengePersistsExpiredStatus(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	codeHash := []byte("expired-code")
	_, _, err := s.ObserveChallenge(ctx, codeHash, "device-1", now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = s.ConfirmChallenge(ctx, codeHash, Node{OwnerUserID: "usr_owner"}, DeviceTokenRecord{}, now.Add(2*time.Minute))
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("ConfirmChallenge() error = %v", err)
	}
	challenge, err := s.GetChallenge(ctx, codeHash)
	if err != nil {
		t.Fatal(err)
	}
	if challenge.Status != BindExpired {
		t.Fatalf("status = %q", challenge.Status)
	}
}

func TestObserveChallengeProtectsDeviceOwnership(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	now := time.Now().UTC()
	codeHash := []byte("owned-code")
	_, _, err := s.ObserveChallenge(ctx, codeHash, "device-1", now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	challenge, claimed, err := s.ObserveChallenge(ctx, codeHash, "device-2", now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !claimed || challenge.DeviceID != "device-1" || !bytes.Equal(challenge.CodeHash, codeHash) {
		t.Fatalf("ObserveChallenge() = %#v, claimed=%v", challenge, claimed)
	}
}

func TestSQLiteV0OwnershipMigrationIsIdempotent(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := sql.Open("sqlite", filepath.Join(dataDir, "mindfs-cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	legacySchema := `
CREATE TABLE bind_challenges (
    code_hash BLOB PRIMARY KEY, device_id TEXT NOT NULL,
    requested_node_name TEXT NOT NULL DEFAULT '', root_hint TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL, node_id TEXT NOT NULL DEFAULT '',
    token_derivation_version INTEGER NOT NULL DEFAULT 1,
    expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL, confirmed_at INTEGER
);
CREATE TABLE nodes (
    id TEXT PRIMARY KEY, device_id TEXT NOT NULL, name TEXT NOT NULL,
    status TEXT NOT NULL, access_mode TEXT NOT NULL, created_at INTEGER NOT NULL,
    last_seen_at INTEGER
);
CREATE TABLE device_tokens (
    id TEXT PRIMARY KEY, node_id TEXT NOT NULL, token_hash BLOB NOT NULL UNIQUE,
    status TEXT NOT NULL, created_at INTEGER NOT NULL, last_used_at INTEGER
);`
	if _, err := database.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	if _, err := database.Exec(
		"INSERT INTO nodes (id, device_id, name, status, access_mode, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		"legacy-node", "legacy-device", "Legacy", "active", "node_auth", toMillis(now),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		"INSERT INTO bind_challenges (code_hash, device_id, status, node_id, expires_at, created_at, confirmed_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		[]byte("legacy-code"), "legacy-device", BindConfirmed, "legacy-node", toMillis(now.Add(time.Hour)), toMillis(now), toMillis(now),
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := OpenSQLite(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, claimed, err := upgraded.ClaimOwnerlessNodes(ctx, "bootstrap@qq.com", now)
	if err != nil || !claimed {
		t.Fatalf("ClaimOwnerlessNodes() = %#v, %v, %v", bootstrap, claimed, err)
	}
	if bootstrap.Status != "pending_verification" || bootstrap.PasswordHash != "" {
		t.Fatalf("bootstrap user = %#v", bootstrap)
	}
	node, err := upgraded.GetNode(ctx, "legacy-node")
	if err != nil || node.OwnerUserID != bootstrap.ID {
		t.Fatalf("legacy node = %#v err=%v", node, err)
	}
	challenge, err := upgraded.GetChallenge(ctx, []byte("legacy-code"))
	if err != nil || challenge.ClaimedByUserID != bootstrap.ID {
		t.Fatalf("legacy challenge = %#v err=%v", challenge, err)
	}
	if _, claimed, err := upgraded.ClaimOwnerlessNodes(ctx, "bootstrap@qq.com", now.Add(time.Minute)); err != nil || claimed {
		t.Fatalf("second ClaimOwnerlessNodes() claimed=%v err=%v", claimed, err)
	}
	if err := upgraded.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQLite(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if node, err := reopened.GetNode(ctx, "legacy-node"); err != nil || node.OwnerUserID != bootstrap.ID {
		t.Fatalf("reopened legacy node = %#v err=%v", node, err)
	}
}

func openTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := OpenSQLite(t.TempDir())
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
