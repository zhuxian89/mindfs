package ops

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestHealthcheckUsesReadyEndpointAndStatusOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("sensitive body ignored"))
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	if err := Healthcheck(context.Background(), parsed.Host, server.Client()); err != nil {
		t.Fatal(err)
	}
}

func TestHealthcheckRejectsUnready(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "database path must not leak", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	if err := Healthcheck(context.Background(), parsed.Host, server.Client()); err == nil {
		t.Fatal("Healthcheck accepted unready response")
	}
}
