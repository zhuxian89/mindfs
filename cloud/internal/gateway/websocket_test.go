package gateway

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"mindfs-cloud/internal/store"
)

func TestWebSocketGatewayBridgesTextBinaryAndClose(t *testing.T) {
	cloud, node := net.Pipe()
	handler := NewHandler(
		nodeStoreStub{node: store.Node{ID: "nnode1", Status: "active"}},
		registryStub{open: func(context.Context, string) (net.Conn, error) { return cloud, nil }},
		time.Second,
		time.Second,
		"https",
	)
	nodeErr := make(chan error, 1)
	go func() {
		defer node.Close()
		request, err := http.ReadRequest(bufio.NewReader(node))
		if err != nil {
			nodeErr <- err
			return
		}
		if request.URL.Path != "/ws" || request.Header.Get("X-MindFS-Relayed") != "1" {
			nodeErr <- io.ErrUnexpectedEOF
			return
		}
		_, err = io.WriteString(node, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Protocol: mindfs\r\n\r\n")
		if err != nil {
			nodeErr <- err
			return
		}
		for i := 0; i < 2; i++ {
			frameType, opcode, payload, _, _, err := readWSFrame(node, 1024)
			if err != nil || frameType != wsFrameData {
				nodeErr <- err
				return
			}
			body, err := io.ReadAll(payload)
			if err != nil {
				nodeErr <- err
				return
			}
			if err := writeWSDataFrame(node, opcode, body); err != nil {
				nodeErr <- err
				return
			}
		}
		nodeErr <- writeWSCloseFrame(node, websocket.CloseNormalClosure, "done")
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := handler.ServeWebSocket(w, r, 1024); err != nil {
			t.Errorf("ServeWebSocket() error = %v", err)
		}
	}))
	defer server.Close()
	dialer := *websocket.DefaultDialer
	dialer.Subprotocols = []string{"mindfs"}
	ws, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/n/nnode1/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if ws.Subprotocol() != "mindfs" {
		t.Fatalf("subprotocol = %q", ws.Subprotocol())
	}
	for _, message := range []struct {
		opcode int
		body   string
	}{{websocket.TextMessage, "hello"}, {websocket.BinaryMessage, "\x00\x01"}} {
		if err := ws.WriteMessage(message.opcode, []byte(message.body)); err != nil {
			t.Fatal(err)
		}
		opcode, payload, err := ws.ReadMessage()
		if err != nil || opcode != message.opcode || string(payload) != message.body {
			t.Fatalf("echo = opcode %d payload %q err %v", opcode, payload, err)
		}
	}
	_, _, err = ws.ReadMessage()
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) || closeErr.Code != websocket.CloseNormalClosure || closeErr.Text != "done" {
		t.Fatalf("close error = %v", err)
	}
	if err := <-nodeErr; err != nil {
		t.Fatal(err)
	}
}

func TestWebSocketGatewayReturnsNodeNonUpgradeResponse(t *testing.T) {
	cloud, node := net.Pipe()
	handler := NewHandler(
		nodeStoreStub{node: store.Node{ID: "nnode1", Status: "active"}},
		registryStub{open: func(context.Context, string) (net.Conn, error) { return cloud, nil }},
		time.Second,
		time.Second,
		"https",
	)
	go func() {
		defer node.Close()
		_, _ = http.ReadRequest(bufio.NewReader(node))
		_, _ = io.WriteString(node, "HTTP/1.1 403 Forbidden\r\nContent-Length: 6\r\nContent-Type: text/plain\r\n\r\ndenied")
	}()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = handler.ServeWebSocket(w, r, 1024)
	}))
	defer server.Close()
	_, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/n/nnode1/ws", nil)
	if err == nil || response == nil {
		t.Fatalf("dial error = %v response=%#v", err, response)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusForbidden || string(body) != "denied" {
		t.Fatalf("response = %d %q", response.StatusCode, body)
	}
}

func TestWebSocketGatewayRejectsUnknownFrame(t *testing.T) {
	cloud, node := net.Pipe()
	handler := NewHandler(
		nodeStoreStub{node: store.Node{ID: "nnode1", Status: "active"}},
		registryStub{open: func(context.Context, string) (net.Conn, error) { return cloud, nil }},
		time.Second,
		time.Second,
		"https",
	)
	go func() {
		defer node.Close()
		_, _ = http.ReadRequest(bufio.NewReader(node))
		_, _ = io.WriteString(node, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		_, _ = node.Write([]byte{99})
	}()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = handler.ServeWebSocket(w, r, 1024)
	}))
	defer server.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/n/nnode1/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_, _, err = ws.ReadMessage()
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) || closeErr.Code != websocket.CloseProtocolError {
		t.Fatalf("close error = %v", err)
	}
}

func TestWebSocketGatewayClosesOversizedPublicMessageWith1009(t *testing.T) {
	cloud, node := net.Pipe()
	handler := NewHandler(
		nodeStoreStub{node: store.Node{ID: "nnode1", Status: "active"}},
		registryStub{open: func(context.Context, string) (net.Conn, error) { return cloud, nil }},
		time.Second,
		time.Second,
		"https",
	)
	nodeClose := make(chan int, 1)
	go func() {
		defer node.Close()
		_, _ = http.ReadRequest(bufio.NewReader(node))
		_, _ = io.WriteString(node, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		frameType, _, _, closeCode, _, _ := readWSFrame(node, 1024)
		if frameType == wsFrameClose {
			nodeClose <- closeCode
		}
	}()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = handler.ServeWebSocket(w, r, 4)
	}))
	defer server.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/n/nnode1/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := ws.WriteMessage(websocket.BinaryMessage, []byte("12345")); err != nil {
		t.Fatal(err)
	}
	_, _, err = ws.ReadMessage()
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) || closeErr.Code != websocket.CloseMessageTooBig {
		t.Fatalf("close error = %v", err)
	}
	select {
	case code := <-nodeClose:
		if code != websocket.CloseMessageTooBig {
			t.Fatalf("node close code = %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("node did not receive close frame")
	}
}

func TestWebSocketFrameLimitIsCheckedBeforeAllocation(t *testing.T) {
	header := []byte{wsFrameData, byte(websocket.BinaryMessage), 0, 0, 4, 0}
	_, _, _, _, _, err := readWSFrame(strings.NewReader(string(header)), 16)
	if err != errWSTooLarge {
		t.Fatalf("readWSFrame() error = %v", err)
	}
}
