package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBrowserAuthStatusUsesNodeAuthMode(t *testing.T) {
	application := newBrowserTestApp(t)
	recorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["auth_required"] != false || payload["access_mode"] != "node_auth" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestBrowserLoginRedirectsOnlyToLocalNodeRoutes(t *testing.T) {
	application := newBrowserTestApp(t)
	safeTarget := "/n/nnode1/?root=project&session=session-1"
	tests := []struct {
		name string
		next string
		want string
	}{
		{name: "node", next: safeTarget, want: safeTarget},
		{name: "absolute external", next: "https://evil.example/n/node/", want: "/nodes"},
		{name: "protocol relative", next: "//evil.example/n/node/", want: "/nodes"},
		{name: "non node path", next: "/bind", want: "/nodes"},
		{name: "backslash", next: `/n/node\\@evil.example/`, want: "/nodes"},
		{name: "encoded backslash", next: `/n/%5cevil.example/`, want: "/nodes"},
		{name: "empty", next: "", want: "/nodes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := "/login"
			if test.next != "" {
				target += "?next=" + url.QueryEscape(test.next)
			}
			recorder := httptest.NewRecorder()
			application.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
			if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != test.want {
				t.Fatalf("response = %d location=%q", recorder.Code, recorder.Header().Get("Location"))
			}
		})
	}
}

func TestBrowserRecoveryRoutes(t *testing.T) {
	application := newBrowserTestApp(t)

	root := httptest.NewRecorder()
	application.Handler().ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/", nil))
	if root.Code != http.StatusSeeOther || root.Header().Get("Location") != "/nodes" {
		t.Fatalf("root response = %d location=%q", root.Code, root.Header().Get("Location"))
	}

	nodes := httptest.NewRecorder()
	application.Handler().ServeHTTP(nodes, httptest.NewRequest(http.MethodGet, "/nodes", nil))
	if nodes.Code != http.StatusOK || !strings.Contains(nodes.Body.String(), "mindfs_launcher_nodes") {
		t.Fatalf("nodes response = %d body=%q", nodes.Code, nodes.Body.String())
	}
	if !strings.Contains(nodes.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("Content-Security-Policy = %q", nodes.Header().Get("Content-Security-Policy"))
	}
}

func newBrowserTestApp(t *testing.T) *App {
	t.Helper()
	publicURL, err := url.Parse("https://relay.example.com")
	if err != nil {
		t.Fatal(err)
	}
	var tokenKey [32]byte
	copy(tokenKey[:], []byte("01234567890123456789012345678901"))
	application, err := New(testConfig(t, publicURL, tokenKey))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	return application
}
