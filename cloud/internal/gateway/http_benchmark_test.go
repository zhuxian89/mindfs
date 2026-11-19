package gateway

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func BenchmarkCloneRequest(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/n/node/api", nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", "node_auth=value; theme=dark")
	req.Header.Set("User-Agent", "relay-benchmark")
	b.ReportAllocs()
	for b.Loop() {
		cloneRequest(req, "/api", "https", false)
	}
}

type discardResponse struct{ header http.Header }

func (w discardResponse) Header() http.Header       { return w.header }
func (discardResponse) WriteHeader(int)             {}
func (discardResponse) Write(p []byte) (int, error) { return len(p), nil }

func BenchmarkWriteHTTPResponse(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/n/node/api", nil)
	for _, size := range []int{53, 64 << 10} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			raw := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", size) + strings.Repeat("x", size)
			source := strings.NewReader(raw)
			reader := bufio.NewReader(source)
			dst := discardResponse{make(http.Header)}
			b.ReportAllocs()
			for b.Loop() {
				source.Reset(raw)
				reader.Reset(source)
				response, err := http.ReadResponse(reader, req)
				if err != nil {
					b.Fatal(err)
				}
				writeHTTPResponse(dst, req, response)
				if err := response.Body.Close(); err != nil {
					b.Fatal(err)
				}
				clear(dst.header)
			}
		})
	}
}
