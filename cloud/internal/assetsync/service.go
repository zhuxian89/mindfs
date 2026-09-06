package assetsync

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultReleasesURL  = "https://api.github.com/repos/a9gent/mindfs/releases?per_page=100"
	DefaultMinimumTag   = "v0.1.8"
	maxReleaseFileBytes = int64(128 << 20)
	maxReleaseTotalSize = int64(2 << 30)
)

type Options struct {
	SourceDir   string
	TargetDir   string
	ReleasesURL string
	MinimumTag  string
	HTTPClient  *http.Client
	CurrentOnly bool
}

type Result struct {
	CurrentFiles     int
	ReleasesImported int
	ReleasesPresent  int
	AssetsAdded      int
	AssetsReused     int
}

type release struct {
	TagName    string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
	Size               int64  `json:"size"`
}

type releaseMarker struct {
	Archive string            `json:"archive"`
	Digest  string            `json:"digest"`
	Assets  map[string]string `json:"assets"`
}

func Sync(ctx context.Context, options Options) (Result, error) {
	var result Result
	sourceDir, targetDir, err := validatePaths(options.SourceDir, options.TargetDir)
	if err != nil {
		return result, err
	}
	if err := os.MkdirAll(filepath.Join(targetDir, "assets"), 0o755); err != nil {
		return result, fmt.Errorf("create asset target: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(targetDir, ".releases"), 0o755); err != nil {
		return result, fmt.Errorf("create release markers: %w", err)
	}
	if options.CurrentOnly {
		if err := copyCurrentBundle(sourceDir, targetDir, &result, false); err != nil {
			return result, err
		}
		return result, nil
	}
	if err := copyCurrentBundle(sourceDir, targetDir, &result, true); err != nil {
		return result, err
	}

	releases, client, err := supportedReleases(ctx, options)
	if err != nil {
		return result, err
	}
	for _, item := range releases {
		asset, ok := linuxReleaseAsset(item)
		if !ok {
			return result, fmt.Errorf("release %s has no linux amd64 archive", item.TagName)
		}
		marker := filepath.Join(targetDir, ".releases", item.TagName+".complete")
		if markerMatches(marker, targetDir, asset) {
			result.ReleasesPresent++
			continue
		}
		added, reused, assetDigests, err := importRelease(ctx, client, targetDir, item.TagName, asset)
		if err != nil {
			return result, err
		}
		markerPayload, err := json.MarshalIndent(releaseMarker{Archive: asset.Name, Digest: asset.Digest, Assets: assetDigests}, "", "  ")
		if err != nil {
			return result, fmt.Errorf("encode release marker %s: %w", item.TagName, err)
		}
		markerPayload = append(markerPayload, '\n')
		if err := writeAtomic(marker, markerPayload, 0o644); err != nil {
			return result, fmt.Errorf("write release marker %s: %w", item.TagName, err)
		}
		result.ReleasesImported++
		result.AssetsAdded += added
		result.AssetsReused += reused
	}
	return result, nil
}

// supportedReleases is shared by import and read-only coverage verification.
func supportedReleases(ctx context.Context, options Options) ([]release, *http.Client, error) {
	releasesURL := strings.TrimSpace(options.ReleasesURL)
	if releasesURL == "" {
		releasesURL = DefaultReleasesURL
	}
	minimumTag := strings.TrimSpace(options.MinimumTag)
	if minimumTag == "" {
		minimumTag = DefaultMinimumTag
	}
	minimum, ok := parseVersion(minimumTag)
	if !ok {
		return nil, nil, fmt.Errorf("invalid minimum release tag %q", minimumTag)
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	releases, err := fetchReleases(ctx, client, releasesURL)
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(releases, func(i, j int) bool {
		left, _ := parseVersion(releases[i].TagName)
		right, _ := parseVersion(releases[j].TagName)
		return compareVersion(left, right) < 0
	})
	filtered := releases[:0]
	for _, item := range releases {
		version, valid := parseVersion(item.TagName)
		if valid && compareVersion(version, minimum) >= 0 && !item.Draft && !item.Prerelease {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == 0 {
		return nil, nil, fmt.Errorf("no supported MindFS releases at or above %s", minimumTag)
	}
	return filtered, client, nil
}

func validatePaths(source, target string) (string, string, error) {
	source = strings.TrimSpace(source)
	target = strings.TrimSpace(target)
	if source == "" {
		return "", "", errors.New("source asset directory is required")
	}
	if target == "" {
		return "", "", errors.New("target asset directory is required")
	}
	var err error
	source, err = filepath.Abs(source)
	if err != nil {
		return "", "", fmt.Errorf("resolve source asset directory: %w", err)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", "", fmt.Errorf("resolve target asset directory: %w", err)
	}
	if source == target {
		return "", "", errors.New("source and target asset directories must differ")
	}
	indexInfo, err := os.Stat(filepath.Join(source, "index.html"))
	if err != nil || !indexInfo.Mode().IsRegular() {
		return "", "", errors.New("source asset directory missing index.html")
	}
	assetsInfo, err := os.Stat(filepath.Join(source, "assets"))
	if err != nil || !assetsInfo.IsDir() {
		return "", "", errors.New("source asset directory missing assets")
	}
	return source, target, nil
}

func copyCurrentBundle(source, target string, result *Result, preserveStable bool) error {
	return filepath.WalkDir(source, func(sourcePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, sourcePath)
		if err != nil || relative == "." {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("source bundle contains symlink %s", relative)
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("source bundle contains non-regular file %s", relative)
		}
		if strings.HasPrefix(relative, "assets"+string(filepath.Separator)) {
			assetName := filepath.ToSlash(strings.TrimPrefix(relative, "assets"+string(filepath.Separator)))
			immutable := isImmutableAsset(assetName)
			if preserveStable && !immutable {
				if info, err := os.Stat(destination); err == nil && info.Mode().IsRegular() {
					result.AssetsReused++
					result.CurrentFiles++
					return nil
				} else if err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
			added, err := installAsset(sourcePath, destination, immutable)
			if err != nil {
				return fmt.Errorf("merge current asset %s: %w", relative, err)
			}
			if added {
				result.AssetsAdded++
			} else {
				result.AssetsReused++
			}
			result.CurrentFiles++
			return nil
		}
		payload, err := os.ReadFile(sourcePath)
		if err != nil {
			return err
		}
		if err := writeAtomic(destination, payload, 0o644); err != nil {
			return err
		}
		result.CurrentFiles++
		return nil
	})
}

func fetchReleases(ctx context.Context, client *http.Client, releasesURL string) ([]release, error) {
	baseURL, err := url.Parse(releasesURL)
	if err != nil {
		return nil, fmt.Errorf("parse MindFS releases URL: %w", err)
	}
	var releases []release
	nextURL := releasesURL
	for page := 0; page < 10 && nextURL != ""; page++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("User-Agent", "mindfs-cloud-asset-sync")
		response, err := client.Do(request)
		if err != nil {
			return nil, fmt.Errorf("list MindFS releases: %w", err)
		}
		if response.StatusCode != http.StatusOK {
			_ = response.Body.Close()
			return nil, fmt.Errorf("list MindFS releases: unexpected status %s", response.Status)
		}
		var pageReleases []release
		decoder := json.NewDecoder(io.LimitReader(response.Body, 8<<20))
		decodeErr := decoder.Decode(&pageReleases)
		linkHeader := response.Header.Get("Link")
		_ = response.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decode MindFS releases: %w", decodeErr)
		}
		releases = append(releases, pageReleases...)
		nextURL = nextReleasePage(linkHeader)
		if nextURL == "" {
			return releases, nil
		}
		next, err := url.Parse(nextURL)
		if err != nil || next.Scheme != baseURL.Scheme || next.Host != baseURL.Host {
			return nil, errors.New("MindFS releases pagination changed origin")
		}
	}
	if nextURL != "" {
		return nil, errors.New("MindFS releases pagination exceeded limit")
	}
	return releases, nil
}

func nextReleasePage(linkHeader string) string {
	for _, item := range strings.Split(linkHeader, ",") {
		parts := strings.Split(strings.TrimSpace(item), ";")
		if len(parts) < 2 || strings.TrimSpace(parts[1]) != `rel="next"` {
			continue
		}
		value := strings.TrimSpace(parts[0])
		if strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">") {
			return strings.TrimSuffix(strings.TrimPrefix(value, "<"), ">")
		}
	}
	return ""
}

func linuxReleaseAsset(item release) (releaseAsset, bool) {
	want := "mindfs_" + item.TagName + "_linux_amd64.tar.gz"
	for _, asset := range item.Assets {
		if asset.Name == want && asset.Size > 0 && asset.Size <= maxReleaseFileBytes {
			return asset, true
		}
	}
	return releaseAsset{}, false
}

func importRelease(ctx context.Context, client *http.Client, targetDir, tag string, asset releaseAsset) (int, int, map[string]string, error) {
	archivePath, err := downloadRelease(ctx, client, targetDir, tag, asset)
	if err != nil {
		return 0, 0, nil, err
	}
	defer os.Remove(archivePath)
	stagingDir, err := os.MkdirTemp(targetDir, ".release-"+tag+"-")
	if err != nil {
		return 0, 0, nil, err
	}
	defer os.RemoveAll(stagingDir)
	if err := extractReleaseAssets(archivePath, stagingDir); err != nil {
		return 0, 0, nil, fmt.Errorf("extract release %s: %w", tag, err)
	}
	added, reused := 0, 0
	assetDigests := make(map[string]string)
	err = filepath.WalkDir(stagingDir, func(sourcePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(stagingDir, sourcePath)
		if err != nil {
			return err
		}
		digest, err := fileDigest(sourcePath)
		if err != nil {
			return err
		}
		assetName := filepath.ToSlash(relative)
		immutable := isImmutableAsset(assetName)
		if immutable {
			assetDigests[assetName] = hex.EncodeToString(digest[:])
		}
		installed, err := installAsset(sourcePath, filepath.Join(targetDir, "assets", relative), immutable)
		if err != nil {
			return fmt.Errorf("merge release %s asset %s: %w", tag, relative, err)
		}
		if installed {
			added++
		} else {
			reused++
		}
		return nil
	})
	return added, reused, assetDigests, err
}

func downloadRelease(ctx context.Context, client *http.Client, targetDir, tag string, asset releaseAsset) (string, error) {
	if !strings.HasPrefix(asset.Digest, "sha256:") || len(asset.Digest) != len("sha256:")+sha256.Size*2 {
		return "", fmt.Errorf("release %s has invalid archive digest", tag)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "mindfs-cloud-asset-sync")
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download release %s: %w", tag, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download release %s: unexpected status %s", tag, response.Status)
	}
	file, err := os.CreateTemp(targetDir, ".mindfs-release-*.tar.gz")
	if err != nil {
		return "", err
	}
	archivePath := file.Name()
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = os.Remove(archivePath)
		}
	}()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, asset.Size+1))
	if err != nil {
		return "", fmt.Errorf("download release %s: %w", tag, err)
	}
	if written != asset.Size {
		return "", fmt.Errorf("download release %s: size %d does not match %d", tag, written, asset.Size)
	}
	wantDigest := strings.TrimPrefix(asset.Digest, "sha256:")
	if got := hex.EncodeToString(hash.Sum(nil)); got != wantDigest {
		return "", fmt.Errorf("download release %s: sha256 mismatch", tag)
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	keep = true
	return archivePath, nil
}

func extractReleaseAssets(archivePath, stagingDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tape := tar.NewReader(gzipReader)
	var total int64
	files := 0
	for {
		header, err := tape.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		entryName := header.Name
		if header.Typeflag == tar.TypeDir {
			entryName = strings.TrimSuffix(strings.ReplaceAll(entryName, "\\", "/"), "/")
		}
		relative, matched, err := releaseAssetPath(entryName)
		if err != nil {
			return err
		}
		if !matched {
			continue
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return fmt.Errorf("release asset %s is not a regular file", header.Name)
		}
		if header.Size < 0 || header.Size > maxReleaseFileBytes || total > maxReleaseTotalSize-header.Size {
			return fmt.Errorf("release asset %s exceeds size limit", header.Name)
		}
		total += header.Size
		destination := filepath.Join(stagingDir, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		written, copyErr := io.Copy(output, io.LimitReader(tape, header.Size))
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if written != header.Size {
			return fmt.Errorf("release asset %s was truncated", header.Name)
		}
		files++
	}
	if files == 0 {
		return errors.New("release archive contains no web assets")
	}
	return nil
}

func releaseAssetPath(name string) (string, bool, error) {
	normalized := strings.ReplaceAll(name, "\\", "/")
	marker := "/web/assets/"
	index := strings.Index("/"+normalized, marker)
	if index < 0 {
		return "", false, nil
	}
	relative := strings.TrimPrefix(("/" + normalized)[index+len(marker):], "/")
	if relative == "" {
		return "", false, nil
	}
	cleaned := path.Clean(relative)
	if cleaned != relative || cleaned == "." || path.IsAbs(cleaned) || strings.HasPrefix(cleaned, "../") {
		return "", false, fmt.Errorf("unsafe release asset path %q", name)
	}
	return cleaned, true, nil
}

func installImmutable(source, destination string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return false, err
	}
	if info, err := os.Stat(destination); err == nil {
		if !info.Mode().IsRegular() {
			return false, errors.New("destination is not a regular file")
		}
		same, err := filesEqual(source, destination)
		if err != nil {
			return false, err
		}
		if !same {
			return false, errors.New("immutable asset name has different content")
		}
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	payload, err := os.ReadFile(source)
	if err != nil {
		return false, err
	}
	if err := writeAtomic(destination, payload, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func installAsset(source, destination string, immutable bool) (bool, error) {
	if immutable {
		return installImmutable(source, destination)
	}
	if info, err := os.Stat(destination); err == nil {
		if !info.Mode().IsRegular() {
			return false, errors.New("destination is not a regular file")
		}
		same, err := filesEqual(source, destination)
		if err != nil {
			return false, err
		}
		if same {
			return false, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	payload, err := os.ReadFile(source)
	if err != nil {
		return false, err
	}
	if err := writeAtomic(destination, payload, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func isImmutableAsset(name string) bool {
	base := path.Base(name)
	extension := path.Ext(base)
	if extension == "" {
		return false
	}
	stem := strings.TrimSuffix(base, extension)
	separator := strings.LastIndexByte(stem, '-')
	if separator < 0 || len(stem)-separator-1 < 8 {
		return false
	}
	for _, character := range stem[separator+1:] {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func filesEqual(left, right string) (bool, error) {
	leftInfo, err := os.Stat(left)
	if err != nil {
		return false, err
	}
	rightInfo, err := os.Stat(right)
	if err != nil {
		return false, err
	}
	if leftInfo.Size() != rightInfo.Size() {
		return false, nil
	}
	leftHash, err := fileDigest(left)
	if err != nil {
		return false, err
	}
	rightHash, err := fileDigest(right)
	if err != nil {
		return false, err
	}
	return leftHash == rightHash, nil
}

func fileDigest(name string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	file, err := os.Open(name)
	if err != nil {
		return digest, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return digest, err
	}
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}

func writeAtomic(destination string, payload []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".asset-sync-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, destination)
}

func markerMatches(marker, targetDir string, asset releaseAsset) bool {
	payload, err := os.ReadFile(marker)
	if err != nil {
		return false
	}
	var recorded releaseMarker
	if json.Unmarshal(payload, &recorded) != nil || recorded.Archive != asset.Name || recorded.Digest != asset.Digest || len(recorded.Assets) == 0 {
		return false
	}
	for name, wantDigest := range recorded.Assets {
		cleaned := path.Clean(name)
		if cleaned != name || cleaned == "." || path.IsAbs(cleaned) || strings.HasPrefix(cleaned, "../") {
			return false
		}
		digest, err := fileDigest(filepath.Join(targetDir, "assets", filepath.FromSlash(name)))
		if err != nil || hex.EncodeToString(digest[:]) != wantDigest {
			return false
		}
	}
	return true
}

func parseVersion(tag string) ([3]int, bool) {
	var version [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(tag), "v"), ".")
	if len(parts) != len(version) {
		return version, false
	}
	for index, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return version, false
		}
		version[index] = value
	}
	return version, true
}

func compareVersion(left, right [3]int) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}
