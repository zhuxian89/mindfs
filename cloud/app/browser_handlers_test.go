package app

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"mindfs-cloud/internal/store"
)

func TestBrowserAuthStatusRequiresUserSession(t *testing.T) {
	application := newBrowserTestApp(t)

	unauthorized := httptest.NewRecorder()
	application.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil))
	if unauthorized.Code != http.StatusUnauthorized || !strings.Contains(unauthorized.Body.String(), `"error":"unauthorized"`) {
		t.Fatalf("unauthorized response = %d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	sessionToken := browserUserSession(t, application)
	request := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	request.AddCookie(&http.Cookie{Name: userSessionCookie, Value: sessionToken})
	recorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["id"] == "" || payload["email"] != "user@qq.com" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestBrowserLoginUsesSafeRelayTargets(t *testing.T) {
	application := newBrowserTestApp(t)
	sessionToken := browserUserSession(t, application)
	safeTarget := "/n/nnode1/?root=project&session=session-1"
	tests := []struct {
		name string
		next string
		want string
	}{
		{name: "nodes", next: "/nodes", want: "/nodes"},
		{name: "node", next: safeTarget, want: safeTarget},
		{name: "absolute external", next: "https://evil.example/n/node/", want: "/nodes"},
		{name: "protocol relative", next: "//evil.example/n/node/", want: "/nodes"},
		{name: "bind", next: "/bind?code=pc_test", want: "/bind?code=pc_test"},
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
			request := httptest.NewRequest(http.MethodGet, target, nil)
			request.AddCookie(&http.Cookie{Name: userSessionCookie, Value: sessionToken})
			recorder := httptest.NewRecorder()
			application.Handler().ServeHTTP(recorder, request)
			if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != test.want {
				t.Fatalf("response = %d location=%q", recorder.Code, recorder.Header().Get("Location"))
			}
		})
	}
}

func TestBrowserRecoveryRoutesRequireLogin(t *testing.T) {
	application := newBrowserTestApp(t)

	root := httptest.NewRecorder()
	application.Handler().ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/", nil))
	if root.Code != http.StatusSeeOther || root.Header().Get("Location") != "/nodes" {
		t.Fatalf("root response = %d location=%q", root.Code, root.Header().Get("Location"))
	}

	nodes := httptest.NewRecorder()
	application.Handler().ServeHTTP(nodes, httptest.NewRequest(http.MethodGet, "/nodes", nil))
	if nodes.Code != http.StatusSeeOther || nodes.Header().Get("Location") != "/login?next=%2Fnodes" {
		t.Fatalf("nodes response = %d location=%q", nodes.Code, nodes.Header().Get("Location"))
	}

	login := httptest.NewRecorder()
	application.Handler().ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/login?next=%2Fnodes", nil))
	if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), `id="login-form"`) || !strings.Contains(login.Body.String(), "/api/auth/register") {
		t.Fatalf("login response = %d body=%q", login.Code, login.Body.String())
	}
	if !strings.Contains(login.Header().Get("Content-Security-Policy"), "connect-src 'self'") {
		t.Fatalf("login Content-Security-Policy = %q", login.Header().Get("Content-Security-Policy"))
	}

	sessionToken := browserUserSession(t, application)
	request := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	request.AddCookie(&http.Cookie{Name: userSessionCookie, Value: sessionToken})
	authorized := httptest.NewRecorder()
	application.Handler().ServeHTTP(authorized, request)
	if authorized.Code != http.StatusOK || !strings.Contains(authorized.Body.String(), `api("/api/nodes")`) {
		t.Fatalf("authorized nodes response = %d body=%q", authorized.Code, authorized.Body.String())
	}
	for _, contract := range []string{"/n/", "MindFSLauncherNodeSync", "节点当前离线。", "setInterval(load,15000)"} {
		if !strings.Contains(authorized.Body.String(), contract) {
			t.Fatalf("authorized nodes page missing %q", contract)
		}
	}
}

func TestBrowserLogoutClearsSessionCookie(t *testing.T) {
	application := newBrowserTestApp(t)
	sessionToken := browserUserSession(t, application)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	request.Header.Set("Origin", application.config.PublicURL.String())
	request.AddCookie(&http.Cookie{Name: userSessionCookie, Value: sessionToken})
	recorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"success":true`) {
		t.Fatalf("logout response = %d body=%s", recorder.Code, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != userSessionCookie || cookies[0].MaxAge >= 0 {
		t.Fatalf("logout cookies = %#v", cookies)
	}
}

func TestRelayNodesListRequiresSessionAndReturnsClientPayload(t *testing.T) {
	application := newBrowserTestApp(t)

	unauthorized := httptest.NewRecorder()
	application.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/nodes", nil))
	if unauthorized.Code != http.StatusUnauthorized || !strings.Contains(unauthorized.Body.String(), `"error":"unauthorized"`) {
		t.Fatalf("unauthorized response = %d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	sessionToken := browserUserSession(t, application)
	ownerUserID := browserUserID(t, application, sessionToken)
	emptyRequest := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	emptyRequest.AddCookie(&http.Cookie{Name: userSessionCookie, Value: sessionToken})
	empty := httptest.NewRecorder()
	application.Handler().ServeHTTP(empty, emptyRequest)
	if empty.Code != http.StatusOK || strings.TrimSpace(empty.Body.String()) != "[]" {
		t.Fatalf("empty nodes response = %d body=%s", empty.Code, empty.Body.String())
	}

	now := time.Date(2026, 8, 4, 11, 0, 0, 0, time.UTC)
	recent := now.Add(time.Hour)
	for index, node := range []store.Node{
		{ID: "offline-never", DeviceID: "device-offline-never", Name: "Offline never", Status: "active", AccessMode: "node_auth", CreatedAt: now.Add(4 * time.Second)},
		{ID: "online-b", DeviceID: "device-online-b", Name: "Online B", Status: "active", AccessMode: "node_auth", CreatedAt: now.Add(2 * time.Second), LastSeenAt: &now},
		{ID: "online-recent", DeviceID: "device-online-recent", Name: "Online recent", Status: "active", AccessMode: "node_auth", CreatedAt: now, LastSeenAt: &recent},
		{ID: "offline-recent", DeviceID: "device-offline-recent", Name: "Offline recent", Status: "active", AccessMode: "node_auth", CreatedAt: now, LastSeenAt: &recent},
		{ID: "online-a", DeviceID: "device-online-a", Name: "Online A", Status: "active", AccessMode: "node_auth", CreatedAt: now.Add(2 * time.Second), LastSeenAt: &now},
	} {
		node.OwnerUserID = ownerUserID
		codeHash := []byte{byte(index + 10)}
		if _, _, err := application.store.ObserveChallenge(context.Background(), codeHash, node.DeviceID, now, now.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		token := store.DeviceTokenRecord{ID: "token-" + node.ID, NodeID: node.ID, TokenHash: []byte("hash-" + node.ID), Status: "active", CreatedAt: now}
		if _, _, err := application.store.ConfirmChallenge(context.Background(), codeHash, node, token, now); err != nil {
			t.Fatal(err)
		}
		if node.LastSeenAt != nil {
			if _, err := application.store.AuthenticateDeviceToken(context.Background(), token.TokenHash, *node.LastSeenAt); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, nodeID := range []string{"online-a", "online-b", "online-recent"} {
		application.registry.Register(nodeID, "connection-"+nodeID, idleRelaySession{})
	}

	request := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	request.AddCookie(&http.Cookie{Name: userSessionCookie, Value: sessionToken})
	recorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("nodes response = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{"online-recent", "online-a", "online-b", "offline-recent", "offline-never"}
	if len(payload) != len(wantIDs) {
		t.Fatalf("payload = %#v", payload)
	}
	for index, wantID := range wantIDs {
		if payload[index]["id"] != wantID {
			t.Fatalf("payload order = %#v want %v", payload, wantIDs)
		}
	}
	if payload[0]["status"] != "online" || payload[3]["status"] != "offline" {
		t.Fatalf("payload statuses = %#v", payload)
	}
	if payload[0]["base_url"] != "https://relay.example.com/n/online-recent/" {
		t.Fatalf("base_url = %#v", payload[0]["base_url"])
	}
	for _, node := range payload {
		for _, forbidden := range []string{"device_id", "device_token", "token_hash", "connection_id", "created_at"} {
			if _, ok := node[forbidden]; ok {
				t.Fatalf("payload exposes %s: %#v", forbidden, node)
			}
		}
	}
}

func TestRelayNodeRenameRequiresSameOriginAndPersists(t *testing.T) {
	application := newBrowserTestApp(t)
	now := time.Date(2026, 8, 4, 11, 0, 0, 0, time.UTC)
	sessionToken := browserUserSession(t, application)
	ownerUserID := browserUserID(t, application, sessionToken)
	createRelayTestNode(t, application, store.Node{ID: "node-rename", DeviceID: "device-rename", OwnerUserID: ownerUserID, Name: "Before", Status: "active", AccessMode: "node_auth", CreatedAt: now}, []byte("rename-code"))
	unauthorized := httptest.NewRecorder()
	unauthorizedRequest := httptest.NewRequest(http.MethodPatch, "/api/nodes/node-rename", strings.NewReader(`{"name":"After"}`))
	unauthorizedRequest.Header.Set("Origin", "https://relay.example.com")
	application.Handler().ServeHTTP(unauthorized, unauthorizedRequest)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized rename status = %d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	request := httptest.NewRequest(http.MethodPatch, "/api/nodes/node-rename", strings.NewReader(`{"name":"After"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: userSessionCookie, Value: sessionToken})
	forbidden := httptest.NewRecorder()
	application.Handler().ServeHTTP(forbidden, request)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d body=%s", forbidden.Code, forbidden.Body.String())
	}

	request = httptest.NewRequest(http.MethodPatch, "/api/nodes/node-rename", strings.NewReader(`{"name":"After"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://relay.example.com")
	request.AddCookie(&http.Cookie{Name: userSessionCookie, Value: sessionToken})
	recorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"name":"After"`) {
		t.Fatalf("rename response = %d body=%s", recorder.Code, recorder.Body.String())
	}
	stored, err := application.store.GetNode(context.Background(), "node-rename")
	if err != nil || stored.Name != "After" {
		t.Fatalf("stored node = %#v err=%v", stored, err)
	}

	empty := httptest.NewRequest(http.MethodPatch, "/api/nodes/node-rename", strings.NewReader(`{"name":" "}`))
	empty.Header.Set("Origin", "https://relay.example.com")
	empty.AddCookie(&http.Cookie{Name: userSessionCookie, Value: sessionToken})
	emptyRecorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(emptyRecorder, empty)
	if emptyRecorder.Code != http.StatusBadRequest {
		t.Fatalf("empty rename status = %d body=%s", emptyRecorder.Code, emptyRecorder.Body.String())
	}

	missing := httptest.NewRequest(http.MethodPatch, "/api/nodes/missing", strings.NewReader(`{"name":"Name"}`))
	missing.Header.Set("Origin", "https://relay.example.com")
	missing.AddCookie(&http.Cookie{Name: userSessionCookie, Value: sessionToken})
	missingRecorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(missingRecorder, missing)
	if missingRecorder.Code != http.StatusNotFound || !strings.Contains(missingRecorder.Body.String(), `"error":"node_not_found"`) {
		t.Fatalf("missing rename response = %d body=%s", missingRecorder.Code, missingRecorder.Body.String())
	}
}

func TestRelayNodeDeleteRevokesTokenAndDisconnects(t *testing.T) {
	application := newBrowserTestApp(t)
	now := time.Date(2026, 8, 4, 11, 0, 0, 0, time.UTC)
	sessionToken := browserUserSession(t, application)
	ownerUserID := browserUserID(t, application, sessionToken)
	node := store.Node{ID: "node-delete", DeviceID: "device-delete", OwnerUserID: ownerUserID, Name: "Delete", Status: "active", AccessMode: "node_auth", CreatedAt: now}
	otherNode := store.Node{ID: "node-keep", DeviceID: "device-keep", OwnerUserID: ownerUserID, Name: "Keep", Status: "active", AccessMode: "node_auth", CreatedAt: now.Add(time.Second)}
	createRelayTestNode(t, application, node, []byte("delete-code"))
	createRelayTestNode(t, application, otherNode, []byte("keep-code"))
	session := &trackingRelaySession{}
	application.registry.Register(node.ID, "connection-delete", session)
	forbiddenRequest := httptest.NewRequest(http.MethodDelete, "/api/nodes/"+node.ID, nil)
	forbiddenRequest.AddCookie(&http.Cookie{Name: userSessionCookie, Value: sessionToken})
	forbidden := httptest.NewRecorder()
	application.Handler().ServeHTTP(forbidden, forbiddenRequest)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("missing origin delete status = %d body=%s", forbidden.Code, forbidden.Body.String())
	}

	request := httptest.NewRequest(http.MethodDelete, "/api/nodes/"+node.ID, nil)
	request.Header.Set("Origin", "https://relay.example.com")
	request.AddCookie(&http.Cookie{Name: userSessionCookie, Value: sessionToken})
	recorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"success":true`) {
		t.Fatalf("delete response = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !session.closed || application.registry.Status(node.ID).Online {
		t.Fatalf("closed=%v presence=%#v", session.closed, application.registry.Status(node.ID))
	}
	if _, err := application.store.GetNode(context.Background(), node.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetNode() error = %v", err)
	}
	if _, err := application.store.AuthenticateDeviceToken(context.Background(), []byte("hash-"+node.ID), now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("AuthenticateDeviceToken() error = %v", err)
	}
	if stored, err := application.store.GetNode(context.Background(), otherNode.ID); err != nil || stored.ID != otherNode.ID {
		t.Fatalf("other node = %#v err=%v", stored, err)
	}
	if authenticated, err := application.store.AuthenticateDeviceToken(context.Background(), []byte("hash-"+otherNode.ID), now); err != nil || authenticated.ID != otherNode.ID {
		t.Fatalf("other token authentication = %#v err=%v", authenticated, err)
	}

	missing := httptest.NewRequest(http.MethodDelete, "/api/nodes/"+node.ID, nil)
	missing.Header.Set("Origin", "https://relay.example.com")
	missing.AddCookie(&http.Cookie{Name: userSessionCookie, Value: sessionToken})
	missingRecorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(missingRecorder, missing)
	if missingRecorder.Code != http.StatusNotFound {
		t.Fatalf("second delete response = %d body=%s", missingRecorder.Code, missingRecorder.Body.String())
	}
}

func TestRelayStoreFailureIsNotReportedAsUnauthorized(t *testing.T) {
	application := newBrowserTestApp(t)
	sessionToken := browserUserSession(t, application)
	if err := application.store.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	request.AddCookie(&http.Cookie{Name: userSessionCookie, Value: sessionToken})
	recorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), `"error":"request_failed"`) {
		t.Fatalf("store failure response = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestRelayNodeStoreFailuresReturnInternalError(t *testing.T) {
	application := newBrowserTestApp(t)
	sessionToken := browserUserSession(t, application)
	workingStore := application.store
	failure := errors.New("relay store failed")

	application.store = relayFailureStore{Store: workingStore, listErr: failure}
	listRequest := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	listRequest.AddCookie(&http.Cookie{Name: userSessionCookie, Value: sessionToken})
	listRecorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(listRecorder, listRequest)
	assertRelayRequestFailed(t, listRecorder)

	application.store = relayFailureStore{Store: workingStore, renameErr: failure}
	renameRequest := httptest.NewRequest(http.MethodPatch, "/api/nodes/node", strings.NewReader(`{"name":"Name"}`))
	renameRequest.Header.Set("Origin", "https://relay.example.com")
	renameRequest.AddCookie(&http.Cookie{Name: userSessionCookie, Value: sessionToken})
	renameRecorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(renameRecorder, renameRequest)
	assertRelayRequestFailed(t, renameRecorder)

	application.store = relayFailureStore{Store: workingStore, deleteErr: failure}
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/nodes/node", nil)
	deleteRequest.Header.Set("Origin", "https://relay.example.com")
	deleteRequest.AddCookie(&http.Cookie{Name: userSessionCookie, Value: sessionToken})
	deleteRecorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(deleteRecorder, deleteRequest)
	assertRelayRequestFailed(t, deleteRecorder)
}

func assertRelayRequestFailed(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), `"error":"request_failed"`) {
		t.Fatalf("store failure response = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func createRelayTestNode(t *testing.T, application *App, node store.Node, codeHash []byte) {
	t.Helper()
	now := node.CreatedAt
	if _, _, err := application.store.ObserveChallenge(context.Background(), codeHash, node.DeviceID, now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	token := store.DeviceTokenRecord{ID: "token-" + node.ID, NodeID: node.ID, TokenHash: []byte("hash-" + node.ID), Status: "active", CreatedAt: now}
	if _, _, err := application.store.ConfirmChallenge(context.Background(), codeHash, node, token, now); err != nil {
		t.Fatal(err)
	}
}

type idleRelaySession struct{}

func (idleRelaySession) Open() (net.Conn, error) { return nil, nil }
func (idleRelaySession) Close() error            { return nil }

type trackingRelaySession struct {
	closed bool
}

func (*trackingRelaySession) Open() (net.Conn, error) { return nil, nil }
func (s *trackingRelaySession) Close() error {
	s.closed = true
	return nil
}

type relayFailureStore struct {
	store.Store
	listErr   error
	renameErr error
	deleteErr error
}

func (s relayFailureStore) ListNodesByOwner(context.Context, string) ([]store.Node, error) {
	return nil, s.listErr
}

func (s relayFailureStore) RenameNodeByOwner(context.Context, string, string, string) (store.Node, error) {
	return store.Node{}, s.renameErr
}

func (s relayFailureStore) DeleteNodeByOwner(context.Context, string, string) error {
	return s.deleteErr
}

func browserUserSession(t *testing.T, application *App) string {
	t.Helper()
	return registerTestSession(t, application, "user@qq.com", "relay-password")
}

func browserUserID(t *testing.T, application *App, token string) string {
	t.Helper()
	user, _, err := application.identity.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	return user.ID
}

func newBrowserTestApp(t *testing.T) *App {
	t.Helper()
	publicURL, err := url.Parse("https://relay.example.com")
	if err != nil {
		t.Fatal(err)
	}
	var tokenKey [32]byte
	copy(tokenKey[:], []byte("01234567890123456789012345678901"))
	application, err := newTestApp(t, testConfig(t, publicURL, tokenKey))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	return application
}
