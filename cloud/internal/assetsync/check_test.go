package assetsync

import (
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

func TestCoverageDetectsNewReleaseAndMissingOrChangedFiles(t *testing.T) {
	source := writeSourceBundle(t, map[string]string{"index-current.js": "current"})
	archives := map[string][]byte{}
	var catalog atomic.Value
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases" {
			_ = json.NewEncoder(w).Encode(catalog.Load())
			return
		}
		downloads.Add(1)
		_, _ = w.Write(archives[r.URL.Path])
	}))
	defer server.Close()
	var releases []release
	for _, tag := range []string{"v0.4.9", "v0.5.0"} {
		archive := releaseArchive(t, map[string]string{
			"mindfs_" + tag + "/web/assets/" + tag + "-AbCdEf12.js": "bundle " + tag,
		})
		archives["/"+tag] = archive
		digest := sha256.Sum256(archive)
		releases = append(releases, release{TagName: tag, Assets: []releaseAsset{{
			Name: "mindfs_" + tag + "_linux_amd64.tar.gz", BrowserDownloadURL: server.URL + "/" + tag,
			Size: int64(len(archive)), Digest: "sha256:" + hex.EncodeToString(digest[:]),
		}}})
	}
	// These entries must never be required by either sync or coverage checks.
	releases = append(releases, release{TagName: "v0.1.7"}, release{TagName: "v0.6.0", Prerelease: true}, release{TagName: "v0.7.0", Draft: true})
	catalog.Store(releases[:1])
	options := Options{SourceDir: source, TargetDir: t.TempDir(), ReleasesURL: server.URL + "/releases", HTTPClient: server.Client()}
	if _, err := Sync(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	if coverage, err := Check(context.Background(), options); err != nil || coverage.Releases != 1 || coverage.LatestTag != "v0.4.9" {
		t.Fatalf("initial coverage = %+v, %v", coverage, err)
	}
	catalog.Store(releases)
	expectIncomplete := func(tag string) {
		t.Helper()
		before := downloads.Load()
		if _, err := Check(context.Background(), options); err == nil || !strings.Contains(err.Error(), tag) {
			t.Fatalf("expected incomplete %s, got %v", tag, err)
		}
		if downloads.Load() != before {
			t.Fatal("read-only check downloaded an archive")
		}
	}
	expectIncomplete("v0.5.0")
	if _, err := Sync(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	if coverage, err := Check(context.Background(), options); err != nil || coverage.Releases != 2 || coverage.LatestTag != "v0.5.0" {
		t.Fatalf("updated coverage = %+v, %v", coverage, err)
	}
	if downloads.Load() != 2 {
		t.Fatalf("incremental import downloaded %d archives", downloads.Load())
	}
	oldFile := filepath.Join(options.TargetDir, "assets", "v0.4.9-AbCdEf12.js")
	if err := os.Remove(oldFile); err != nil {
		t.Fatal(err)
	}
	expectIncomplete("v0.4.9")
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatal("read-only check repaired a file")
	}
	if _, err := Sync(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldFile, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	expectIncomplete("v0.4.9")
	if payload, err := os.ReadFile(oldFile); err != nil || string(payload) != "corrupt" {
		t.Fatal("read-only check overwrote a file")
	}
	if _, err := Sync(context.Background(), options); err == nil || !strings.Contains(err.Error(), "different content") {
		t.Fatalf("immutable content collision must remain a failure, got %v", err)
	}
}

func TestCoverageFailsWhenCatalogOrCurrentEntryCannotBeVerified(t *testing.T) {
	target := writeSourceBundle(t, map[string]string{"index-current.js": "current"})
	for _, test := range []struct {
		name, payload string
		status        int
	}{
		{"empty", "[]", 200},
		{"invalid JSON", "invalid", 200},
		{"unavailable", "unavailable", 503},
		{"missing archive", `[{"tag_name":"v0.5.0"}]`, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.payload))
			}))
			defer server.Close()
			if _, err := Check(context.Background(), Options{TargetDir: target, ReleasesURL: server.URL, HTTPClient: server.Client()}); err == nil {
				t.Fatal("unverifiable coverage was accepted")
			}
		})
	}
	if err := os.Remove(filepath.Join(target, "assets", "index-current.js")); err != nil {
		t.Fatal(err)
	}
	// An invalid URL ensures current-entry validation fails before any network call.
	if _, err := Check(context.Background(), Options{TargetDir: target, ReleasesURL: ":invalid"}); err == nil || !strings.Contains(err.Error(), "index-current.js") {
		t.Fatalf("missing current entry = %v", err)
	}
}
