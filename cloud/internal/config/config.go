package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	stdpath "path"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	defaultAddr              = "127.0.0.1:8080"
	defaultBindTTL           = 10 * time.Minute
	defaultAdminSessionTTL   = 12 * time.Hour
	defaultStreamOpenTimeout = 10 * time.Second
	defaultHeaderTimeout     = 30 * time.Second
	defaultMaxWSMessageBytes = int64(32 << 20)
)

type Config struct {
	Addr              string
	PublicURL         *url.URL
	DataDir           string
	AssetsDir         string
	AdminUsername     string
	AdminPassword     Secret
	TokenKey          [32]byte
	BindTTL           time.Duration
	AdminSessionTTL   time.Duration
	StreamOpenTimeout time.Duration
	HeaderTimeout     time.Duration
	MaxWSMessageBytes int64
}

type Secret string

func Load() (Config, error) {
	publicURL, err := parsePublicURL(os.Getenv("MINDFS_CLOUD_PUBLIC_URL"))
	if err != nil {
		return Config{}, err
	}
	dataDir, err := resolveDataDir(os.Getenv("MINDFS_CLOUD_DATA_DIR"))
	if err != nil {
		return Config{}, err
	}
	tokenKey, err := parseTokenKey(os.Getenv("MINDFS_CLOUD_TOKEN_KEY"))
	if err != nil {
		return Config{}, err
	}
	assetsDir, err := resolveAssetsDir(os.Getenv("MINDFS_CLOUD_ASSETS_DIR"))
	if err != nil {
		return Config{}, err
	}

	username := strings.TrimSpace(os.Getenv("MINDFS_CLOUD_ADMIN_USERNAME"))
	if username == "" {
		return Config{}, errors.New("MINDFS_CLOUD_ADMIN_USERNAME is required")
	}
	password := Secret(os.Getenv("MINDFS_CLOUD_ADMIN_PASSWORD"))
	if password == "" {
		return Config{}, errors.New("MINDFS_CLOUD_ADMIN_PASSWORD is required")
	}

	return Config{
		Addr:              defaultString(os.Getenv("MINDFS_CLOUD_ADDR"), defaultAddr),
		PublicURL:         publicURL,
		DataDir:           dataDir,
		AssetsDir:         assetsDir,
		AdminUsername:     username,
		AdminPassword:     password,
		TokenKey:          tokenKey,
		BindTTL:           defaultBindTTL,
		AdminSessionTTL:   defaultAdminSessionTTL,
		StreamOpenTimeout: defaultStreamOpenTimeout,
		HeaderTimeout:     defaultHeaderTimeout,
		MaxWSMessageBytes: defaultMaxWSMessageBytes,
	}, nil
}

func resolveAssetsDir(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("MINDFS_CLOUD_ASSETS_DIR is required")
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve MINDFS_CLOUD_ASSETS_DIR: %w", err)
	}
	if err := CheckAssetsDir(absolute); err != nil {
		return "", err
	}
	return absolute, nil
}

func CheckAssetsDir(absolute string) error {
	indexInfo, err := os.Stat(filepath.Join(absolute, "index.html"))
	if err != nil {
		return errors.New("MINDFS_CLOUD_ASSETS_DIR missing index.html")
	}
	if indexInfo.IsDir() {
		return errors.New("MINDFS_CLOUD_ASSETS_DIR index.html must be a file")
	}
	assetsInfo, err := os.Stat(filepath.Join(absolute, "assets"))
	if err != nil {
		return errors.New("MINDFS_CLOUD_ASSETS_DIR missing assets")
	}
	if !assetsInfo.IsDir() {
		return errors.New("MINDFS_CLOUD_ASSETS_DIR assets must be a directory")
	}
	return checkIndexAssetReferences(absolute)
}

func checkIndexAssetReferences(absolute string) error {
	indexFile, err := os.Open(filepath.Join(absolute, "index.html"))
	if err != nil {
		return errors.New("MINDFS_CLOUD_ASSETS_DIR missing index.html")
	}
	defer indexFile.Close()
	assetsRoot, err := os.OpenRoot(filepath.Join(absolute, "assets"))
	if err != nil {
		return errors.New("MINDFS_CLOUD_ASSETS_DIR missing assets")
	}
	defer assetsRoot.Close()

	tokenizer := html.NewTokenizer(io.LimitReader(indexFile, 4<<20))
	checked := make(map[string]struct{})
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			if errors.Is(tokenizer.Err(), io.EOF) {
				return nil
			}
			return fmt.Errorf("MINDFS_CLOUD_ASSETS_DIR parse index.html: %w", tokenizer.Err())
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			for _, attribute := range token.Attr {
				if attribute.Key != "src" && attribute.Key != "href" {
					continue
				}
				assetPath, matched, err := indexAssetPath(attribute.Val)
				if err != nil {
					return err
				}
				if !matched {
					continue
				}
				if _, exists := checked[assetPath]; exists {
					continue
				}
				checked[assetPath] = struct{}{}
				file, err := assetsRoot.Open(filepath.FromSlash(assetPath))
				if err != nil {
					return fmt.Errorf("MINDFS_CLOUD_ASSETS_DIR missing index asset %s", assetPath)
				}
				info, statErr := file.Stat()
				_ = file.Close()
				if statErr != nil || !info.Mode().IsRegular() {
					return fmt.Errorf("MINDFS_CLOUD_ASSETS_DIR index asset %s must be a file", assetPath)
				}
			}
		}
	}
}

func indexAssetPath(reference string) (string, bool, error) {
	value := strings.TrimSpace(reference)
	if value == "" || strings.HasPrefix(value, "//") {
		return "", false, nil
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", false, fmt.Errorf("MINDFS_CLOUD_ASSETS_DIR invalid index asset reference %q", value)
	}
	if parsed.IsAbs() || parsed.Host != "" {
		return "", false, nil
	}
	assetPath := strings.TrimPrefix(parsed.Path, "./")
	assetPath = strings.TrimPrefix(assetPath, "/")
	if strings.Contains(assetPath, "\\") {
		return "", false, fmt.Errorf("MINDFS_CLOUD_ASSETS_DIR unsafe index asset reference %q", value)
	}
	switch {
	case strings.HasPrefix(assetPath, "assets/"):
		assetPath = strings.TrimPrefix(assetPath, "assets/")
	case strings.HasPrefix(assetPath, "mindfs-assets/"):
		assetPath = strings.TrimPrefix(assetPath, "mindfs-assets/")
	default:
		return "", false, nil
	}
	cleaned := stdpath.Clean(assetPath)
	if assetPath == "" || cleaned != assetPath || cleaned == "." || strings.HasPrefix(cleaned, "../") {
		return "", false, fmt.Errorf("MINDFS_CLOUD_ASSETS_DIR unsafe index asset reference %q", value)
	}
	return cleaned, true, nil
}

func parsePublicURL(value string) (*url.URL, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("MINDFS_CLOUD_PUBLIC_URL is required")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("invalid MINDFS_CLOUD_PUBLIC_URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("MINDFS_CLOUD_PUBLIC_URL must use http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("MINDFS_CLOUD_PUBLIC_URL must be absolute")
	}
	if parsed.User != nil {
		return nil, errors.New("MINDFS_CLOUD_PUBLIC_URL must not contain credentials")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, nil
}

func resolveDataDir(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolve user config directory: %w", err)
		}
		value = filepath.Join(configDir, "mindfs-cloud")
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve MINDFS_CLOUD_DATA_DIR: %w", err)
	}
	return absolute, nil
}

func parseTokenKey(value string) ([32]byte, error) {
	var out [32]byte
	value = strings.TrimSpace(value)
	if value == "" {
		return out, errors.New("MINDFS_CLOUD_TOKEN_KEY is required")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return out, errors.New("MINDFS_CLOUD_TOKEN_KEY must be unpadded base64url")
	}
	if len(decoded) != len(out) {
		return out, errors.New("MINDFS_CLOUD_TOKEN_KEY must decode to 32 bytes")
	}
	copy(out[:], decoded)
	return out, nil
}

func defaultString(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}
