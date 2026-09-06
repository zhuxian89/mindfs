package app

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type auditNodeSession struct{ seen chan *http.Request }

func (s *auditNodeSession) Open() (net.Conn, error) {
	relay, node := net.Pipe()
	go func() {
		defer node.Close()
		r, err := http.ReadRequest(bufio.NewReader(node))
		if err != nil {
			return
		}
		s.seen <- r
		io.WriteString(node, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
	}()
	return relay, nil
}

func (*auditNodeSession) Close() error { return nil }

func TestGatewayDoesNotForwardCloudSessionToAnotherNode(t *testing.T) {
	a := newBrowserTestApp(t)
	victim := registerTestSession(t, a, "victim@qq.com", "victim-password")
	ownerToken := registerTestSession(t, a, "node-owner@qq.com", "owner-password")
	owner, _, err := a.identity.Authenticate(context.Background(), ownerToken)
	if err != nil {
		t.Fatal(err)
	}
	code := "pc_" + base64.RawURLEncoding.EncodeToString([]byte("audit-bind-code-1234"))
	if _, err := a.binding.Poll(context.Background(), code, "audit-node-device"); err != nil {
		t.Fatal(err)
	}
	confirmation, err := a.binding.Confirm(context.Background(), owner.ID, code, "test node")
	if err != nil {
		t.Fatal(err)
	}
	session := &auditNodeSession{seen: make(chan *http.Request, 1)}
	a.registry.Register(confirmation.NodeID, "audit", session)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	req, _ := http.NewRequest("GET", server.URL+"/n/"+confirmation.NodeID+"/", nil)
	req.AddCookie(&http.Cookie{Name: userSessionCookie, Value: victim})
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	seen := <-session.seen
	if _, err := seen.Cookie(userSessionCookie); err != http.ErrNoCookie {
		t.Fatal("cloud session leaked to node")
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("node response status=%d", response.StatusCode)
	}
}

func TestSpoofedSourceCannotBypassLoginRateLimit(t *testing.T) {
	for _, spoof := range []bool{false, true} {
		t.Run(fmt.Sprint(spoof), func(t *testing.T) {
			a := newBrowserTestApp(t)
			blocked := 0
			for i := 0; i < 31; i++ {
				req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(fmt.Sprintf(`{"email":"audit%d@qq.com","password":"invalid-password"}`, i)))
				req.RemoteAddr = "192.0.2.1:45678"
				req.Header.Set("Origin", a.config.PublicURL.String())
				req.Header.Set("X-Forwarded-For", "192.0.2.1")
				if spoof {
					req.Header.Set("CF-Connecting-IP", fmt.Sprintf("198.51.100.%d", i+1))
				}
				recorder := httptest.NewRecorder()
				a.Handler().ServeHTTP(recorder, req)
				if recorder.Code == 429 {
					blocked++
				} else if recorder.Code != 401 {
					t.Fatalf("unexpected status %d", recorder.Code)
				}
			}
			if blocked != 1 {
				t.Fatalf("spoof=%v blocked=%d", spoof, blocked)
			}
			t.Logf("same peer, 31 distinct emails, spoof=%v, rate-limited=%d", spoof, blocked)
		})
	}
}
