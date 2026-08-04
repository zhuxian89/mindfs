package assetsync

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSyncMergesCurrentAndHistoricalReleaseAssets(t *testing.T) {
	source := writeSourceBundle(t, map[string]string{
		"index-ZyXwVu98.js":  "current",
		"agents/augment.svg": "current-stable",
	})
	target := t.TempDir()
	archive := releaseArchive(t, map[string]string{
		"mindfs_v0.1.8_linux_amd64/web/assets/index-AbCdEf12.js":  "old",
		"mindfs_v0.1.8_linux_amd64/web/assets/fonts/font.woff2":   "font",
		"mindfs_v0.1.8_linux_amd64/web/assets/agents/augment.svg": "release-stable",
	})
	digest := sha256.Sum256(archive)
	var archiveRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			_ = json.NewEncoder(w).Encode([]release{
				{TagName: "v0.1.6", Assets: []releaseAsset{{Name: "mindfs_v0.1.6_linux_amd64.tar.gz", BrowserDownloadURL: serverURL(r, "/old")}}},
				{TagName: "v0.1.8", Assets: []releaseAsset{{
					Name:               "mindfs_v0.1.8_linux_amd64.tar.gz",
					BrowserDownloadURL: serverURL(r, "/archive"),
					Digest:             "sha256:" + hex.EncodeToString(digest[:]),
					Size:               int64(len(archive)),
				}}},
			})
		case "/archive":
			archiveRequests.Add(1)
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	options := Options{SourceDir: source, TargetDir: target, ReleasesURL: server.URL + "/releases", HTTPClient: server.Client()}
	result, err := Sync(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReleasesImported != 1 || archiveRequests.Load() != 1 {
		t.Fatalf("first sync result=%#v requests=%d", result, archiveRequests.Load())
	}
	for name, want := range map[string]string{
		"index-ZyXwVu98.js":  "current",
		"index-AbCdEf12.js":  "old",
		"fonts/font.woff2":   "font",
		"agents/augment.svg": "release-stable",
	} {
		payload, err := os.ReadFile(filepath.Join(target, "assets", filepath.FromSlash(name)))
		if err != nil || string(payload) != want {
			t.Fatalf("asset %s = %q, %v", name, payload, err)
		}
	}
	result, err = Sync(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReleasesPresent != 1 || archiveRequests.Load() != 1 {
		t.Fatalf("second sync result=%#v requests=%d", result, archiveRequests.Load())
	}
	stable, err := os.ReadFile(filepath.Join(target, "assets", "agents", "augment.svg"))
	if err != nil || string(stable) != "release-stable" {
		t.Fatalf("stable asset after second sync = %q, %v", stable, err)
	}
	if err := os.Remove(filepath.Join(target, "assets", "index-AbCdEf12.js")); err != nil {
		t.Fatal(err)
	}
	result, err = Sync(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReleasesImported != 1 || archiveRequests.Load() != 2 {
		t.Fatalf("repair sync result=%#v requests=%d", result, archiveRequests.Load())
	}
}

func TestSyncRejectsImmutableAssetCollision(t *testing.T) {
	source := writeSourceBundle(t, map[string]string{"index-AbCdEf12.js": "new"})
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "assets", "index-AbCdEf12.js"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Sync(context.Background(), Options{SourceDir: source, TargetDir: target, CurrentOnly: true})
	if err == nil || !strings.Contains(err.Error(), "different content") {
		t.Fatalf("Sync() error = %v", err)
	}
}

func TestSyncAllowsStableAssetReplacement(t *testing.T) {
	source := writeSourceBundle(t, map[string]string{"agents/augment.svg": "current"})
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "assets", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	stablePath := filepath.Join(target, "assets", "agents", "augment.svg")
	if err := os.WriteFile(stablePath, []byte("historical"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), Options{SourceDir: source, TargetDir: target, CurrentOnly: true}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(stablePath)
	if err != nil || string(payload) != "current" {
		t.Fatalf("stable asset = %q, %v", payload, err)
	}
}

func TestImmutableAssetClassification(t *testing.T) {
	for name, want := range map[string]bool{
		"index-DeNebQ9q.js":                       true,
		"fonts/KaTeX_Main-Regular-B22Nviop.woff2": true,
		"agents/augment.svg":                      false,
		"agents/claude-code.svg":                  false,
	} {
		if got := isImmutableAsset(name); got != want {
			t.Fatalf("isImmutableAsset(%q) = %v want %v", name, got, want)
		}
	}
}

func TestNextReleasePage(t *testing.T) {
	header := `<https://api.github.com/releases?page=1>; rel="prev", <https://api.github.com/releases?page=3>; rel="next"`
	if got := nextReleasePage(header); got != "https://api.github.com/releases?page=3" {
		t.Fatalf("nextReleasePage() = %q", got)
	}
}

func TestExtractReleaseAssetsRejectsTraversalAndLinks(t *testing.T) {
	for _, header := range []tar.Header{
		{Name: "root/web/assets/../secret", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg},
		{Name: "root/web/assets/link.js", Linkname: "/etc/passwd", Typeflag: tar.TypeSymlink},
	} {
		t.Run(header.Name, func(t *testing.T) {
			archive := releaseArchiveHeaders(t, []tar.Header{header}, [][]byte{[]byte("x")})
			path := filepath.Join(t.TempDir(), "release.tar.gz")
			if err := os.WriteFile(path, archive, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := extractReleaseAssets(path, t.TempDir()); err == nil {
				t.Fatal("unsafe archive was accepted")
			}
		})
	}
}

func writeSourceBundle(t *testing.T, assets map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<script src="./assets/index-current.js"></script>`), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range assets {
		path := filepath.Join(dir, "assets", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func releaseArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	headers := make([]tar.Header, 0, len(files))
	payloads := make([][]byte, 0, len(files))
	for name, content := range files {
		payload := []byte(content)
		headers = append(headers, tar.Header{Name: name, Mode: 0o644, Size: int64(len(payload)), Typeflag: tar.TypeReg})
		payloads = append(payloads, payload)
	}
	return releaseArchiveHeaders(t, headers, payloads)
}

func releaseArchiveHeaders(t *testing.T, headers []tar.Header, payloads [][]byte) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	for index := range headers {
		if err := tarWriter.WriteHeader(&headers[index]); err != nil {
			t.Fatal(err)
		}
		if headers[index].Typeflag == tar.TypeReg && headers[index].Size > 0 {
			if _, err := tarWriter.Write(payloads[index]); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func serverURL(r *http.Request, path string) string {
	return "http://" + r.Host + path
}
