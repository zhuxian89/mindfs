package gateway

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mindfs-cloud/internal/store"
)

func TestGatewaysPreserveEscapedRequestTarget(t *testing.T) {
	targets := []struct{ incoming, want string }{
		{"/n/node1", "/"},
		{"/n/node1/", "/"},
		{"/n/node1/api/memory?agent=codex", "/api/memory?agent=codex"},
		{"/n/node1/api/calls/task%3A1?root=a%2Fb&root=c+d", "/api/calls/task%3A1?root=a%2Fb&root=c+d"},
		{"/n/node1/api/calls/task%3a1?x=%3a&x=%3A", "/api/calls/task%3a1?x=%3a&x=%3A"},
		{"/n/node1/api/calls/a%2Fb", "/api/calls/a%2Fb"},
		{"/n/node1/api/calls/a%252Fb", "/api/calls/a%252Fb"},
		{"/n/node1/api/calls/%E4%B8%AD%20%23%3F%25", "/api/calls/%E4%B8%AD%20%23%3F%25"},
		{"/n/node1/api/%61%2E%2e//call/", "/api/%61%2E%2e//call/"},
		{"/n/%6Eode1/api/calls/task%3A1?", "/api/calls/task%3A1?"},
	}
	for _, upgrade := range []bool{false, true} {
		name := "http"
		if upgrade {
			name = "websocket"
		}
		t.Run(name, func(t *testing.T) {
			for _, target := range targets {
				t.Run(target.incoming, func(t *testing.T) {
					cloud, node := net.Pipe()
					defer node.Close()
					_ = node.SetDeadline(time.Now().Add(3 * time.Second))
					seen := make(chan string, 1)
					go func() {
						request, err := http.ReadRequest(bufio.NewReader(node))
						if err != nil {
							seen <- "read failed: " + err.Error()
							return
						}
						defer request.Body.Close()
						seen <- request.RequestURI
						// A non-upgrade response exercises WS handshake forwarding
						// without needing another WebSocket peer for each path.
						_, _ = io.WriteString(node, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
					}()
					handler := NewHandler(nodeStoreStub{node: store.Node{Status: "active"}}, registryStub{
						open: func(_ context.Context, nodeID string) (net.Conn, error) {
							if nodeID != "node1" {
								t.Errorf("node ID = %q", nodeID)
							}
							return cloud, nil
						},
					}, time.Second, time.Second, "https")
					r := httptest.NewRequest(http.MethodGet, target.incoming, nil)
					w := httptest.NewRecorder()
					var err error
					if upgrade {
						r.Header.Set("Connection", "Upgrade")
						r.Header.Set("Upgrade", "websocket")
						err = handler.ServeWebSocket(w, r, 1024)
					} else {
						err = handler.ServeHTTP(w, r)
					}
					if err != nil {
						t.Fatal(err)
					}
					if got := <-seen; got != target.want {
						t.Fatalf("Node request target = %q, want %q", got, target.want)
					}
				})
			}
		})
	}
}

func TestParseNodeRouteRejectsAmbiguousOrMalformedEscapes(t *testing.T) {
	for _, path := range []string{"/n/node%2Fother/api", "/n/%/api", "/n/node/api/%zz"} {
		if _, _, err := parseNodeRoute(path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
}
