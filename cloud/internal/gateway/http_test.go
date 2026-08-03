package gateway

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mindfs-cloud/internal/connector"
	"mindfs-cloud/internal/store"
)

type nodeStoreStub struct {
	node store.Node
	err  error
}

func (s nodeStoreStub) GetNode(context.Context, string) (store.Node, error) { return s.node, s.err }

type registryStub struct {
	open func(context.Context, string) (net.Conn, error)
}

func (s registryStub) OpenStream(ctx context.Context, nodeID string) (net.Conn, error) {
	return s.open(ctx, nodeID)
}

func TestHTTPGatewayStripsPrefixAndStreamsRequestResponse(t *testing.T) {
	cloud, node := net.Pipe()
	defer node.Close()
	requestSeen := make(chan *http.Request, 1)
	bodySeen := make(chan string, 1)
	go func() {
		request, err := http.ReadRequest(bufio.NewReader(node))
		if err != nil {
			return
		}
		body, _ := io.ReadAll(request.Body)
		requestSeen <- request
		bodySeen <- string(body)
		_, _ = io.WriteString(node, "HTTP/1.1 201 Created\r\nContent-Type: text/plain\r\nContent-Length: 7\r\n\r\ncreated")
	}()
	handler := NewHandler(
		nodeStoreStub{node: store.Node{ID: "nnode1", Status: "active"}},
		registryStub{open: func(context.Context, string) (net.Conn, error) { return cloud, nil }},
		time.Second,
		time.Second,
		"https",
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "https://relay.example/n/nnode1/api/files?q=one", bytes.NewBufferString("payload"))
	request.Header.Set("X-MindFS-Relayed", "forged")
	request.Header.Set("X-MindFS-Relay-Service-Slug", "forged")
	request.Header.Set("X-MindFS-E2EE-Proof", "unchanged")
	if err := handler.ServeHTTP(recorder, request); err != nil {
		t.Fatal(err)
	}
	seen := <-requestSeen
	if seen.URL.Path != "/api/files" || seen.URL.RawQuery != "q=one" || seen.Method != http.MethodPost {
		t.Fatalf("forwarded request = %s %s", seen.Method, seen.URL.String())
	}
	if body := <-bodySeen; body != "payload" {
		t.Fatalf("body = %q", body)
	}
	if seen.Header.Get("X-MindFS-Relayed") != "1" || seen.Header.Get("X-MindFS-Relay-Service-Slug") != "" {
		t.Fatalf("internal headers = %#v", seen.Header)
	}
	if seen.Header.Get("X-MindFS-E2EE-Proof") != "unchanged" {
		t.Fatal("E2EE header changed")
	}
	if seen.Header.Get("X-Forwarded-Host") != "relay.example" || seen.Header.Get("X-Forwarded-Proto") != "https" {
		t.Fatalf("forwarded headers = %#v", seen.Header)
	}
	if recorder.Code != http.StatusCreated || recorder.Body.String() != "created" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestHTTPGatewayErrors(t *testing.T) {
	tests := []struct {
		name       string
		store      nodeStoreStub
		open       func(context.Context, string) (net.Conn, error)
		wantStatus int
		wantCode   string
	}{
		{name: "missing", store: nodeStoreStub{err: store.ErrNotFound}, wantStatus: 404, wantCode: "node_not_found"},
		{name: "offline", store: nodeStoreStub{node: store.Node{Status: "active"}}, open: func(context.Context, string) (net.Conn, error) { return nil, connector.ErrNodeOffline }, wantStatus: 503, wantCode: "node_offline"},
		{name: "timeout", store: nodeStoreStub{node: store.Node{Status: "active"}}, open: func(context.Context, string) (net.Conn, error) { return nil, context.DeadlineExceeded }, wantStatus: 504, wantCode: "node_timeout"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			open := test.open
			if open == nil {
				open = func(context.Context, string) (net.Conn, error) { return nil, connector.ErrNodeOffline }
			}
			handler := NewHandler(test.store, registryStub{open: open}, time.Second, time.Second, "https")
			err := handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/n/nnode1/", nil))
			gatewayErr, ok := err.(*Error)
			if !ok || gatewayErr.Status != test.wantStatus || gatewayErr.Code != test.wantCode {
				t.Fatalf("error = %#v", err)
			}
		})
	}
}

func TestParseNodeRouteNormalizesRoot(t *testing.T) {
	for _, path := range []string{"/n/nnode1", "/n/nnode1/"} {
		nodeID, stripped, err := parseNodeRoute(path)
		if err != nil || nodeID != "nnode1" || stripped != "/" {
			t.Fatalf("parseNodeRoute(%q) = %q, %q, %v", path, nodeID, stripped, err)
		}
	}
}
