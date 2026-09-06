package assetsync

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The metadata is a one-release JSON array from the public GitHub Releases API.
// Keep network downloads explicit; normal tests use small synthetic archives.
func TestOfficialReleaseAssetCompatibility(t *testing.T) {
	archivePath := os.Getenv("MINDFS_COMPAT_RELEASE_ARCHIVE")
	metadataPath := os.Getenv("MINDFS_COMPAT_RELEASE_METADATA")
	if os.Getenv("MINDFS_RUN_COMPAT") != "1" || archivePath == "" || metadataPath == "" {
		t.Skip("requires MINDFS_RUN_COMPAT=1 and explicit release archive/metadata paths")
	}
	metadata, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	var releases []release
	if err := json.Unmarshal(metadata, &releases); err != nil {
		t.Fatal(err)
	}
	if len(releases) != 1 {
		t.Fatal("metadata must describe exactly one official release")
	}
	item := releases[0]
	asset, ok := linuxReleaseAsset(item)
	if !ok || filepath.Base(archivePath) != asset.Name {
		t.Fatal("archive does not match the release's Linux AMD64 asset")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			localAsset := asset
			localAsset.BrowserDownloadURL = serverURL(r, "/archive")
			localRelease := item
			localRelease.Assets = []releaseAsset{localAsset}
			_ = json.NewEncoder(w).Encode([]release{localRelease})
		case "/archive":
			http.ServeFile(w, r, archivePath)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	options := Options{
		SourceDir: writeSourceBundle(t, map[string]string{"index-current.js": "current fixture"}),
		TargetDir: t.TempDir(), ReleasesURL: server.URL + "/releases", HTTPClient: server.Client(),
	}
	result, err := Sync(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReleasesImported != 1 {
		t.Fatalf("import result = %+v", result)
	}
	coverage, err := Check(context.Background(), options)
	if err != nil || coverage.Releases != 1 || coverage.LatestTag != item.TagName {
		t.Fatalf("coverage = %+v, %v", coverage, err)
	}

	// Compare every published asset, including stable filenames, with the
	// imported file. Sync itself verifies the original archive size and SHA-256.
	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	count := 0
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		_, name, found := strings.Cut(header.Name, "/web/assets/")
		if !found || header.Typeflag == tar.TypeDir {
			continue
		}
		if !filepath.IsLocal(name) {
			t.Fatal("unsafe archive asset path")
		}
		want := sha256.New()
		if _, err := io.Copy(want, reader); err != nil {
			t.Fatal(err)
		}
		got, err := fileDigest(filepath.Join(options.TargetDir, "assets", filepath.FromSlash(name)))
		if err != nil || string(got[:]) != string(want.Sum(nil)) {
			t.Fatalf("asset differs from archive: %s (%v)", name, err)
		}
		count++
	}
	if count == 0 {
		t.Fatal("no published assets checked")
	}
	result, err = Sync(context.Background(), options)
	if err != nil || result.ReleasesImported != 0 || result.ReleasesPresent != 1 {
		t.Fatalf("incremental sync = %+v, %v", result, err)
	}
	t.Logf("official %s: archive digest validated, %d asset files match, coverage check and incremental rerun passed", item.TagName, count)
}
