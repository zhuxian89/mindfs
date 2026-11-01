package store

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func TestCancelledReadsObserveNewWALNodes(t *testing.T) {
	s := openTestStore(t)
	seedConcurrentNode(t, s)
	if _, err := s.db.Exec("PRAGMA wal_autocheckpoint=0; PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}

	// Exercise cancellation while a query is producing its result. Older SQLite
	// drivers could lose that result without closing it, pinning a WAL snapshot.
	var workers sync.WaitGroup
	var cancelled atomic.Int64
	for range 4 {
		workers.Go(func() {
			for i := range 3000 {
				ctx, cancel := context.WithCancel(t.Context())
				done := make(chan struct{})
				go func() {
					defer close(done)
					time.Sleep(time.Duration(i%20) * time.Microsecond)
					cancel()
				}()
				_, err := s.GetNode(ctx, "node")
				contextErr := ctx.Err()
				cancel()
				<-done
				if err == nil {
					continue
				}
				var sqliteErr *sqlite.Error
				if errors.Is(err, context.Canceled) ||
					(contextErr != nil && errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_INTERRUPT) {
					cancelled.Add(1)
					continue
				}
				t.Errorf("read during cancellation: %v", err)
				return
			}
		})
	}
	workers.Wait()
	if cancelled.Load() == 0 {
		t.Fatal("no queries were cancelled")
	}

	// Hold every physical reader so a healthy connection cannot hide one that
	// retained an implicit read transaction. Do not reopen the pool or checkpoint
	// the new commit: the new node must be visible from WAL on every reader.
	var readers []*sql.Conn
	for range s.readDB.Stats().MaxOpenConnections {
		conn, err := s.readDB.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		readers = append(readers, conn)
	}
	if _, err := s.db.Exec(`INSERT INTO nodes(id, device_id, owner_user_id, name, status, access_mode, created_at)
		VALUES ('new-node', 'new-device', 'owner', 'new', 'active', 'node_auth', 0)`); err != nil {
		t.Fatal(err)
	}
	for i, conn := range readers {
		if node, err := getNode(t.Context(), conn, "new-node"); err != nil || node.Name != "new" {
			t.Errorf("reader %d cannot see committed node after cancellation: %+v, %v", i, node, err)
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if node, err := s.GetNode(t.Context(), "new-node"); err != nil || node.Name != "new" {
		t.Fatalf("GetNode after cancellation: %+v, %v", node, err)
	}
	if nodes, err := s.ListNodesByOwner(t.Context(), "owner"); err != nil || len(nodes) != 2 {
		t.Fatalf("ListNodesByOwner after cancellation: %+v, %v", nodes, err)
	}
}
