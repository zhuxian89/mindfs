package connector

import (
	"context"
	"net"
	"testing"
)

type fakeSession struct {
	open   func() (net.Conn, error)
	closed bool
}

func (s *fakeSession) Open() (net.Conn, error) { return s.open() }
func (s *fakeSession) Close() error {
	s.closed = true
	return nil
}

func TestRegistryReplacementUsesCompareAndDelete(t *testing.T) {
	registry := NewSessionRegistry()
	first := &fakeSession{open: func() (net.Conn, error) { return nil, nil }}
	second := &fakeSession{open: func() (net.Conn, error) { return nil, nil }}
	if replaced := registry.Register("node-1", "conn-1", first); replaced != nil {
		t.Fatal("first register replaced a session")
	}
	if replaced := registry.Register("node-1", "conn-2", second); replaced != first {
		t.Fatal("second register did not return old session")
	}
	registry.Unregister("node-1", "conn-1")
	presence := registry.Status("node-1")
	if !presence.Online || presence.ConnectionID != "conn-2" {
		t.Fatalf("presence = %#v", presence)
	}
	registry.Unregister("node-1", "conn-2")
	if registry.Status("node-1").Online {
		t.Fatal("current connection was not removed")
	}
}

func TestRegistryOpenStream(t *testing.T) {
	registry := NewSessionRegistry()
	server, client := net.Pipe()
	defer client.Close()
	registry.Register("node-1", "conn-1", &fakeSession{open: func() (net.Conn, error) { return server, nil }})
	opened, err := registry.OpenStream(context.Background(), "node-1")
	if err != nil {
		t.Fatal(err)
	}
	if opened != server {
		t.Fatal("unexpected stream")
	}
	_ = opened.Close()
}

func TestRegistryDisconnectClosesActiveSession(t *testing.T) {
	registry := NewSessionRegistry()
	session := &fakeSession{open: func() (net.Conn, error) { return nil, nil }}
	registry.Register("node-1", "conn-1", session)
	if err := registry.Disconnect("node-1"); err != nil {
		t.Fatal(err)
	}
	if !session.closed || registry.Status("node-1").Online {
		t.Fatalf("closed=%v presence=%#v", session.closed, registry.Status("node-1"))
	}
	if err := registry.Disconnect("node-1"); err != nil {
		t.Fatal(err)
	}
}
