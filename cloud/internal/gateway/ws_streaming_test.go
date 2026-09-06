package gateway

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"mindfs-cloud/internal/store"
)

type repeatedByte byte

func (value repeatedByte) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(value)
	}
	return len(p), nil
}

func writeTestFrameHeader(w io.Writer, opcode int, size int) error {
	header := [6]byte{wsFrameData, byte(opcode)}
	binary.BigEndian.PutUint32(header[2:], uint32(size))
	_, err := w.Write(header[:])
	return err
}

func openStreamingGateway(t testing.TB, limit int64, send func(net.Conn) error) (*websocket.Conn, <-chan error, <-chan struct{}) {
	t.Helper()
	cloud, node := net.Pipe()
	nodeDone := make(chan error, 1)
	go func() {
		defer node.Close()
		if _, err := http.ReadRequest(bufio.NewReader(node)); err != nil {
			nodeDone <- err
			return
		}
		if _, err := io.WriteString(node, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n"); err != nil {
			nodeDone <- err
			return
		}
		nodeDone <- send(node)
	}()
	handler := NewHandler(nodeStoreStub{node: store.Node{Status: "active"}}, registryStub{
		open: func(_ context.Context, _ string) (net.Conn, error) { return cloud, nil },
	}, time.Second, time.Second, "http")
	finished := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		if err := handler.ServeWebSocket(w, r, limit); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(func() {
		cloud.Close()
		node.Close()
		server.Close()
	})
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/n/node/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	return ws, nodeDone, finished
}

func TestNodeMessageStreamsBeforeComplete(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	const size = 2 << 20
	ws, nodeDone, _ := openStreamingGateway(t, size, func(node net.Conn) error {
		if err := writeTestFrameHeader(node, websocket.BinaryMessage, size); err != nil {
			return err
		}
		if _, err := io.CopyN(node, repeatedByte(7), 32768); err != nil {
			return err
		}
		<-release
		_, err := io.CopyN(node, repeatedByte(8), size-32768)
		return err
	})
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, reader, err := ws.NextReader()
	if err != nil {
		t.Fatal(err)
	}
	first := make([]byte, 32768)
	if _, err := io.ReadFull(reader, first); err != nil {
		t.Fatalf("first chunk waited for the complete message: %v", err)
	}
	for _, value := range first {
		if value != 7 {
			t.Fatal("first chunk corrupted")
		}
	}
	releaseOnce.Do(func() { close(release) })
	rest, err := io.ReadAll(reader)
	if err != nil || len(rest) != size-32768 {
		t.Fatalf("remaining bytes=%d err=%v", len(rest), err)
	}
	for _, value := range rest {
		if value != 8 {
			t.Fatal("second chunk corrupted")
		}
	}
	if err := <-nodeDone; err != nil {
		t.Fatal(err)
	}
}

func TestStreamingWebSocketPreservesMessageBoundaries(t *testing.T) {
	sizes := []int{0, 1, 4096, 4097, 32768, 32769, 256 << 10, 256<<10 + 1, 1 << 20, 1<<20 + 1, 32 << 20, 3}
	ws, nodeDone, _ := openStreamingGateway(t, 32<<20, func(node net.Conn) error {
		for i, size := range sizes {
			if err := writeTestFrameHeader(node, 1+i%2, size); err != nil {
				return err
			}
			if _, err := io.CopyN(node, repeatedByte('a'+i), int64(size)); err != nil {
				return err
			}
		}
		return writeWSCloseFrame(node, websocket.CloseNormalClosure, "done")
	})
	ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	buffer := make([]byte, 32768)
	for i, size := range sizes {
		opcode, reader, err := ws.NextReader()
		if err != nil || opcode != 1+i%2 {
			t.Fatalf("message %d opcode=%d err=%v", i, opcode, err)
		}
		var received int
		for {
			n, err := reader.Read(buffer)
			received += n
			for _, value := range buffer[:n] {
				if value != byte('a'+i) {
					t.Fatalf("message %d corrupted", i)
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if received != size {
			t.Fatalf("message %d size=%d want=%d", i, received, size)
		}
	}
	_, _, err := ws.ReadMessage()
	var closed *websocket.CloseError
	if !errors.As(err, &closed) || closed.Code != websocket.CloseNormalClosure || closed.Text != "done" {
		t.Fatalf("close not preserved: %v", err)
	}
	if err := <-nodeDone; err != nil {
		t.Fatal(err)
	}
}

func TestTruncatedNodeMessageNeverCompletes(t *testing.T) {
	for _, size := range []int{3, 65536, 2 << 20} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			ws, _, _ := openStreamingGateway(t, 4<<20, func(node net.Conn) error {
				if err := writeTestFrameHeader(node, websocket.BinaryMessage, size+1); err != nil {
					return err
				}
				_, err := io.CopyN(node, repeatedByte(1), int64(size))
				return err
			})
			ws.SetReadDeadline(time.Now().Add(2 * time.Second))
			if _, _, err := ws.ReadMessage(); err == nil {
				t.Fatal("truncated payload delivered as a complete message")
			}
		})
	}
}

func TestStreamingRejectsInvalidNodeHeaders(t *testing.T) {
	for _, test := range []struct {
		name                    string
		opcode, size, closeCode int
	}{
		{"oversize", websocket.BinaryMessage, 1025, websocket.CloseMessageTooBig},
		{"invalid opcode", 99, 100, websocket.CloseProtocolError},
	} {
		t.Run(test.name, func(t *testing.T) {
			ws, _, _ := openStreamingGateway(t, 1024, func(node net.Conn) error {
				return writeTestFrameHeader(node, test.opcode, test.size)
			})
			ws.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, _, err := ws.ReadMessage()
			var closed *websocket.CloseError
			if !errors.As(err, &closed) || closed.Code != test.closeCode {
				t.Fatalf("invalid frame close=%v", err)
			}
		})
	}
}

func TestStreamingReleasesNodeOnClientDisconnect(t *testing.T) {
	ws, nodeDone, finished := openStreamingGateway(t, 32<<20, func(node net.Conn) error {
		if err := writeTestFrameHeader(node, websocket.BinaryMessage, 32<<20); err != nil {
			return err
		}
		_, err := io.CopyN(node, repeatedByte(1), 32<<20)
		return err
	})
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := ws.NextReader(); err != nil {
		t.Fatal(err)
	}
	ws.Close()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("gateway retained disconnected client")
	}
	select {
	case <-nodeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("node writer remained blocked")
	}
}

func TestStreamingBoundsStalledClientWrite(t *testing.T) {
	ws, _, finished := openStreamingGateway(t, 32<<20, func(node net.Conn) error {
		if err := writeTestFrameHeader(node, websocket.BinaryMessage, 32<<20); err != nil {
			return err
		}
		_, err := io.CopyN(node, repeatedByte(1), 32<<20)
		return err
	})
	if conn, ok := ws.UnderlyingConn().(*net.TCPConn); ok {
		if err := conn.SetReadBuffer(1024); err != nil {
			t.Fatal(err)
		}
	}
	// Do not read. A blocked downstream must release the bridge at its write
	// deadline, even though the upstream is capable of sending more data.
	select {
	case <-finished:
	case <-time.After(wsBridgeWriteTimeout + 5*time.Second):
		t.Fatal("stalled receiver outlived the write bound")
	}
}

func TestStreamingConcurrentLargeMessages(t *testing.T) {
	for i := range 8 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			ws, nodeDone, _ := openStreamingGateway(t, 32<<20, func(node net.Conn) error {
				if err := writeTestFrameHeader(node, websocket.BinaryMessage, 32<<20); err != nil {
					return err
				}
				_, err := io.CopyN(node, repeatedByte(i), 32<<20)
				return err
			})
			ws.SetReadDeadline(time.Now().Add(10 * time.Second))
			_, reader, err := ws.NextReader()
			if err != nil {
				t.Fatal(err)
			}
			var total int
			buffer := make([]byte, 32768)
			for {
				n, err := reader.Read(buffer)
				total += n
				for _, value := range buffer[:n] {
					if value != byte(i) {
						t.Fatal("concurrent connection payload mixed")
					}
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if total != 32<<20 {
				t.Fatalf("received=%d", total)
			}
			if err := <-nodeDone; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func BenchmarkNodeWebSocketStreaming(b *testing.B) {
	for _, size := range []int{1 << 20, 32 << 20} {
		b.Run(fmt.Sprintf("%dMiB", size>>20), func(b *testing.B) {
			start := make(chan struct{})
			var startOnce sync.Once
			defer startOnce.Do(func() { close(start) })
			ws, nodeDone, _ := openStreamingGateway(b, 32<<20, func(node net.Conn) error {
				<-start
				buffer := make([]byte, 32768)
				for range b.N {
					if err := writeTestFrameHeader(node, websocket.BinaryMessage, size); err != nil {
						return err
					}
					if _, err := io.CopyBuffer(node, &io.LimitedReader{R: repeatedByte(1), N: int64(size)}, buffer); err != nil {
						return err
					}
				}
				return nil
			})
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()
			startOnce.Do(func() { close(start) })
			for range b.N {
				_, reader, err := ws.NextReader()
				if err != nil {
					b.Fatal(err)
				}
				if n, err := io.Copy(io.Discard, reader); err != nil || n != int64(size) {
					b.Fatalf("received=%d err=%v", n, err)
				}
			}
			b.StopTimer()
			if err := <-nodeDone; err != nil {
				b.Fatal(err)
			}
		})
	}
}
