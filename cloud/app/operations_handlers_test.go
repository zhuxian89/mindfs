package app

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReadyAndAssetHandlers(t *testing.T) {
	publicURL, _ := url.Parse("http://relay.example.com")
	var tokenKey [32]byte
	cfg := testConfig(t, publicURL, tokenKey)
	assetPath := filepath.Join(cfg.AssetsDir, "assets", "index-test.js")
	if err := os.WriteFile(assetPath, []byte("asset-ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	application, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()

	ready := httptest.NewRecorder()
	application.Handler().ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusOK || !strings.Contains(ready.Body.String(), "ready") {
		t.Fatalf("ready response = %d %s", ready.Code, ready.Body.String())
	}
	asset := httptest.NewRecorder()
	application.Handler().ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/mindfs-assets/index-test.js", nil))
	if asset.Code != http.StatusOK || asset.Body.String() != "asset-ok" {
		t.Fatalf("asset response = %d %q", asset.Code, asset.Body.String())
	}
	if !strings.Contains(asset.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("Cache-Control = %q", asset.Header().Get("Cache-Control"))
	}
	metrics := httptest.NewRecorder()
	application.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics?secret=hidden", nil))
	if metrics.Code != http.StatusOK || !strings.Contains(metrics.Body.String(), "mindfs_cloud_http_requests_total") {
		t.Fatalf("metrics response = %d %s", metrics.Code, metrics.Body.String())
	}
	for _, forbidden := range []string{"/mindfs-assets/", "secret=hidden", "path=", "query="} {
		if strings.Contains(metrics.Body.String(), forbidden) {
			t.Fatalf("metrics exposed %q: %s", forbidden, metrics.Body.String())
		}
	}

	if err := os.Remove(filepath.Join(cfg.AssetsDir, "index.html")); err != nil {
		t.Fatal(err)
	}
	unready := httptest.NewRecorder()
	application.Handler().ServeHTTP(unready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if unready.Code != http.StatusServiceUnavailable {
		t.Fatalf("unready response = %d %s", unready.Code, unready.Body.String())
	}
	health := httptest.NewRecorder()
	application.Handler().ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health response = %d", health.Code)
	}
}

func TestAssetHandlerRejectsUnsafePaths(t *testing.T) {
	publicURL, _ := url.Parse("http://relay.example.com")
	var tokenKey [32]byte
	cfg := testConfig(t, publicURL, tokenKey)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(cfg.AssetsDir, "assets", "outside.js")); err != nil {
			t.Fatal(err)
		}
	}
	application, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()

	paths := []string{"/mindfs-assets/", "/mindfs-assets/missing.js", "/mindfs-assets/%2e%2e/index.html"}
	if runtime.GOOS != "windows" {
		paths = append(paths, "/mindfs-assets/outside.js")
	}
	for _, target := range paths {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		application.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s returned %d body=%q", target, recorder.Code, recorder.Body.String())
		}
		if recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s Cache-Control = %q", target, recorder.Header().Get("Cache-Control"))
		}
		if strings.Contains(recorder.Body.String(), outside) {
			t.Fatal("asset error leaked absolute path")
		}
	}
}

func TestAssetHandlerServesMultipleReleaseHashes(t *testing.T) {
	publicURL, _ := url.Parse("http://relay.example.com")
	var tokenKey [32]byte
	cfg := testConfig(t, publicURL, tokenKey)
	assets := map[string]string{
		"index-C0gNCfj8.js": "v0.4.4",
		"index-B4USfphH.js": "v0.4.5",
		"index-DeNebQ9q.js": "v0.4.6",
	}
	for name, content := range assets {
		if err := os.WriteFile(filepath.Join(cfg.AssetsDir, "assets", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	application, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	for name, want := range assets {
		recorder := httptest.NewRecorder()
		application.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/mindfs-assets/"+name, nil))
		if recorder.Code != http.StatusOK || recorder.Body.String() != want {
			t.Fatalf("%s response = %d %q", name, recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Header().Get("Cache-Control"), "immutable") {
			t.Fatalf("%s Cache-Control = %q", name, recorder.Header().Get("Cache-Control"))
		}
	}
}
