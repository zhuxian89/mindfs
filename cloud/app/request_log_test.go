package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"mindfs-cloud/internal/ops"
)

func TestHandshakeIsCountedOnceWhileSocketIsOpen(t *testing.T) {
	hold := make(chan struct{})
	returned := make(chan struct{})
	upgrader := websocket.Upgrader{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		ops.WebSocketUpgraded(w)
		<-hold
	})
	statuses := make(chan int, 2)
	tracked := requestLogMiddleware(handler, func(_ string, status int, _ time.Duration) { statuses <- status })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(returned)
		tracked.ServeHTTP(w, r)
	}))
	defer server.Close()
	defer func() {
		select {
		case <-hold:
		default:
			close(hold)
		}
	}()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case status := <-statuses:
		if status != 101 {
			t.Fatalf("upgrade status=%d", status)
		}
	case <-time.After(time.Second):
		t.Fatal("handshake was not observed while connected")
	}
	close(hold)
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("handler still active")
	}
	select {
	case status := <-statuses:
		t.Fatalf("duplicate request status %d", status)
	default:
	}
}
