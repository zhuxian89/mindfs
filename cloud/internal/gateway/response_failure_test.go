package gateway

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mindfs-cloud/internal/store"
)

func TestTruncatedResponsesAbortPublicTransfer(t *testing.T) {
	for _, size := range []int{3, 65536} {
		for _, upgrade := range []bool{false, true} {
			t.Run(fmt.Sprintf("size=%d/upgrade=%v", size, upgrade), func(t *testing.T) {
				relay, node := net.Pipe()
				go func() {
					defer node.Close()
					if _, err := http.ReadRequest(bufio.NewReader(node)); err != nil {
						return
					}
					fmt.Fprintf(node, "HTTP/1.1 200 OK\r\nContent-Type: application/octet-stream\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n%s\r\n", size, strings.Repeat("a", size))
					// Deliberately omit the terminating zero chunk.
				}()
				h := NewHandler(nodeStoreStub{node: store.Node{Status: "active"}}, registryStub{open: func(context.Context, string) (net.Conn, error) { return relay, nil }}, time.Second, time.Second, "http")
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if upgrade {
						_ = h.ServeWebSocket(w, r, 1024)
					} else {
						_ = h.ServeHTTP(w, r)
					}
				}))
				defer server.Close()
				response, err := server.Client().Get(server.URL + "/n/nnode1/download")
				if err == nil {
					defer response.Body.Close()
					_, err = io.Copy(io.Discard, response.Body)
				}
				if err == nil {
					t.Fatal("truncated upstream was reported as a complete response")
				}
			})
		}
	}
}

func TestGatewaySeparatesControlAndNodeCookies(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		r := httptest.NewRequest("GET", "https://relay.example/n/nnode1/", nil)
		r.Header.Add("Cookie", "mindfs_cloud_session=secret; node_auth=keep")
		r.Header.Add("Cookie", "__Host-mindfs_cloud_session=secret; other=value")
		outbound := cloneRequest(r, "/", "https", upgrade)
		if got := outbound.Header.Get("Cookie"); got != "node_auth=keep; other=value" {
			t.Fatalf("forwarded cookies=%q", got)
		}
		if r.Header.Get("Cookie") != "mindfs_cloud_session=secret; node_auth=keep" {
			t.Fatal("source request mutated")
		}
	}
	header := http.Header{}
	for _, cookie := range []string{
		"mindfs_cloud_session=forged; Path=/", "__Host-mindfs_cloud_session=forged; Secure; Path=/",
		"node_auth=keep; Path=/; Domain=example.com; HttpOnly; Secure; SameSite=Lax",
		"node_api=keep; Path=/api", "bad cookie",
	} {
		header.Add("Set-Cookie", cookie)
	}
	header.Set("Clear-Site-Data", "\"cookies\"")
	header.Set("Service-Worker-Allowed", "/")
	sanitizeNodeResponseHeaders(header, "nnode1")
	cookies := (&http.Response{Header: header}).Cookies()
	if len(cookies) != 2 || cookies[0].Path != "/n/nnode1/" || cookies[1].Path != "/n/nnode1/api" {
		t.Fatalf("node cookies=%v", cookies)
	}
	if cookies[0].Domain != "" || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatal("cookie isolation lost attributes")
	}
	if header.Get("Clear-Site-Data") != "" || header.Get("Service-Worker-Allowed") != "" {
		t.Fatal("node origin-wide headers escaped")
	}
}
