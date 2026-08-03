package compat_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	compatAdminUsername = "admin"
	compatAdminPassword = "compat-admin-password"
)

type bindObservation struct {
	Code          string
	PairingSecret string
	NodeName      string
}

type observationCollector struct {
	mu    sync.Mutex
	value bindObservation
	ready chan struct{}
	once  sync.Once
}

func newObservationCollector() *observationCollector {
	return &observationCollector{ready: make(chan struct{})}
}

func (c *observationCollector) observe(line string) {
	line = strings.TrimSpace(line)
	c.mu.Lock()
	defer c.mu.Unlock()
	if strings.HasPrefix(line, "pairing secret:") {
		c.value.PairingSecret = strings.TrimSpace(strings.TrimPrefix(line, "pairing secret:"))
	}
	if parsed, err := url.Parse(line); err == nil && parsed.IsAbs() && parsed.Path == "/bind" {
		c.value.Code = strings.TrimSpace(parsed.Query().Get("code"))
		c.value.NodeName = strings.TrimSpace(parsed.Query().Get("node_name"))
	}
	if c.value.Code != "" && c.value.PairingSecret != "" {
		c.once.Do(func() { close(c.ready) })
	}
}

func (c *observationCollector) wait(ctx context.Context) (bindObservation, error) {
	select {
	case <-ctx.Done():
		return bindObservation{}, ctx.Err()
	case <-c.ready:
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.value, nil
	}
}

type compatibilityRun struct {
	ctx        context.Context
	repoRoot   string
	runDir     string
	cloudAddr  string
	nodeAddr   string
	cloudURL   string
	cloudData  string
	cloudBin   string
	nodeBin    string
	homeDir    string
	staticDir  string
	rootDir    string
	pathDir    string
	tokenKey   string
	cloud      *managedProcess
	node       *managedProcess
	observe    *observationCollector
	bind       bindObservation
	nodeID     string
	httpClient *http.Client
}

func newCompatibilityRun(ctx context.Context, runDir string) (*compatibilityRun, error) {
	repoRoot, err := findRepoRoot(".")
	if err != nil {
		return nil, err
	}
	cloudAddr, err := freeLoopbackAddress()
	if err != nil {
		return nil, err
	}
	nodeAddr, err := freeLoopbackAddress()
	if err != nil {
		return nil, err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	run := &compatibilityRun{
		ctx:       ctx,
		repoRoot:  repoRoot,
		runDir:    runDir,
		cloudAddr: cloudAddr,
		nodeAddr:  nodeAddr,
		cloudURL:  "http://" + cloudAddr,
		cloudData: filepath.Join(runDir, "cloud-data"),
		cloudBin:  filepath.Join(runDir, "mindfs-relay"),
		nodeBin:   strings.TrimSpace(os.Getenv("MINDFS_COMPAT_NODE_BINARY")),
		homeDir:   filepath.Join(runDir, "home"),
		staticDir: filepath.Join(runDir, "static"),
		rootDir:   filepath.Join(runDir, "root"),
		pathDir:   filepath.Join(runDir, "path"),
		tokenKey:  base64.RawURLEncoding.EncodeToString(make([]byte, 32)),
		observe:   newObservationCollector(),
		httpClient: &http.Client{
			Jar:       jar,
			Timeout:   5 * time.Second,
			Transport: &http.Transport{Proxy: nil},
		},
	}
	for _, dir := range []string{run.homeDir, run.staticDir, run.rootDir, run.pathDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	if err := writeFixture(run.staticDir); err != nil {
		return nil, err
	}
	return run, nil
}

func (r *compatibilityRun) build() error {
	if r.nodeBin == "" {
		r.nodeBin = filepath.Join(r.runDir, "mindfs")
		if err := buildBinary(r.ctx, r.repoRoot, r.nodeBin, "-ldflags", "-X main.version=v0.1.0", "./cli/cmd"); err != nil {
			return fmt.Errorf("node: %w", err)
		}
	} else {
		absolute, err := filepath.Abs(r.nodeBin)
		if err != nil {
			return fmt.Errorf("node binary: %w", err)
		}
		r.nodeBin = absolute
		if !fileExists(r.nodeBin) {
			return fmt.Errorf("node binary does not exist: %s", filepath.Base(r.nodeBin))
		}
	}
	if err := buildBinary(r.ctx, filepath.Join(r.repoRoot, "cloud"), r.cloudBin, "./cmd/mindfs-relay"); err != nil {
		return fmt.Errorf("cloud: %w", err)
	}
	return nil
}

func (r *compatibilityRun) startCloud() error {
	cloud, err := startProcess(r.ctx, processSpec{
		name:   "test-cloud",
		binary: r.cloudBin,
		env: isolatedEnv(map[string]string{
			"MINDFS_CLOUD_ADDR":           r.cloudAddr,
			"MINDFS_CLOUD_PUBLIC_URL":     r.cloudURL,
			"MINDFS_CLOUD_DATA_DIR":       r.cloudData,
			"MINDFS_CLOUD_ASSETS_DIR":     r.staticDir,
			"MINDFS_CLOUD_ADMIN_USERNAME": compatAdminUsername,
			"MINDFS_CLOUD_ADMIN_PASSWORD": compatAdminPassword,
			"MINDFS_CLOUD_TOKEN_KEY":      r.tokenKey,
		}),
	})
	if err != nil {
		return err
	}
	r.cloud = cloud
	ctx, cancel := context.WithTimeout(r.ctx, stageTimeout)
	defer cancel()
	if err := waitForHTTP(ctx, r.httpClient, r.cloudURL+"/healthz", func(response *http.Response, _ []byte) bool {
		return response.StatusCode == http.StatusOK
	}); err != nil {
		return fmt.Errorf("cloud health: %w; cloud=%s", err, cloud.logTail())
	}
	return nil
}

func (r *compatibilityRun) restartCloud() error {
	if r.cloud == nil {
		return errors.New("test cloud is not running")
	}
	if err := r.cloud.stop(3 * time.Second); err != nil {
		return err
	}
	r.cloud = nil
	return r.startCloud()
}

func (r *compatibilityRun) startNode() error {
	node, err := startProcess(r.ctx, processSpec{
		name:    "test-node",
		binary:  r.nodeBin,
		args:    []string{"-foreground", "-e2ee", "-web-push=false", "-bind-relay", "-addr", r.nodeAddr, r.rootDir},
		workDir: r.rootDir,
		onLine:  r.observe.observe,
		env: isolatedEnv(map[string]string{
			"HOME":                  r.homeDir,
			"XDG_CONFIG_HOME":       filepath.Join(r.homeDir, ".config"),
			"MINDFS_STATIC_DIR":     r.staticDir,
			"MINDFS_RELAY_BASE_URL": r.cloudURL,
			"PATH":                  r.pathDir,
			"HTTP_PROXY":            "http://127.0.0.1:1",
			"HTTPS_PROXY":           "http://127.0.0.1:1",
			"ALL_PROXY":             "http://127.0.0.1:1",
			"NO_PROXY":              "127.0.0.1,localhost",
		}),
	})
	if err != nil {
		return err
	}
	r.node = node
	ctx, cancel := context.WithTimeout(r.ctx, stageTimeout)
	defer cancel()
	if err := waitForHTTP(ctx, r.httpClient, "http://"+r.nodeAddr+"/health", func(response *http.Response, body []byte) bool {
		return response.StatusCode == http.StatusOK && string(body) == "ok"
	}); err != nil {
		return fmt.Errorf("node health: %w; node=%s", err, node.logTail())
	}
	observation, err := r.observe.wait(ctx)
	if err != nil {
		return fmt.Errorf("bind URL was not observed before timeout; node=%s", node.logTail())
	}
	r.bind = observation
	return nil
}

func (r *compatibilityRun) bindNode() error {
	csrf, err := r.adminLogin()
	if err != nil {
		return err
	}
	if err := r.waitForPendingBinding(); err != nil {
		return err
	}
	var confirmed struct {
		Status string `json:"status"`
		NodeID string `json:"node_id"`
	}
	request := map[string]string{
		"code":      r.bind.Code,
		"action":    "confirm",
		"node_name": "Compatibility Node",
	}
	if err := r.doJSON(http.MethodPost, r.cloudURL+"/api/bind/confirm", request, map[string]string{"X-CSRF-Token": csrf}, &confirmed); err != nil {
		return err
	}
	if confirmed.Status != "confirmed" || strings.TrimSpace(confirmed.NodeID) == "" {
		return fmt.Errorf("unexpected confirmation response status=%q", confirmed.Status)
	}
	r.nodeID = confirmed.NodeID
	return nil
}

func (r *compatibilityRun) adminLogin() (string, error) {
	var response struct {
		CSRFToken string `json:"csrf_token"`
	}
	err := r.doJSON(http.MethodPost, r.cloudURL+"/api/cloud/v1/auth/login", map[string]string{
		"username": compatAdminUsername,
		"password": compatAdminPassword,
	}, nil, &response)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(response.CSRFToken) == "" {
		return "", errors.New("admin login returned no CSRF token")
	}
	return response.CSRFToken, nil
}

func (r *compatibilityRun) waitForPendingBinding() error {
	ctx, cancel := context.WithTimeout(r.ctx, stageTimeout)
	defer cancel()
	target := r.cloudURL + "/api/bind/status?code=" + url.QueryEscape(r.bind.Code)
	err := waitForHTTP(ctx, r.httpClient, target, func(response *http.Response, body []byte) bool {
		if response.StatusCode != http.StatusOK {
			return false
		}
		var status struct {
			Status string `json:"status"`
		}
		return json.Unmarshal(body, &status) == nil && status.Status == "pending"
	})
	if err != nil {
		return errors.New("binding did not become pending before timeout")
	}
	return nil
}

func (r *compatibilityRun) waitForPublicHealth() error {
	ctx, cancel := context.WithTimeout(r.ctx, stageTimeout)
	defer cancel()
	return waitForHTTP(ctx, r.httpClient, r.publicNodeURL("/health"), func(response *http.Response, body []byte) bool {
		return response.StatusCode == http.StatusOK && string(body) == "ok"
	})
}

func (r *compatibilityRun) verifyHTTP() error {
	indexResponse, indexBody, err := r.request(http.MethodGet, r.publicNodeURL("/"), nil, nil)
	if err != nil {
		return err
	}
	if indexResponse.StatusCode != http.StatusOK || !bytes.Contains(indexBody, []byte("compat-index")) {
		return fmt.Errorf("fixture index returned %s", indexResponse.Status)
	}
	if bytes.Contains(indexBody, []byte("./assets/")) || !bytes.Contains(indexBody, []byte("/mindfs-assets/app.js")) {
		return errors.New("relayed fixture index did not rewrite asset paths")
	}
	sharedAssetResponse, sharedAssetBody, err := r.request(http.MethodGet, r.cloudURL+"/mindfs-assets/app.js", nil, nil)
	if err != nil {
		return err
	}
	if sharedAssetResponse.StatusCode != http.StatusOK || string(sharedAssetBody) != `window.__mindfsCompat = "asset-ok";` {
		return fmt.Errorf("rewritten shared asset returned %s", sharedAssetResponse.Status)
	}

	assetResponse, assetBody, err := r.request(http.MethodGet, r.publicNodeURL("/assets/app.js")+"?cache=compat", nil, nil)
	if err != nil {
		return err
	}
	if assetResponse.StatusCode != http.StatusOK || string(assetBody) != `window.__mindfsCompat = "asset-ok";` {
		return fmt.Errorf("fixture asset returned %s", assetResponse.Status)
	}

	openPayload := []byte(`{"client_id":"compat-http","node_id":"wrong-node","client_eph_pk":"invalid","client_nonce":"invalid","proof":"invalid"}`)
	openResponse, _, err := r.request(http.MethodPost, r.publicNodeURL("/api/e2ee/open")+"?compat=query", openPayload, map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return err
	}
	if openResponse.StatusCode != http.StatusForbidden {
		return fmt.Errorf("POST body did not reach Node E2EE handler: %s", openResponse.Status)
	}
	return nil
}

func (r *compatibilityRun) publicNodeURL(path string) string {
	return r.cloudURL + "/n/" + url.PathEscape(r.nodeID) + "/" + strings.TrimPrefix(path, "/")
}

func (r *compatibilityRun) request(method, target string, payload []byte, headers map[string]string) (*http.Response, []byte, error) {
	request, err := http.NewRequestWithContext(r.ctx, method, target, bytes.NewReader(payload))
	if err != nil {
		return nil, nil, err
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := r.httpClient.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return response, nil, err
	}
	return response, body, nil
}

func (r *compatibilityRun) doJSON(method, target string, input any, headers map[string]string, output any) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(r.ctx, method, target, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := r.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("%s %s returned %s", method, request.URL.Path, response.Status)
	}
	if output == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(output)
}

func (r *compatibilityRun) close() error {
	var errs []error
	if r.node != nil {
		if err := r.node.stop(3 * time.Second); err != nil {
			errs = append(errs, err)
		}
		r.node = nil
	}
	if r.cloud != nil {
		if err := r.cloud.stop(3 * time.Second); err != nil {
			errs = append(errs, err)
		}
		r.cloud = nil
	}
	return errors.Join(errs...)
}

func (r *compatibilityRun) verifyPortsReleased() error {
	for name, address := range map[string]string{"cloud": r.cloudAddr, "node": r.nodeAddr} {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return fmt.Errorf("%s port was not released", name)
		}
		_ = listener.Close()
	}
	return nil
}

func writeFixture(staticDir string) error {
	assetDir := filepath.Join(staticDir, "assets")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		return err
	}
	files := map[string]string{
		filepath.Join(staticDir, "index.html"):  `<!doctype html><link rel="icon" href="./favicon.svg"><script src="./assets/app.js"></script><main>compat-index</main>`,
		filepath.Join(staticDir, "favicon.svg"): `<svg xmlns="http://www.w3.org/2000/svg"></svg>`,
		filepath.Join(assetDir, "app.js"):       `window.__mindfsCompat = "asset-ok";`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write fixture %s: %w", filepath.Base(path), err)
		}
	}
	return nil
}
