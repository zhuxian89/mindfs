package compat_test

import (
	"context"
	"os"
	"testing"
	"time"
)

const (
	totalTimeout = 120 * time.Second
	stageTimeout = 15 * time.Second
)

func TestUnmodifiedNodeRelayCompatibility(t *testing.T) {
	if os.Getenv("MINDFS_RUN_COMPAT") != "1" {
		t.Skip("relay compatibility suite requires MINDFS_RUN_COMPAT=1")
	}

	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()
	run, err := newCompatibilityRun(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("compat stage=preflight failed: %v", err)
	}
	defer func() {
		if err := run.close(); err != nil {
			t.Errorf("compat cleanup failed: %v", err)
		}
	}()
	if err := run.build(); err != nil {
		t.Fatalf("compat stage=build failed: %v", err)
	}
	if err := run.startCloud(); err != nil {
		t.Fatalf("compat stage=cloud_start failed: %v", err)
	}
	if err := run.startNode(); err != nil {
		t.Fatalf("compat stage=node_start failed: %v", err)
	}
	if err := run.bindNode(); err != nil {
		t.Fatalf("compat stage=bind_confirm failed: %v", err)
	}
	if err := run.waitForPublicHealth(); err != nil {
		t.Fatalf("compat stage=connector_wait failed: %v", err)
	}
	if err := run.verifyHTTP(); err != nil {
		t.Fatalf("compat stage=http failed: %v", err)
	}
	session, err := run.openE2EESession()
	if err != nil {
		t.Fatalf("compat stage=e2ee_open failed: %v", err)
	}
	defer session.close()
	if err := run.verifyProtectedHTTP(session); err != nil {
		t.Fatalf("compat stage=e2ee_http failed: %v", err)
	}
	if err := run.verifyEncryptedWebSocket(session); err != nil {
		t.Fatalf("compat stage=ws_e2ee failed: %v", err)
	}
	if err := run.restartCloud(); err != nil {
		t.Fatalf("compat stage=cloud_restart failed: %v", err)
	}
	if err := run.waitForPublicHealth(); err != nil {
		t.Fatalf("compat stage=reconnect failed: %v", err)
	}
	if err := run.close(); err != nil {
		t.Fatalf("compat stage=cleanup failed: %v", err)
	}
	if err := run.verifyPortsReleased(); err != nil {
		t.Fatalf("compat stage=cleanup failed: %v", err)
	}
}
