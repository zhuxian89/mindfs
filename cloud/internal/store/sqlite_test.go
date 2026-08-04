package store

import (
	"bytes"
	"context"
	"errors"
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
		ID:         "nnode1",
		DeviceID:   "device-1",
		Name:       "Office Mac",
		Status:     "active",
		AccessMode: "node_auth",
		CreatedAt:  now,
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
	if confirmed.Status != BindConfirmed || gotNode.ID != node.ID {
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
			ID:         "node-" + name,
			DeviceID:   "device-" + name,
			Name:       name,
			Status:     "active",
			AccessMode: "node_auth",
			CreatedAt:  now.Add(time.Duration(index) * time.Second),
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

	nodes, err := s.ListNodes(ctx)
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
	node := Node{ID: "node-rename", DeviceID: "device-rename", Name: "Before", Status: "active", AccessMode: "node_auth", CreatedAt: now}
	token := DeviceTokenRecord{ID: "token-rename", NodeID: node.ID, TokenHash: []byte("hash-rename"), Status: "active", CreatedAt: now}
	if _, _, err := s.ConfirmChallenge(ctx, codeHash, node, token, now); err != nil {
		t.Fatal(err)
	}

	renamed, err := s.RenameNode(ctx, node.ID, "After")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "After" {
		t.Fatalf("renamed = %#v", renamed)
	}
	if _, err := s.RenameNode(ctx, "missing", "Name"); !errors.Is(err, ErrNotFound) {
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
	node := Node{ID: "node-delete", DeviceID: "device-delete", Name: "Delete", Status: "active", AccessMode: "node_auth", CreatedAt: now}
	tokenHash := []byte("hash-delete")
	token := DeviceTokenRecord{ID: "token-delete", NodeID: node.ID, TokenHash: tokenHash, Status: "active", CreatedAt: now}
	if _, _, err := s.ConfirmChallenge(ctx, codeHash, node, token, now); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteNode(ctx, node.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetNode(ctx, node.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetNode() error = %v", err)
	}
	if _, err := s.AuthenticateDeviceToken(ctx, tokenHash, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AuthenticateDeviceToken() error = %v", err)
	}
	if err := s.DeleteNode(ctx, node.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second DeleteNode() error = %v", err)
	}
}

func TestSQLiteSchemaContainsOnlyV0Tables(t *testing.T) {
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
	want := []string{"admin_sessions", "bind_challenges", "device_tokens", "nodes"}
	if len(tables) != len(want) {
		t.Fatalf("tables = %#v", tables)
	}
	for i := range want {
		if tables[i] != want[i] {
			t.Fatalf("tables = %#v want %#v", tables, want)
		}
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

	_, _, err = s.ConfirmChallenge(ctx, codeHash, Node{}, DeviceTokenRecord{}, now.Add(2*time.Minute))
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

func openTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := OpenSQLite(t.TempDir())
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
