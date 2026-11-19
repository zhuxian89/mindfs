package gateway

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"

	"mindfs-cloud/internal/connector"
	"mindfs-cloud/internal/store"
)

func TestStalledRequestLeavesEstablishedStreamUsable(t *testing.T) {
	const nodeID = "test-node"
	registry := connector.NewSessionRegistry()
	handler := connector.NewHandler(nil, registry)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeAuthenticated(w, r, store.Node{ID: nodeID})
	}))
	defer server.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	// Use the same original yamux implementation as the installed nodes.
	node, err := yamux.Client(connector.NewWebSocketNetConn(ws), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	deadline := time.Now().Add(3 * time.Second)
	for !registry.Status(nodeID).Online && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	chat, err := registry.OpenStream(context.Background(), nodeID)
	if err != nil {
		t.Fatal(err)
	}
	defer chat.Close()
	peer, err := node.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if err := chat.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if err := peer.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}

	// The Relay advertises the window on the wire. The legacy node can send
	// well over 256 KiB without waiting for the Relay application to read.
	payload := bytes.Repeat([]byte("a"), 3<<20)
	if _, err := peer.Write(payload); err != nil {
		t.Fatal(err)
	}
	received := make([]byte, len(payload))
	if _, err := io.ReadFull(chat, received); err != nil || !bytes.Equal(payload, received) {
		t.Fatal("large response corrupted", err)
	}

	// The node never accepts the second stream. A small request stalls waiting
	// for headers; a large upload fills the peer's initial receive window.
	h := NewHandler(nodeStoreStub{node: store.Node{Status: "active"}}, registry, 80*time.Millisecond, 100*time.Millisecond, "http")
	for _, size := range []int{0, 2 << 20} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		request := httptest.NewRequest(http.MethodPost, "/n/"+nodeID+"/slow", bytes.NewReader(make([]byte, size))).WithContext(ctx)
		err := h.ServeHTTP(httptest.NewRecorder(), request)
		cancel()
		failure, ok := err.(*Error)
		if !ok || failure.Status != http.StatusGatewayTimeout {
			t.Fatalf("stalled request size=%d: %v", size, err)
		}
		if _, err := peer.Write([]byte("pong")); err != nil {
			t.Fatal(err)
		}
		var pong [4]byte
		if _, err := io.ReadFull(chat, pong[:]); err != nil || string(pong[:]) != "pong" {
			t.Fatalf("established stream after timeout: %q %v", pong, err)
		}
	}
}

func TestEventStreamFlushesBeforeResponseEnds(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-release
	}))
	defer upstream.Close()
	response, err := upstream.Client().Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeHTTPResponse(w, r, response) }))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || line != "data: first\n" {
		t.Fatalf("first event = %q, %v", line, err)
	}
}
