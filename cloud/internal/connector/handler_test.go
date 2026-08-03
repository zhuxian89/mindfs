package connector

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"

	"mindfs-cloud/internal/store"
)

type staticAuthenticator struct{}

func (staticAuthenticator) Authenticate(_ context.Context, token string) (store.Node, error) {
	if token != "dt_valid" {
		return store.Node{}, store.ErrNotFound
	}
	return store.Node{ID: "nnode1", Name: "Office Mac"}, nil
}

func TestConnectorCreatesYamuxServerAndOpensStream(t *testing.T) {
	registry := NewSessionRegistry()
	handler := NewHandler(staticAuthenticator{}, registry)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		node, err := handler.Authenticate(r)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler.ServeAuthenticated(w, r, node)
	}))
	defer server.Close()

	headers := http.Header{"Authorization": []string{"Bearer dt_valid"}}
	ws, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), headers)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer ws.Close()
	if got := response.Header.Get("X-MindFS-Relay-Node-Name"); got != "Office Mac" {
		t.Fatalf("node name header = %q", got)
	}
	clientSession, err := yamux.Client(NewWebSocketNetConn(ws), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	deadline := time.Now().Add(2 * time.Second)
	for !registry.Status("nnode1").Online && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !registry.Status("nnode1").Online {
		t.Fatal("connector did not become online")
	}
	accepted := make(chan error, 1)
	go func() {
		stream, err := clientSession.Accept()
		if err != nil {
			accepted <- err
			return
		}
		defer stream.Close()
		payload, err := io.ReadAll(io.LimitReader(stream, 4))
		if err == nil && string(payload) != "ping" {
			err = errors.New("unexpected stream payload")
		}
		accepted <- err
	}()
	stream, err := registry.OpenStream(context.Background(), "nnode1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}
}

func TestWebSocketNetConnRejectsTextTransport(t *testing.T) {
	errorsCh := make(chan error, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			errorsCh <- err
			return
		}
		defer ws.Close()
		_, err = NewWebSocketNetConn(ws).Read(make([]byte, 1))
		errorsCh <- err
	}))
	defer server.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := ws.WriteMessage(websocket.TextMessage, []byte("invalid")); err != nil {
		t.Fatal(err)
	}
	if err := <-errorsCh; !errors.Is(err, ErrNonBinaryTransport) {
		t.Fatalf("Read() error = %v", err)
	}
}

func TestConnectorTextTransportMarksNodeOffline(t *testing.T) {
	registry := NewSessionRegistry()
	handler := NewHandler(staticAuthenticator{}, registry)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		node, err := handler.Authenticate(r)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler.ServeAuthenticated(w, r, node)
	}))
	defer server.Close()
	headers := http.Header{"Authorization": []string{"Bearer dt_valid"}}
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), headers)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	deadline := time.Now().Add(time.Second)
	for !registry.Status("nnode1").Online && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := ws.WriteMessage(websocket.TextMessage, []byte("invalid")); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for registry.Status("nnode1").Online && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if registry.Status("nnode1").Online {
		t.Fatal("node remained online after text transport message")
	}
}
