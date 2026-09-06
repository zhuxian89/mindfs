package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestBindingCreationLimitsPreserveExistingPolls(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < maxNewBindChallengesPerMinute; i++ {
		if _, _, err := s.ObserveChallenge(ctx, []byte(fmt.Sprint(i)), "device", now, now.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.ObserveChallenge(ctx, []byte("excess"), "device", now, now.Add(time.Minute)); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("creation limit=%v", err)
	}
	if _, _, err := s.ObserveChallenge(ctx, []byte("0"), "device", now, now.Add(time.Minute)); err != nil {
		t.Fatalf("existing poll rejected=%v", err)
	}
	later := now.Add(time.Minute + time.Millisecond)
	if _, _, err := s.ObserveChallenge(ctx, []byte("later"), "device", later, later.Add(time.Minute)); err != nil {
		t.Fatalf("new window rejected=%v", err)
	}
}

func TestBindingStorageCapacityIsBounded(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `WITH RECURSIVE numbers(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM numbers WHERE n < ?)
		INSERT INTO bind_challenges(code_hash,device_id,status,expires_at,created_at)
		SELECT CAST(printf('capacity-%d',n) AS BLOB),'device','pending',?,? FROM numbers`, maxBindChallenges, toMillis(now.Add(time.Hour)), toMillis(now.Add(-time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ObserveChallenge(ctx, []byte("new"), "device", now, now.Add(time.Minute)); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("capacity limit=%v", err)
	}
	if _, _, err := s.ObserveChallenge(ctx, []byte("capacity-1"), "device", now, now.Add(time.Minute)); err != nil {
		t.Fatalf("existing poll rejected=%v", err)
	}
}

func TestCleanupRemovesOldChallengesWithoutRevokingDevices(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	hash := []byte("confirmed")
	_, _, err := s.ObserveChallenge(ctx, hash, "device", now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	node := Node{ID: "node", DeviceID: "device", OwnerUserID: "owner", Name: "Node", Status: "active", AccessMode: "node_auth", CreatedAt: now}
	token := DeviceTokenRecord{ID: "token", NodeID: node.ID, TokenHash: []byte("token-hash"), Status: "active", CreatedAt: now}
	if _, _, err := s.ConfirmChallenge(ctx, hash, node, token, now); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteExpired(ctx, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetChallenge(ctx, hash); err != nil {
		t.Fatalf("retry tombstone removed early=%v", err)
	}
	later := now.Add(bindChallengeRetention + 2*time.Minute)
	if _, _, err := s.ObserveChallenge(ctx, []byte("fresh"), "device2", later, later.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteExpired(ctx, later); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetChallenge(ctx, hash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired challenge retained=%v", err)
	}
	if _, err := s.GetChallenge(ctx, []byte("fresh")); err != nil {
		t.Fatalf("fresh challenge removed=%v", err)
	}
	if _, err := s.AuthenticateDeviceToken(ctx, token.TokenHash, later); err != nil {
		t.Fatalf("device token revoked by cleanup=%v", err)
	}
}
