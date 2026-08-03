package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mindfs-cloud/internal/config"
)

func TestHealth(t *testing.T) {
	publicURL, err := url.Parse("http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	var tokenKey [32]byte
	copy(tokenKey[:], []byte("01234567890123456789012345678901"))
	application, err := New(testConfig(t, publicURL, tokenKey))
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

func TestBindingFlowIsIdempotentAndProtectsOwnership(t *testing.T) {
	publicURL, _ := url.Parse("https://relay.example.com")
	var tokenKey [32]byte
	copy(tokenKey[:], []byte("01234567890123456789012345678901"))
	application, err := New(testConfig(t, publicURL, tokenKey))
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

	sessionCookie, csrf := loginAdmin(t, server.URL, "admin", "secret")
	confirmBody := requestJSON(t, server.URL+"/api/bind/confirm", http.MethodPost, map[string]string{
		"code": code, "action": "confirm", "node_name": "Office Mac",
	}, map[string]string{"Cookie": sessionCookie, "X-CSRF-Token": csrf}, http.StatusOK)
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

func TestAdminAuthAndCSRFAreRequired(t *testing.T) {
	publicURL, _ := url.Parse("http://relay.example.com")
	var tokenKey [32]byte
	application, err := New(testConfig(t, publicURL, tokenKey))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	server := httptest.NewServer(application.Handler())
	defer server.Close()

	badLogin := requestJSON(t, server.URL+"/api/cloud/v1/auth/login", http.MethodPost, map[string]string{
		"username": "admin", "password": "wrong",
	}, nil, http.StatusUnauthorized)
	if badLogin["error"] != "auth_required" {
		t.Fatalf("bad login = %#v", badLogin)
	}

	code := "pc_" + base64.RawURLEncoding.EncodeToString([]byte("binding-code-abcdef"))
	_ = pollBind(t, server.URL, code, "device-1")
	requestJSON(t, server.URL+"/api/bind/confirm", http.MethodPost, map[string]string{
		"code": code, "action": "confirm", "node_name": "Node",
	}, nil, http.StatusUnauthorized)
	sessionCookie, _ := loginAdmin(t, server.URL, "admin", "secret")
	denied := requestJSON(t, server.URL+"/api/bind/confirm", http.MethodPost, map[string]string{
		"code": code, "action": "confirm", "node_name": "Node",
	}, map[string]string{"Cookie": sessionCookie, "X-CSRF-Token": "bad"}, http.StatusForbidden)
	if denied["error"] != "access_denied" {
		t.Fatalf("CSRF response = %#v", denied)
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
	firstApp, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	firstServer := httptest.NewServer(firstApp.Handler())
	code := "pc_" + base64.RawURLEncoding.EncodeToString([]byte("restart-code-123456"))
	_ = pollBind(t, firstServer.URL, code, "device-1")
	sessionCookie, csrf := loginAdmin(t, firstServer.URL, "admin", "secret")
	_ = requestJSON(t, firstServer.URL+"/api/bind/confirm", http.MethodPost, map[string]string{
		"code": code, "action": "confirm", "node_name": "Restart Node",
	}, map[string]string{"Cookie": sessionCookie, "X-CSRF-Token": csrf}, http.StatusOK)
	before := pollBind(t, firstServer.URL, code, "device-1")
	firstServer.Close()
	if err := firstApp.Close(); err != nil {
		t.Fatal(err)
	}

	secondApp, err := New(cfg)
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
	application, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	server := httptest.NewServer(application.Handler())
	defer server.Close()
	sessionCookie, csrf := loginAdmin(t, server.URL, "admin", "secret")

	waitingCode := "pc_" + base64.RawURLEncoding.EncodeToString([]byte("waiting-code-123456"))
	waiting := requestGETJSON(t, server.URL+"/api/bind/status?code="+url.QueryEscape(waitingCode), sessionCookie, http.StatusOK)
	if waiting["status"] != "waiting_for_device" || waiting["device_seen"] != false {
		t.Fatalf("waiting status = %#v", waiting)
	}

	rejectedCode := "pc_" + base64.RawURLEncoding.EncodeToString([]byte("rejected-code-12345"))
	_ = pollBind(t, server.URL, rejectedCode, "device-1")
	_ = requestJSON(t, server.URL+"/api/bind/confirm", http.MethodPost, map[string]string{
		"code": rejectedCode, "action": "reject",
	}, map[string]string{"Cookie": sessionCookie, "X-CSRF-Token": csrf}, http.StatusOK)
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
	}, map[string]string{"Cookie": sessionCookie, "X-CSRF-Token": csrf}, http.StatusConflict)
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
	if application, err := New(cfg); err == nil {
		_ = application.Close()
		t.Fatal("New() succeeded with an unusable data directory")
	}
}

func TestSQLiteDoesNotStorePlainDeviceTokenOrAdminPassword(t *testing.T) {
	publicURL, _ := url.Parse("http://relay.example.com")
	var tokenKey [32]byte
	dataDir := t.TempDir()
	cfg := testConfig(t, publicURL, tokenKey)
	cfg.DataDir = dataDir
	application, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler())
	code := "pc_" + base64.RawURLEncoding.EncodeToString([]byte("storage-code-123456"))
	_ = pollBind(t, server.URL, code, "device-1")
	sessionCookie, csrf := loginAdmin(t, server.URL, "admin", "secret")
	_ = requestJSON(t, server.URL+"/api/bind/confirm", http.MethodPost, map[string]string{
		"code": code, "action": "confirm", "node_name": "Storage Node",
	}, map[string]string{"Cookie": sessionCookie, "X-CSRF-Token": csrf}, http.StatusOK)
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
	for _, secret := range []string{deviceToken, "secret"} {
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
		PublicURL:         publicURL,
		DataDir:           t.TempDir(),
		AssetsDir:         assetsDir,
		AdminUsername:     "admin",
		AdminPassword:     "secret",
		TokenKey:          tokenKey,
		BindTTL:           10 * time.Minute,
		AdminSessionTTL:   12 * time.Hour,
		StreamOpenTimeout: 10 * time.Second,
		HeaderTimeout:     30 * time.Second,
		MaxWSMessageBytes: 32 << 20,
	}
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

func loginAdmin(t *testing.T, baseURL, username, password string) (string, string) {
	t.Helper()
	reqBody, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/cloud/v1/auth/login", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
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
		t.Fatalf("login status=%d body=%#v", resp.StatusCode, body)
	}
	csrf, _ := body["csrf_token"].(string)
	if csrf == "" {
		t.Fatalf("login response = %#v", body)
	}
	if len(resp.Cookies()) == 0 {
		t.Fatal("login did not set session cookie")
	}
	return resp.Cookies()[0].String(), csrf
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
