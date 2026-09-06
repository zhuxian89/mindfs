package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func seedConcurrentNode(t testing.TB, s *SQLiteStore) {
	t.Helper()
	_, err := s.db.Exec(`INSERT INTO nodes(id, device_id, owner_user_id, name, status, access_mode, created_at)
		VALUES ('node', 'device', 'owner', 'original', 'active', 'node_auth', 0)`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestNodeReadsDuringWriteAndAfterCommit(t *testing.T) {
	s := openTestStore(t)
	seedConcurrentNode(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE nodes SET name = 'changed' WHERE id = 'node'"); err != nil {
		t.Fatal(err)
	}
	// The only writer is occupied; readers must still see committed data.
	node, err := s.GetNode(ctx, "node")
	if err != nil || node.Name != "original" {
		t.Fatalf("read during uncommitted write: %+v, %v", node, err)
	}
	nodes, err := s.ListNodesByOwner(ctx, "owner")
	if err != nil || len(nodes) != 1 || nodes[0].Name != "original" {
		t.Fatalf("list during uncommitted write: %+v, %v", nodes, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	node, err = s.GetNode(ctx, "node")
	if err != nil || node.Name != "changed" {
		t.Fatalf("committed rename not visible: %+v, %v", node, err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE nodes SET status = 'disabled' WHERE id = 'node'"); err != nil {
		t.Fatal(err)
	}
	if node, err := s.GetNode(ctx, "node"); err != nil || node.Status != "disabled" {
		t.Fatalf("disabled status not visible: %+v, %v", node, err)
	}
	if err := s.DeleteNodeByOwner(ctx, "owner", "node"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetNode(ctx, "node"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted node still visible: %v", err)
	}
}

func TestSQLitePoolsAreDurableReadOnlyAndReopenable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data space ?#%")
	s, err := OpenSQLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	seedConcurrentNode(t, s)
	var journal string
	var synchronous int
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil || journal != "wal" {
		t.Fatalf("journal mode=%q err=%v", journal, err)
	}
	if err := s.db.QueryRow("PRAGMA synchronous").Scan(&synchronous); err != nil || synchronous != 2 {
		t.Fatalf("synchronous=%d err=%v", synchronous, err)
	}
	if _, err := s.readDB.Exec("UPDATE nodes SET name = 'forbidden'"); err == nil {
		t.Fatal("read pool accepted a write")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.readDB.Ping(); err == nil {
		t.Fatal("reader pool left open")
	}
	reopened, err := OpenSQLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if node, err := reopened.GetNode(context.Background(), "node"); err != nil || node.Name != "original" {
		t.Fatalf("reopened data=%+v err=%v", node, err)
	}
}

func TestReadPoolSaturationRespectsCancellation(t *testing.T) {
	s := openTestStore(t)
	seedConcurrentNode(t, s)
	var held []*sql.Conn
	for range s.readDB.Stats().MaxOpenConnections {
		conn, err := s.readDB.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		held = append(held, conn)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := s.GetNode(ctx, "node"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pool wait ignored cancellation: %v", err)
	}
	held[0].Close()
	if _, err := s.GetNode(context.Background(), "node"); err != nil {
		t.Fatalf("reader leaked after cancellation: %v", err)
	}
}

func TestConcurrentNodeReadsAndWrites(t *testing.T) {
	s := openTestStore(t)
	seedConcurrentNode(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			for range 100 {
				if _, err := s.GetNode(ctx, "node"); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	for i := range 100 {
		if _, err := s.RenameNodeByOwner(ctx, "owner", "node", fmt.Sprint(i)); err != nil {
			t.Error(err)
			break
		}
	}
	workers.Wait()
	if node, err := s.GetNode(ctx, "node"); err != nil || node.Name != "99" {
		t.Fatalf("final write not visible: %+v %v", node, err)
	}
}

func TestSessionCleanupUsesExpiryIndex(t *testing.T) {
	s := openTestStore(t)
	var id, parent, unused int
	var detail string
	err := s.db.QueryRow("EXPLAIN QUERY PLAN DELETE FROM user_sessions WHERE expires_at <= ?", 0).Scan(&id, &parent, &unused, &detail)
	if err != nil || !strings.Contains(detail, "idx_user_sessions_expires_at") {
		t.Fatalf("session cleanup plan=%q err=%v", detail, err)
	}
}
