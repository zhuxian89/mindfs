package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"mindfs-cloud/internal/identity"
)

func TestHTTPRegistrationPasswordLoginAndReset(t *testing.T) {
	publicURL, _ := url.Parse("https://relay.example.com")
	application, err := newTestApp(t, testConfig(t, publicURL, [32]byte{}))
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	server := httptest.NewServer(application.Handler())
	defer server.Close()
	headers := map[string]string{"Origin": publicURL.String()}
	email := "user@qq.com"
	password := "relay-password"

	requestJSON(t, server.URL+"/api/auth/register/request-code", http.MethodPost, map[string]string{"email": email}, headers, http.StatusOK)
	code := application.mail.(*testMailSender).code(t, email, "register")
	registered := requestJSON(t, server.URL+"/api/auth/register", http.MethodPost, map[string]string{
		"email": email, "password": password, "code": code,
	}, headers, http.StatusOK)
	if registered["user"].(map[string]any)["email"] != email {
		t.Fatalf("register response = %#v", registered)
	}

	loginRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/login", strings.NewReader(`{"email":"user@qq.com","password":"relay-password"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRequest.Header.Set("Origin", publicURL.String())
	loginResponse, err := http.DefaultClient.Do(loginRequest)
	if err != nil {
		t.Fatal(err)
	}
	if loginResponse.StatusCode != http.StatusOK || len(loginResponse.Cookies()) != 1 {
		_ = loginResponse.Body.Close()
		t.Fatalf("login response = %d cookies=%#v", loginResponse.StatusCode, loginResponse.Cookies())
	}
	loginCookie := loginResponse.Cookies()[0]
	_ = loginResponse.Body.Close()

	meRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/auth/me", nil)
	meRequest.AddCookie(loginCookie)
	meResponse, err := http.DefaultClient.Do(meRequest)
	if err != nil {
		t.Fatal(err)
	}
	if meResponse.StatusCode != http.StatusOK {
		_ = meResponse.Body.Close()
		t.Fatalf("me status = %d", meResponse.StatusCode)
	}
	_ = meResponse.Body.Close()

	requestJSON(t, server.URL+"/api/auth/password/request-code", http.MethodPost, map[string]string{"email": email}, headers, http.StatusOK)
	resetCode := application.mail.(*testMailSender).code(t, email, "password_reset")
	requestJSON(t, server.URL+"/api/auth/password/reset", http.MethodPost, map[string]string{
		"email": email, "code": resetCode, "new_password": "new-relay-password",
	}, headers, http.StatusOK)
	requestJSON(t, server.URL+"/api/auth/login", http.MethodPost, map[string]string{
		"email": email, "password": password,
	}, headers, http.StatusUnauthorized)
	requestJSON(t, server.URL+"/api/auth/login", http.MethodPost, map[string]string{
		"email": email, "password": "new-relay-password",
	}, headers, http.StatusOK)
}

func TestHTTPAuthRejectsNonQQAndLegacyLogin(t *testing.T) {
	publicURL, _ := url.Parse("https://relay.example.com")
	application, err := newTestApp(t, testConfig(t, publicURL, [32]byte{}))
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	server := httptest.NewServer(application.Handler())
	defer server.Close()
	headers := map[string]string{"Origin": publicURL.String()}
	response := requestJSON(t, server.URL+"/api/auth/register/request-code", http.MethodPost, map[string]string{
		"email": "user@gmail.com",
	}, headers, http.StatusForbidden)
	if response["error"] != "email_not_allowed" {
		t.Fatalf("response = %#v", response)
	}
	if len(application.mail.(*testMailSender).codes) != 0 {
		t.Fatal("non-QQ registration sent mail")
	}

	legacy, _ := http.NewRequest(http.MethodPost, server.URL+"/api/cloud/v1/auth/login", strings.NewReader(`{"username":"admin","password":"secret"}`))
	legacyResponse, err := http.DefaultClient.Do(legacy)
	if err != nil {
		t.Fatal(err)
	}
	defer legacyResponse.Body.Close()
	if legacyResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("legacy login status = %d", legacyResponse.StatusCode)
	}
}

func TestLoginPageContainsPasswordAccountFlowsOnly(t *testing.T) {
	application := newBrowserTestApp(t)
	recorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/login", nil))
	body := recorder.Body.String()
	for _, required := range []string{"login-form", "register-form", "reset-form", "Relay 密码"} {
		if !strings.Contains(body, required) {
			t.Fatalf("login page missing %q", required)
		}
	}
	for _, forbidden := range []string{"api/auth/google", "api/auth/github", "api/auth/linuxdo", "QQ 邮箱密码", "username"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("login page contains %q", forbidden)
		}
	}
}

func TestPasswordChangeRequiresSameOriginAndRotatesSession(t *testing.T) {
	application := newBrowserTestApp(t)
	oldToken := registerTestSession(t, application, "change@qq.com", "relay-password")
	body := `{"current_password":"relay-password","new_password":"changed-password"}`

	forbiddenRequest := httptest.NewRequest(http.MethodPost, "/api/auth/password/change", strings.NewReader(body))
	forbiddenRequest.AddCookie(&http.Cookie{Name: userSessionCookie, Value: oldToken})
	forbidden := httptest.NewRecorder()
	application.Handler().ServeHTTP(forbidden, forbiddenRequest)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d body=%s", forbidden.Code, forbidden.Body.String())
	}
	if _, _, err := application.identity.Authenticate(forbiddenRequest.Context(), oldToken); err != nil {
		t.Fatalf("session changed after forbidden request: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/auth/password/change", strings.NewReader(body))
	request.Header.Set("Origin", application.config.PublicURL.String())
	request.AddCookie(&http.Cookie{Name: userSessionCookie, Value: oldToken})
	recorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("change password status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != userSessionCookie || cookies[0].Value == oldToken {
		t.Fatalf("rotated cookies = %#v", cookies)
	}
	if _, _, err := application.identity.Authenticate(request.Context(), oldToken); !errors.Is(err, identity.ErrAuthRequired) {
		t.Fatalf("old session Authenticate() error = %v", err)
	}
	if _, _, err := application.identity.Authenticate(request.Context(), cookies[0].Value); err != nil {
		t.Fatalf("new session Authenticate() error = %v", err)
	}
}

func TestLogoutRequiresSameOriginAndRevokesSession(t *testing.T) {
	application := newBrowserTestApp(t)
	token := registerTestSession(t, application, "logout@qq.com", "relay-password")

	forbiddenRequest := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	forbiddenRequest.AddCookie(&http.Cookie{Name: userSessionCookie, Value: token})
	forbidden := httptest.NewRecorder()
	application.Handler().ServeHTTP(forbidden, forbiddenRequest)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d body=%s", forbidden.Code, forbidden.Body.String())
	}
	if _, _, err := application.identity.Authenticate(forbiddenRequest.Context(), token); err != nil {
		t.Fatalf("session revoked after forbidden logout: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	request.Header.Set("Origin", application.config.PublicURL.String())
	request.AddCookie(&http.Cookie{Name: userSessionCookie, Value: token})
	recorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("logout status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, _, err := application.identity.Authenticate(request.Context(), token); !errors.Is(err, identity.ErrAuthRequired) {
		t.Fatalf("logged-out session Authenticate() error = %v", err)
	}
}

func TestLogoutDoesNotClearCookieWhenSessionRevocationFails(t *testing.T) {
	application := newBrowserTestApp(t)
	token := registerTestSession(t, application, "logout-failure@qq.com", "relay-password")
	if err := application.store.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	request.Header.Set("Origin", application.config.PublicURL.String())
	request.AddCookie(&http.Cookie{Name: userSessionCookie, Value: token})
	recorder := httptest.NewRecorder()
	application.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("logout failure status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if cookies := recorder.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("logout failure cleared cookies = %#v", cookies)
	}
}

func TestAuthenticatedWritesReportSessionStoreFailures(t *testing.T) {
	application := newBrowserTestApp(t)
	token := registerTestSession(t, application, "store-failure@qq.com", "relay-password")
	if err := application.store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path string
		body string
	}{
		{path: "/api/auth/password/change", body: `{"current_password":"relay-password","new_password":"changed-password"}`},
		{path: "/api/bind/confirm", body: `{"code":"pc_dGVzdC1jb2Rl","name":"Node"}`},
	} {
		request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
		request.Header.Set("Origin", application.config.PublicURL.String())
		request.AddCookie(&http.Cookie{Name: userSessionCookie, Value: token})
		recorder := httptest.NewRecorder()
		application.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("%s status = %d body=%s", test.path, recorder.Code, recorder.Body.String())
		}
	}
}
