package gateway

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"

	"mindfs-cloud/internal/connector"
	"mindfs-cloud/internal/store"
)

// Each client keeps one request in flight on its own established WebSocket.
// Both shared and independent nodes use real connector and yamux sessions.
func BenchmarkChatRoundTrip(b *testing.B) {
	for _, load := range []struct{ clients, nodes int }{{1, 1}, {100, 1}, {100, 100}} {
		clients := load.clients
		b.Run(fmt.Sprintf("clients=%d/nodes=%d", clients, load.nodes), func(b *testing.B) {
			urls := chatEchoServer(b, load.nodes)
			browsers := make([]*websocket.Conn, clients)
			for i := range browsers {
				ws, _, err := websocket.DefaultDialer.Dial(urls[i%len(urls)], nil)
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() { ws.Close() })
				browsers[i] = ws
			}
			start := make(chan struct{})
			var workers sync.WaitGroup
			samples := make([]time.Duration, clients*b.N)
			for i, ws := range browsers {
				payload := bytes.Repeat([]byte{byte(i)}, 256)
				workers.Go(func() {
					<-start
					ws.SetReadDeadline(time.Now().Add(time.Minute))
					ws.SetWriteDeadline(time.Now().Add(time.Minute))
					for j := range b.N {
						before := time.Now()
						if err := ws.WriteMessage(websocket.BinaryMessage, payload); err != nil {
							b.Error(err)
							return
						}
						opcode, reply, err := ws.ReadMessage()
						if err != nil || opcode != websocket.BinaryMessage || !bytes.Equal(payload, reply) {
							b.Errorf("client %d invalid echo: %v", i, err)
							return
						}
						samples[i*b.N+j] = time.Since(before)
					}
				})
			}
			b.ReportAllocs()
			b.ResetTimer()
			close(start)
			workers.Wait()
			b.StopTimer()
			slices.Sort(samples)
			b.ReportMetric(float64(len(samples))/b.Elapsed().Seconds(), "echoes/s")
			b.ReportMetric(float64(samples[(len(samples)*95-1)/100].Microseconds())/1000, "ms/p95")
			b.ReportMetric(float64(samples[(len(samples)*99-1)/100].Microseconds())/1000, "ms/p99")
		})
	}
}

func chatEchoServer(b *testing.B, nodes int) []string {
	b.Helper()
	registry := connector.NewSessionRegistry()
	handler := connector.NewHandler(nil, registry)
	gateway := NewHandler(nodeStoreStub{node: store.Node{Status: "active"}}, registry, time.Second, time.Second, "http")
	mux := http.NewServeMux()
	mux.HandleFunc("/connector/{node}", func(w http.ResponseWriter, r *http.Request) {
		handler.ServeAuthenticated(w, r, store.Node{ID: r.PathValue("node")})
	})
	mux.HandleFunc("/n/", func(w http.ResponseWriter, r *http.Request) {
		if err := gateway.ServeWebSocket(w, r, 32<<20); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
	})
	server := httptest.NewServer(mux)
	b.Cleanup(server.Close)
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	urls := make([]string, nodes)
	for i := range urls {
		nodeID := fmt.Sprintf("chat-%d", i)
		ws, _, err := websocket.DefaultDialer.Dial(url+"/connector/"+nodeID, nil)
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() { ws.Close() })
		node, err := yamux.Client(connector.NewWebSocketNetConn(ws), nil)
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() { node.Close() })
		go func() {
			for {
				stream, err := node.Accept()
				if err != nil {
					return
				}
				go echoChatFrames(stream)
			}
		}()
		deadline := time.Now().Add(time.Second)
		for !registry.Status(nodeID).Online {
			if time.Now().After(deadline) {
				b.Fatal("connector failed to register")
			}
			time.Sleep(time.Millisecond)
		}
		urls[i] = url + "/n/" + nodeID + "/chat"
	}
	return urls
}

// Use the node's wire format and separate header and payload writes so the
// echo peer stays independent of changes to the Relay's frame encoder.
func echoChatFrames(stream net.Conn) {
	defer stream.Close()
	reader := bufio.NewReader(stream)
	if _, err := http.ReadRequest(reader); err != nil {
		return
	}
	if _, err := io.WriteString(stream, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n"); err != nil {
		return
	}
	var header [6]byte
	var payload [256]byte
	for {
		if _, err := io.ReadFull(reader, header[:]); err != nil || header[0] != wsFrameData || binary.BigEndian.Uint32(header[2:]) != uint32(len(payload)) {
			return
		}
		if _, err := io.ReadFull(reader, payload[:]); err != nil {
			return
		}
		if _, err := stream.Write(header[:]); err != nil {
			return
		}
		if _, err := stream.Write(payload[:]); err != nil {
			return
		}
	}
}
