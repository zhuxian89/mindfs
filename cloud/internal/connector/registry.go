package connector

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

var ErrNodeOffline = errors.New("node offline")

type RelaySession interface {
	Open() (net.Conn, error)
	Close() error
}

type NodePresence struct {
	NodeID       string
	ConnectionID string
	Online       bool
	ConnectedAt  time.Time
}

type registryEntry struct {
	connectionID string
	connectedAt  time.Time
	session      RelaySession
}

type SessionRegistry interface {
	Register(string, string, RelaySession) RelaySession
	Unregister(string, string)
	OpenStream(context.Context, string) (net.Conn, error)
	Status(string) NodePresence
}

type Registry struct {
	mu       sync.RWMutex
	sessions map[string]registryEntry
}

func NewSessionRegistry() *Registry {
	return &Registry{sessions: make(map[string]registryEntry)}
}

func (r *Registry) Register(nodeID, connectionID string, session RelaySession) RelaySession {
	r.mu.Lock()
	previous := r.sessions[nodeID]
	r.sessions[nodeID] = registryEntry{
		connectionID: connectionID,
		connectedAt:  time.Now().UTC(),
		session:      session,
	}
	r.mu.Unlock()
	return previous.session
}

func (r *Registry) Unregister(nodeID, connectionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.sessions[nodeID]
	if ok && current.connectionID == connectionID {
		delete(r.sessions, nodeID)
	}
}

func (r *Registry) OpenStream(ctx context.Context, nodeID string) (net.Conn, error) {
	r.mu.RLock()
	entry, ok := r.sessions[nodeID]
	r.mu.RUnlock()
	if !ok || entry.session == nil {
		return nil, ErrNodeOffline
	}
	type result struct {
		conn net.Conn
		err  error
	}
	opened := make(chan result, 1)
	go func() {
		conn, err := entry.session.Open()
		opened <- result{conn: conn, err: err}
	}()
	select {
	case result := <-opened:
		return result.conn, result.err
	case <-ctx.Done():
		go func() {
			result := <-opened
			if result.conn != nil {
				_ = result.conn.Close()
			}
		}()
		return nil, ctx.Err()
	}
}

func (r *Registry) Status(nodeID string) NodePresence {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.sessions[nodeID]
	if !ok {
		return NodePresence{NodeID: nodeID}
	}
	return NodePresence{
		NodeID:       nodeID,
		ConnectionID: entry.connectionID,
		Online:       true,
		ConnectedAt:  entry.connectedAt,
	}
}

func (r *Registry) Close() error {
	r.mu.Lock()
	sessions := make([]RelaySession, 0, len(r.sessions))
	for nodeID, entry := range r.sessions {
		sessions = append(sessions, entry.session)
		delete(r.sessions, nodeID)
	}
	r.mu.Unlock()
	for _, session := range sessions {
		_ = session.Close()
	}
	return nil
}

var _ SessionRegistry = (*Registry)(nil)
