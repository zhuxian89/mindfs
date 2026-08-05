package app

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mindfs-cloud/internal/identity"
)

func TestRelayNodeOwnershipIsolationAndBindingPayloads(t *testing.T) {
	application := newBrowserTestApp(t)
	server := httptest.NewServer(application.Handler())
	defer server.Close()
	userAToken := registerTestSession(t, application, "owner-a@qq.com", "relay-password-a")
	userBToken := registerTestSession(t, application, "owner-b@qq.com", "relay-password-b")
	userAID := browserUserID(t, application, userAToken)
	userBID := browserUserID(t, application, userBToken)
	origin := application.config.PublicURL.String()

	codeA := "pc_" + base64.RawURLEncoding.EncodeToString([]byte("owner-a-bind-code"))
	_ = pollBind(t, server.URL, codeA, "device-a")
	boundA := requestJSON(t, server.URL+"/api/bind/confirm", http.MethodPost, map[string]string{
		"code": codeA, "name": "Owner A Node",
	}, map[string]string{"Cookie": sessionCookieHeader(userAToken), "Origin": origin}, http.StatusOK)
	nodeAID := boundA["node_id"].(string)

	codeB := "pc_" + base64.RawURLEncoding.EncodeToString([]byte("owner-b-bind-code"))
	_ = pollBind(t, server.URL, codeB, "device-b")
	boundB := requestJSON(t, server.URL+"/api/bind/confirm", http.MethodPost, map[string]string{
		"code": codeB, "action": "confirm", "node_name": "Owner B Node",
	}, map[string]string{"Cookie": sessionCookieHeader(userBToken), "Origin": origin}, http.StatusOK)
	nodeBID := boundB["node_id"].(string)

	assertOwnedNodeIDs(t, application, userAToken, []string{nodeAID})
	assertOwnedNodeIDs(t, application, userBToken, []string{nodeBID})
	if node, err := application.store.GetNode(context.Background(), nodeAID); err != nil || node.OwnerUserID != userAID {
		t.Fatalf("owner A node = %#v err=%v", node, err)
	}
	if node, err := application.store.GetNode(context.Background(), nodeBID); err != nil || node.OwnerUserID != userBID {
		t.Fatalf("owner B node = %#v err=%v", node, err)
	}

	session := &trackingRelaySession{}
	application.registry.Register(nodeAID, "owner-a-connection", session)
	before := pollBind(t, server.URL, codeA, "device-a")
	requestJSON(t, server.URL+"/api/nodes/"+nodeAID, http.MethodPatch, map[string]string{"name": "Stolen"}, map[string]string{
		"Cookie": sessionCookieHeader(userBToken), "Origin": origin,
	}, http.StatusNotFound)
	requestJSON(t, server.URL+"/api/nodes/"+nodeAID, http.MethodDelete, nil, map[string]string{
		"Cookie": sessionCookieHeader(userBToken), "Origin": origin,
	}, http.StatusNotFound)
	after := pollBind(t, server.URL, codeA, "device-a")
	if before["device_token"] != after["device_token"] || session.closed || !application.registry.Status(nodeAID).Online {
		t.Fatalf("unauthorized mutation changed node state: before=%#v after=%#v closed=%v", before, after, session.closed)
	}
	claimed := requestJSON(t, server.URL+"/api/bind/confirm", http.MethodPost, map[string]string{
		"code": codeA, "name": "Other User",
	}, map[string]string{"Cookie": sessionCookieHeader(userBToken), "Origin": origin}, http.StatusConflict)
	if claimed["error"] != "bind_claimed" {
		t.Fatalf("other-user bind response = %#v", claimed)
	}
}

func TestV0BootstrapRegistrationClaimsExistingNodes(t *testing.T) {
	publicURL, _ := url.Parse("https://relay.example.com")
	var tokenKey [32]byte
	copy(tokenKey[:], []byte("01234567890123456789012345678901"))
	cfg := testConfig(t, publicURL, tokenKey)
	createLegacyRelayDatabase(t, cfg.DataDir)
	application, err := newTestApp(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()

	pending, err := application.store.GetUserByEmail(context.Background(), cfg.BootstrapEmail)
	if err != nil || pending.Status != "pending_verification" || pending.PasswordHash != "" {
		t.Fatalf("pending bootstrap = %#v err=%v", pending, err)
	}
	if _, err := application.identity.RequestRegistrationCode(context.Background(), cfg.BootstrapEmail, "bootstrap-register"); err != nil {
		t.Fatal(err)
	}
	code := application.mail.(*testMailSender).code(t, cfg.BootstrapEmail, identity.PurposeRegister)
	registered, _, err := application.identity.Register(context.Background(), cfg.BootstrapEmail, "relay-password", code)
	if err != nil {
		t.Fatal(err)
	}
	if registered.ID != pending.ID || registered.Status != "active" {
		t.Fatalf("registered bootstrap = %#v pending=%#v", registered, pending)
	}
	nodes, err := application.store.ListNodesByOwner(context.Background(), registered.ID)
	if err != nil || len(nodes) != 1 || nodes[0].ID != "legacy-node" {
		t.Fatalf("claimed nodes = %#v err=%v", nodes, err)
	}
	if node, err := application.store.AuthenticateDeviceToken(context.Background(), []byte("legacy-token-hash"), time.Now().UTC()); err != nil || node.ID != "legacy-node" {
		t.Fatalf("legacy connector token = %#v err=%v", node, err)
	}
}

func assertOwnedNodeIDs(t *testing.T, application *App, token string, want []string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	request.AddCookie(&http.Cookie{Name: userSessionCookie, Value: token})
	recorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var nodes []RelayNodePayload
	if err := decodeResponseJSON(recorder, &nodes); err != nil {
		t.Fatal(err)
	}
	if len(nodes) != len(want) {
		t.Fatalf("nodes = %#v want=%v", nodes, want)
	}
	for index := range want {
		if nodes[index].ID != want[index] {
			t.Fatalf("nodes = %#v want=%v", nodes, want)
		}
	}
}

func decodeResponseJSON(recorder *httptest.ResponseRecorder, target any) error {
	return json.NewDecoder(recorder.Body).Decode(target)
}

func createLegacyRelayDatabase(t *testing.T, dataDir string) {
	t.Helper()
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", filepath.Join(dataDir, "mindfs-cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	legacySchema := `
CREATE TABLE admin_sessions (session_hash BLOB PRIMARY KEY, csrf_hash BLOB NOT NULL, expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL, last_seen_at INTEGER NOT NULL);
CREATE TABLE bind_challenges (code_hash BLOB PRIMARY KEY, device_id TEXT NOT NULL, requested_node_name TEXT NOT NULL DEFAULT '', root_hint TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, node_id TEXT NOT NULL DEFAULT '', token_derivation_version INTEGER NOT NULL DEFAULT 1, expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL, confirmed_at INTEGER);
CREATE TABLE nodes (id TEXT PRIMARY KEY, device_id TEXT NOT NULL, name TEXT NOT NULL, status TEXT NOT NULL, access_mode TEXT NOT NULL, created_at INTEGER NOT NULL, last_seen_at INTEGER);
CREATE TABLE device_tokens (id TEXT PRIMARY KEY, node_id TEXT NOT NULL, token_hash BLOB NOT NULL UNIQUE, status TEXT NOT NULL, created_at INTEGER NOT NULL, last_used_at INTEGER);`
	if _, err := database.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := database.Exec("INSERT INTO nodes (id, device_id, name, status, access_mode, created_at) VALUES ('legacy-node', 'legacy-device', 'Legacy Node', 'active', 'node_auth', ?)", now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO bind_challenges (code_hash, device_id, status, node_id, expires_at, created_at, confirmed_at) VALUES (?, 'legacy-device', 'confirmed', 'legacy-node', ?, ?, ?)", []byte("legacy-code"), now+int64(time.Hour/time.Millisecond), now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO device_tokens (id, node_id, token_hash, status, created_at) VALUES ('legacy-token', 'legacy-node', ?, 'active', ?)", []byte("legacy-token-hash"), now); err != nil {
		t.Fatal(err)
	}
}
