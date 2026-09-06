package compat_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestCurrentNodeAPICompatibility(t *testing.T) {
	if os.Getenv("MINDFS_RUN_COMPAT") != "1" {
		t.Skip("requires MINDFS_RUN_COMPAT=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()
	r, err := newCompatibilityRun(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.close(); err != nil {
			t.Error(err)
		}
	}()
	for _, stage := range []func() error{r.build, r.startCloud, r.startNode, r.bindNode, r.waitForPublicHealth} {
		if err := stage(); err != nil {
			t.Fatal(err)
		}
	}
	session, err := r.openE2EESession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.close()
	call := func(method, path, payload string, want int, key string, value any) map[string]any {
		t.Helper()
		headers := requestProofHeaders(session, method, path)
		var body []byte
		if payload != "" {
			envelope, err := encryptBytes(session.Key, []byte(payload))
			if err != nil {
				t.Fatal(err)
			}
			body, err = json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
		}
		resp, data, err := r.request(method, r.publicNodeURL(path), body, headers)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != want || resp.Header.Get("X-MindFS-E2EE") != "1" {
			t.Fatalf("%s %s expected encrypted status %d, got %d", method, path, want, resp.StatusCode)
		}
		var envelope cipherEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatal(err)
		}
		plain, err := decryptBytes(session.Key, &envelope)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		if err := json.Unmarshal(plain, &result); err != nil {
			t.Fatal(err)
		}
		got, exists := result[key]
		if !exists {
			t.Fatalf("%s %s missing expected response key %s", method, path, key)
		}
		if value != nil && got != value {
			t.Fatalf("%s %s unexpected value for %s", method, path, key)
		}
		t.Logf("%s %s -> %d, encrypted response decoded and field %s verified", method, path, want, key)
		return result
	}
	call("GET", "/api/agents/memory", "", 200, "idle_hours", float64(72))
	call("POST", "/api/agents/release-idle", "{}", 200, "released_sessions", float64(0))
	call("PUT", "/api/preferences/idle-session-resource-release", `{"hours":24}`, 200, "hours", float64(24))
	call("GET", "/api/preferences/idle-session-resource-release", "", 200, "hours", float64(24))
	call("PUT", "/api/preferences/new-project-meta-location", `{"location":"home"}`, 200, "location", "home")
	call("GET", "/api/preferences/new-project-meta-location", "", 200, "location", "home")
	call("PUT", "/api/preferences/session-naming", `{"disabled":true}`, 200, "disabled", true)
	call("GET", "/api/preferences/session-naming", "", 200, "disabled", true)
	saved := call("POST", "/api/prompts", `{"text":"isolated-release-audit"}`, 200, "items", nil)
	if len(saved["items"].([]any)) != 1 {
		t.Fatal("prompt was not saved")
	}
	deleted := call("DELETE", "/api/prompts", `{"text":"isolated-release-audit"}`, 200, "items", nil)
	if items, ok := deleted["items"].([]any); deleted["items"] != nil && (!ok || len(items) != 0) {
		t.Fatal("prompt was not deleted")
	}
	call("GET", "/api/agents/codex/rate-limits?agent=audit-unconfigured", "", 502, "error", "agent not configured: audit-unconfigured")
	call("POST", "/api/agents/codex/rate-limit-reset", `{}`, 400, "error", "idempotency_key required")

	// The existing Node route accepts encoded identifiers. Use nonexistent
	// test data so the test exercises proof verification without running an Agent.
	for _, path := range []string{
		"/api/sessions/missing/toolcalls/task%3A1?root=missing",
		"/api/sessions/missing/toolcalls/task%3a1?root=missing",
		"/api/sessions/missing/toolcalls/task%2F1?root=missing",
		"/api/sessions/missing/toolcalls/task%252F1?root=missing",
	} {
		direct, directBody, err := r.request(http.MethodGet, "http://"+r.nodeAddr+path, nil, requestProofHeaders(session, http.MethodGet, path))
		if err != nil {
			t.Fatal(err)
		}
		if direct.StatusCode == 401 || direct.Header.Get("X-MindFS-E2EE") != "1" {
			t.Fatalf("Node did not accept encoded path proof for %s: %d", path, direct.StatusCode)
		}
		var envelope cipherEnvelope
		if err := json.Unmarshal(directBody, &envelope); err != nil {
			t.Fatal(err)
		}
		plain, err := decryptBytes(session.Key, &envelope)
		if err != nil {
			t.Fatal(err)
		}
		var directError map[string]any
		if err := json.Unmarshal(plain, &directError); err != nil {
			t.Fatal(err)
		}
		call(http.MethodGet, path, "", direct.StatusCode, "error", directError["error"])
	}
	if err := r.close(); err != nil {
		t.Fatal(err)
	}
	if err := r.verifyPortsReleased(); err != nil {
		t.Fatal(err)
	}
}

func requestProofHeaders(session *e2eeSession, method, path string) map[string]string {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	return map[string]string{
		"Content-Type": "application/json", "X-MindFS-E2EE": "1", "X-MindFS-Client-ID": session.ClientID,
		"X-MindFS-TS": timestamp, "X-MindFS-Proof": buildRequestProof(session.Key, method, path, timestamp, session.ClientID),
	}
}
