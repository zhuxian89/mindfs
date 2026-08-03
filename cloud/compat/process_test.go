package compat_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const processLogLimit = 12

type processSpec struct {
	name    string
	binary  string
	args    []string
	env     []string
	workDir string
	onLine  func(string)
}

type managedProcess struct {
	name string
	cmd  *exec.Cmd
	done chan error

	mu   sync.Mutex
	logs []string
}

func startProcess(ctx context.Context, spec processSpec) (*managedProcess, error) {
	cmd := exec.CommandContext(ctx, spec.binary, spec.args...)
	cmd.Dir = spec.workDir
	cmd.Env = spec.env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%s stdout: %w", spec.name, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("%s stderr: %w", spec.name, err)
	}
	process := &managedProcess{name: spec.name, cmd: cmd, done: make(chan error, 1)}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", spec.name, err)
	}

	var readers sync.WaitGroup
	readers.Add(2)
	go process.readLines(stdout, spec.onLine, &readers)
	go process.readLines(stderr, spec.onLine, &readers)
	go func() {
		err := cmd.Wait()
		readers.Wait()
		process.done <- err
		close(process.done)
	}()
	return process, nil
}

func (p *managedProcess) readLines(reader io.Reader, onLine func(string), readers *sync.WaitGroup) {
	defer readers.Done()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if onLine != nil {
			onLine(line)
		}
		p.appendLog(redactProcessLine(line))
	}
	if err := scanner.Err(); err != nil {
		p.appendLog("output reader failed")
	}
}

func (p *managedProcess) appendLog(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.logs = append(p.logs, line)
	if len(p.logs) > processLogLimit {
		p.logs = append([]string(nil), p.logs[len(p.logs)-processLogLimit:]...)
	}
}

func (p *managedProcess) logTail() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.logs, " | ")
}

func (p *managedProcess) stop(timeout time.Duration) error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	select {
	case err := <-p.done:
		return normalizeProcessExit(err)
	default:
	}
	_ = p.cmd.Process.Kill()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-p.done:
		return normalizeProcessExit(err)
	case <-timer.C:
		return fmt.Errorf("%s did not exit after kill", p.name)
	}
}

func normalizeProcessExit(err error) error {
	var exitErr *exec.ExitError
	if err == nil || errors.As(err, &exitErr) {
		return nil
	}
	return err
}

func redactProcessLine(line string) string {
	lower := strings.ToLower(line)
	for _, marker := range []string{
		"pairing secret", "device_token", "device token", "authorization",
		"ciphertext", "x-mindfs-proof", "x-mindfs-ts",
	} {
		if strings.Contains(lower, marker) {
			return "[redacted sensitive process output]"
		}
	}
	if strings.Contains(line, "/bind?") && strings.Contains(line, "code=") {
		return "relay bind URL observed [redacted]"
	}
	return line
}

func buildBinary(ctx context.Context, workDir, output string, args ...string) error {
	commandArgs := append([]string{"build", "-o", output}, args...)
	cmd := exec.CommandContext(ctx, "go", commandArgs...)
	cmd.Dir = workDir
	payload, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go build %s: %w: %s", filepath.Base(output), err, summarizeBuildOutput(payload))
	}
	return nil
}

func summarizeBuildOutput(payload []byte) string {
	lines := strings.Split(strings.TrimSpace(string(payload)), "\n")
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	for i := range lines {
		lines[i] = redactProcessLine(lines[i])
	}
	return strings.Join(lines, " | ")
}

func freeLoopbackAddress() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", err
	}
	return address, nil
}

func waitForHTTP(ctx context.Context, client *http.Client, target string, accept func(*http.Response, []byte) bool) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastStatus string
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			_ = response.Body.Close()
			if readErr == nil && accept(response, body) {
				return nil
			}
			lastStatus = response.Status
		} else {
			lastStatus = "connection unavailable"
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for %s: %s: %w", target, lastStatus, ctx.Err())
		case <-ticker.C:
		}
	}
}

func isolatedEnv(overrides map[string]string) []string {
	values := make(map[string]string, len(overrides)+8)
	for _, key := range []string{"TMPDIR", "TMP", "TEMP", "LANG", "LC_ALL", "TZ", "SYSTEMROOT", "WINDIR"} {
		if value := os.Getenv(key); value != "" {
			values[key] = value
		}
	}
	for key, value := range overrides {
		values[key] = value
	}
	out := make([]string, 0, len(values))
	for key, value := range values {
		out = append(out, key+"="+value)
	}
	return out
}

func findRepoRoot(start string) (string, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if fileExists(filepath.Join(current, "go.mod")) && fileExists(filepath.Join(current, "cloud", "go.mod")) {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("repository root not found")
		}
		current = parent
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func TestRedactProcessLine(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "secret", input: "pairing secret: actual-secret"},
		{name: "bind URL", input: "http://127.0.0.1:8080/bind?code=pc_actual"},
		{name: "token", input: `{"device_token":"actual-token"}`},
		{name: "ciphertext", input: `{"ciphertext":"actual-ciphertext"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			redacted := redactProcessLine(test.input)
			if strings.Contains(redacted, "actual") {
				t.Fatalf("sensitive value remained in %q", redacted)
			}
		})
	}
}

func TestIsolatedEnvDoesNotInheritCredentials(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "host-secret")
	environment := isolatedEnv(map[string]string{"HOME": t.TempDir()})
	for _, entry := range environment {
		if strings.HasPrefix(entry, "OPENAI_API_KEY=") || strings.Contains(entry, "host-secret") {
			t.Fatalf("credential inherited by child environment")
		}
	}
}
