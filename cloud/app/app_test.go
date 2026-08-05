package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mindfs-cloud/internal/config"
	"mindfs-cloud/internal/identity"
)

type testMailSender struct {
	mu    sync.Mutex
	codes map[string]string
}

func (s *testMailSender) SendVerificationCode(_ context.Context, email string, purpose identity.VerificationPurpose, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.codes == nil {
		s.codes = make(map[string]string)
	}
	s.codes[email+":"+string(purpose)] = code
	return nil
}

func (s *testMailSender) code(t *testing.T, email string, purpose identity.VerificationPurpose) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	code := s.codes[email+":"+string(purpose)]
	if code == "" {
		t.Fatalf("no code for %s %s", email, purpose)
	}
	return code
}

func TestHealth(t *testing.T) {
	publicURL, err := url.Parse("http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	var tokenKey [32]byte
	copy(tokenKey[:], []byte("01234567890123456789012345678901"))
	application, err := newTestApp(t, testConfig(t, publicURL, tokenKey))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	recorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestBindPageWaitsForConnectorBeforeRevealingNodeLink(t *testing.T) {
	publicURL, _ := url.Parse("https://relay.example.com")
	var tokenKey [32]byte
	application, err := newTestApp(t, testConfig(t, publicURL, tokenKey))
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()

	recorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/bind?code=pc_test", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, contract := range []string{
		"waitForNodeOnline(body.node_id,body.node_url)",
		"fetch('/api/nodes',{cache:'no-store'})",
		"node&&node.status==='online'",
		"Binding confirmed. Waiting for the node to connect...",
		"Binding confirmed. The node is still connecting. Keep this page open.",
	} {
		if !strings.Contains(body, contract) {
			t.Fatalf("bind page missing %q", contract)
		}
	}
	if count := strings.Count(body, "nodeLink.classList.remove('hidden')"); count != 1 {
		t.Fatalf("node link reveal count = %d, want 1 online-gated reveal", count)
	}
}

func TestBindingFlowIsIdempotentAndProtectsOwnership(t *testing.T) {
	publicURL, _ := url.Parse("https://relay.example.com")
	var tokenKey [32]byte
	copy(tokenKey[:], []byte("01234567890123456789012345678901"))
	application, err := newTestApp(t, testConfig(t, publicURL, tokenKey))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	server := httptest.NewServer(application.Handler())
	defer server.Close()

	code := "pc_" + base64.RawURLEncoding.EncodeToString([]byte("binding-code-123456"))
	first := pollBind(t, server.URL, code, "device-1")
	if first["status"] != "pending" || first["next_poll_after_ms"].(float64) <= 0 {
		t.Fatalf("first poll = %#v", first)
	}

	sessionCookie := sessionCookieHeader(registerTestSession(t, application, "user@qq.com", "relay-password"))
	confirmBody := requestJSON(t, server.URL+"/api/bind/confirm", http.MethodPost, map[string]string{
		"code": code, "action": "confirm", "node_name": "Office Mac",
	}, map[string]string{"Cookie": sessionCookie, "Origin": application.config.PublicURL.String()}, http.StatusOK)
	if confirmBody["status"] != "confirmed" || !strings.Contains(confirmBody["node_url"].(string), "/n/n") {
		t.Fatalf("confirm = %#v", confirmBody)
	}

	confirmed1 := pollBind(t, server.URL, code, "device-1")
	confirmed2 := pollBind(t, server.URL, code, "device-1")
	encoded1, _ := json.Marshal(confirmed1)
	encoded2, _ := json.Marshal(confirmed2)
	if !bytes.Equal(encoded1, encoded2) {
		t.Fatalf("confirmed polls differ: %s != %s", encoded1, encoded2)
	}
	if confirmed1["status"] != "confirmed" || confirmed1["endpoint"] != "wss://relay.example.com/ws/connector" {
		t.Fatalf("confirmed poll = %#v", confirmed1)
	}
	claimed := pollBind(t, server.URL, code, "device-2")
	if len(claimed) != 1 || claimed["status"] != "claimed" {
		t.Fatalf("claimed poll leaked credentials: %#v", claimed)
	}
}

func TestUserSessionAndSameOriginAreRequired(t *testing.T) {
	publicURL, _ := url.Parse("http://relay.example.com")
	var tokenKey [32]byte
	application, err := newTestApp(t, testConfig(t, publicURL, tokenKey))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	server := httptest.NewServer(application.Handler())
	defer server.Close()

	legacyRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/api/cloud/v1/auth/login", strings.NewReader(`{"username":"admin","password":"secret"}`))
	legacyResponse, err := http.DefaultClient.Do(legacyRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = legacyResponse.Body.Close()
	if legacyResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("legacy login status = %d", legacyResponse.StatusCode)
	}

	code := "pc_" + base64.RawURLEncoding.EncodeToString([]byte("binding-code-abcdef"))
	_ = pollBind(t, server.URL, code, "device-1")
	requestJSON(t, server.URL+"/api/bind/confirm", http.MethodPost, map[string]string{
		"code": code, "action": "confirm", "node_name": "Node",
	}, nil, http.StatusUnauthorized)
	sessionCookie := sessionCookieHeader(registerTestSession(t, application, "user@qq.com", "relay-password"))
	denied := requestJSON(t, server.URL+"/api/bind/confirm", http.MethodPost, map[string]string{
		"code": code, "action": "confirm", "node_name": "Node",
	}, map[string]string{"Cookie": sessionCookie, "Origin": "https://attacker.example"}, http.StatusForbidden)
	if denied["error"] != "forbidden" {
		t.Fatalf("Origin response = %#v", denied)
	}
	if got := pollBind(t, server.URL, code, "device-1"); got["status"] != "pending" {
		t.Fatalf("challenge changed after denied confirm: %#v", got)
	}
}

func TestBindingSurvivesApplicationRestart(t *testing.T) {
	publicURL, _ := url.Parse("https://relay.example.com")
	var tokenKey [32]byte
	copy(tokenKey[:], []byte("01234567890123456789012345678901"))
	dataDir := t.TempDir()
	cfg := testConfig(t, publicURL, tokenKey)
	cfg.DataDir = dataDir
	firstApp, err := newTestApp(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	firstServer := httptest.NewServer(firstApp.Handler())
	code := "pc_" + base64.RawURLEncoding.EncodeToString([]byte("restart-code-123456"))
	_ = pollBind(t, firstServer.URL, code, "device-1")
	sessionCookie := sessionCookieHeader(registerTestSession(t, firstApp, "user@qq.com", "relay-password"))
	_ = requestJSON(t, firstServer.URL+"/api/bind/confirm", http.MethodPost, map[string]string{
		"code": code, "action": "confirm", "node_name": "Restart Node",
	}, map[string]string{"Cookie": sessionCookie, "Origin": firstApp.config.PublicURL.String()}, http.StatusOK)
	before := pollBind(t, firstServer.URL, code, "device-1")
	firstServer.Close()
	if err := firstApp.Close(); err != nil {
		t.Fatal(err)
	}

	secondApp, err := newTestApp(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer secondApp.Close()
	secondServer := httptest.NewServer(secondApp.Handler())
	defer secondServer.Close()
	after := pollBind(t, secondServer.URL, code, "device-1")
	for _, key := range []string{"device_token", "node_id", "node_name", "endpoint"} {
		if before[key] != after[key] {
			t.Fatalf("%s changed across restart: %#v != %#v", key, before[key], after[key])
		}
	}
}

func TestBindingWaitingRejectAndExpiryStates(t *testing.T) {
	publicURL, _ := url.Parse("http://relay.example.com")
	var tokenKey [32]byte
	cfg := testConfig(t, publicURL, tokenKey)
	cfg.BindTTL = 200 * time.Millisecond
	application, err := newTestApp(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	server := httptest.NewServer(application.Handler())
	defer server.Close()
	sessionCookie := sessionCookieHeader(registerTestSession(t, application, "user@qq.com", "relay-password"))

	waitingCode := "pc_" + base64.RawURLEncoding.EncodeToString([]byte("waiting-code-123456"))
	waiting := requestGETJSON(t, server.URL+"/api/bind/status?code="+url.QueryEscape(waitingCode), sessionCookie, http.StatusOK)
	if waiting["status"] != "waiting_for_device" || waiting["device_seen"] != false {
		t.Fatalf("waiting status = %#v", waiting)
	}

	rejectedCode := "pc_" + base64.RawURLEncoding.EncodeToString([]byte("rejected-code-12345"))
	_ = pollBind(t, server.URL, rejectedCode, "device-1")
	_ = requestJSON(t, server.URL+"/api/bind/confirm", http.MethodPost, map[string]string{
		"code": rejectedCode, "action": "reject",
	}, map[string]string{"Cookie": sessionCookie, "Origin": application.config.PublicURL.String()}, http.StatusOK)
	if rejected := pollBind(t, server.URL, rejectedCode, "device-1"); rejected["status"] != "revoked" {
		t.Fatalf("rejected poll = %#v", rejected)
	}

	expiredCode := "pc_" + base64.RawURLEncoding.EncodeToString([]byte("expired-code-123456"))
	_ = pollBind(t, server.URL, expiredCode, "device-1")
	time.Sleep(250 * time.Millisecond)
	if expired := pollBind(t, server.URL, expiredCode, "device-1"); expired["status"] != "expired" {
		t.Fatalf("expired poll = %#v", expired)
	}
	expiredConfirm := requestJSON(t, server.URL+"/api/bind/confirm", http.MethodPost, map[string]string{
		"code": expiredCode, "action": "confirm", "node_name": "Late Node",
	}, map[string]string{"Cookie": sessionCookie, "Origin": application.config.PublicURL.String()}, http.StatusConflict)
	if expiredConfirm["error"] != "bind_expired" {
		t.Fatalf("expired confirm = %#v", expiredConfirm)
	}
}

func TestNewFailsWhenSQLitePathIsUnavailable(t *testing.T) {
	publicURL, _ := url.Parse("http://relay.example.com")
	var tokenKey [32]byte
	file := t.TempDir() + "/not-a-directory"
	if err := os.WriteFile(file, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(t, publicURL, tokenKey)
	cfg.DataDir = file
	if application, err := newTestApp(t, cfg); err == nil {
		_ = application.Close()
		t.Fatal("New() succeeded with an unusable data directory")
	}
}

func TestSQLiteDoesNotStorePlainDeviceTokenOrRelayPassword(t *testing.T) {
	publicURL, _ := url.Parse("http://relay.example.com")
	var tokenKey [32]byte
	dataDir := t.TempDir()
	cfg := testConfig(t, publicURL, tokenKey)
	cfg.DataDir = dataDir
	application, err := newTestApp(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler())
	code := "pc_" + base64.RawURLEncoding.EncodeToString([]byte("storage-code-123456"))
	_ = pollBind(t, server.URL, code, "device-1")
	sessionCookie := sessionCookieHeader(registerTestSession(t, application, "user@qq.com", "relay-password"))
	_ = requestJSON(t, server.URL+"/api/bind/confirm", http.MethodPost, map[string]string{
		"code": code, "action": "confirm", "node_name": "Storage Node",
	}, map[string]string{"Cookie": sessionCookie, "Origin": application.config.PublicURL.String()}, http.StatusOK)
	credentials := pollBind(t, server.URL, code, "device-1")
	deviceToken := credentials["device_token"].(string)
	server.Close()
	if err := application.Close(); err != nil {
		t.Fatal(err)
	}
	database, err := os.ReadFile(dataDir + "/mindfs-cloud.db")
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{deviceToken, "relay-password"} {
		if bytes.Contains(database, []byte(secret)) {
			t.Fatalf("database contains plaintext secret %q", secret)
		}
	}
}

func TestPublicWebSocketURL(t *testing.T) {
	publicURL, _ := url.Parse("https://relay.example.com/base")
	got := publicWebSocketURL(config.Config{PublicURL: publicURL})
	if got != "wss://relay.example.com/base/ws/connector" {
		t.Fatalf("publicWebSocketURL() = %q", got)
	}
}

func testConfig(t *testing.T, publicURL *url.URL, tokenKey [32]byte) config.Config {
	t.Helper()
	assetsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(assetsDir, "index.html"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(assetsDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	return config.Config{
		PublicURL:      publicURL,
		DataDir:        t.TempDir(),
		AssetsDir:      assetsDir,
		TokenKey:       tokenKey,
		BindTTL:        10 * time.Minute,
		BootstrapEmail: "sender@qq.com",
		SMTP: identity.SMTPConfig{
			Host: "smtp.qq.com", Port: 465, TLS: true,
			From: "sender@qq.com", Username: "sender@qq.com", Password: "smtp-secret",
		},
		StreamOpenTimeout: 10 * time.Second,
		HeaderTimeout:     30 * time.Second,
		MaxWSMessageBytes: 32 << 20,
	}
}

func newTestApp(t *testing.T, cfg config.Config) (*App, error) {
	t.Helper()
	mail := &testMailSender{}
	passwords := identity.NewPasswordHasherWithParams(identity.PasswordParams{
		Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	return newApp(cfg, mail, passwords)
}

func registerTestSession(t *testing.T, application *App, email, password string) string {
	t.Helper()
	if _, err := application.identity.RequestRegistrationCode(context.Background(), email, "test-source"); err != nil {
		t.Fatal(err)
	}
	mail := application.mail.(*testMailSender)
	code := mail.code(t, email, identity.PurposeRegister)
	_, token, err := application.identity.Register(context.Background(), email, password, code)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func sessionCookieHeader(token string) string {
	return userSessionCookie + "=" + token
}

func pollBind(t *testing.T, baseURL, code, deviceID string) map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/bind/poll?code="+url.QueryEscape(code), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-MindFS-Device-ID", deviceID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("poll status=%d body=%#v", resp.StatusCode, body)
	}
	return body
}

func requestJSON(t *testing.T, endpoint, method string, input any, headers map[string]string, wantStatus int) map[string]any {
	t.Helper()
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, endpoint, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s status=%d want=%d body=%#v", method, endpoint, resp.StatusCode, wantStatus, body)
	}
	return body
}

func requestGETJSON(t *testing.T, endpoint, cookie string, wantStatus int) map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s status=%d want=%d body=%#v", endpoint, resp.StatusCode, wantStatus, body)
	}
	return body
}
