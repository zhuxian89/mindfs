package deploy_test

import (
	"os"
	"strings"
	"testing"
)

func TestDeploymentFilesContainRequiredBoundaries(t *testing.T) {
	dockerfile := read(t, "../Dockerfile")
	for _, required := range []string{
		"FROM node:22-alpine AS web-build",
		"FROM golang:1.26.6-alpine AS cloud-build",
		"FROM gcr.io/distroless/static-debian12:nonroot",
		"USER 65532:65532",
		"MINDFS_CLOUD_ASSETS_DIR=/opt/mindfs/web",
		`HEALTHCHECK`,
	} {
		if !strings.Contains(dockerfile, required) {
			t.Fatalf("Dockerfile missing %q", required)
		}
	}
	compose := read(t, "docker-compose.yml")
	for _, required := range []string{
		"dockerfile: cloud/Dockerfile",
		"sync-assets",
		"service_completed_successfully",
		"relay-data",
		"relay-assets",
		"/var/lib/mindfs-assets:ro",
		"relay-backups",
		"condition: service_healthy",
	} {
		if !strings.Contains(compose, required) {
			t.Fatalf("compose missing %q", required)
		}
	}
	caddy := read(t, "Caddyfile.example")
	if !strings.Contains(caddy, "reverse_proxy relay:8080") {
		t.Fatal("Caddyfile does not proxy Relay")
	}
}

func TestProductionComposeUsesOnePublishedImage(t *testing.T) {
	compose := read(t, "docker-compose.1panel.yml")
	for _, required := range []string{
		"image: ${RELAY_IMAGE:-ghcr.io/zhuxian89/mindfs-relay:latest}",
		"container_name: mindfs-relay",
		"127.0.0.1:13005:8080",
		"relay-data:/var/lib/mindfs-cloud",
		"relay-assets:/var/lib/mindfs-assets:ro",
		"relay-backups:/backups",
	} {
		if !strings.Contains(compose, required) {
			t.Fatalf("production compose missing %q", required)
		}
	}
	if strings.Contains(compose, "build:") || strings.Count(compose, "<<: *relay-image") != 2 {
		t.Fatal("production services must share one published image without building")
	}
}

func TestRelayImageWorkflowSeparatesPublishingFromDeployment(t *testing.T) {
	workflow := read(t, "../../.github/workflows/build-relay.yml")
	for _, required := range []string{
		"branches: [main]",
		"if: github.ref == 'refs/heads/main'",
		"contents: read",
		"packages: write",
		"cancel-in-progress: false",
		"go-version-file: cloud/go.mod",
		"run: go test ./...",
		"context: .",
		"file: cloud/Dockerfile",
		"platforms: linux/amd64,linux/arm64",
		"tags: ${{ env.IMAGE }}:${{ github.sha }}",
		"git fetch --no-tags origin main",
		`if [ "$(git rev-parse FETCH_HEAD)" != "$GITHUB_SHA" ]; then`,
		`docker buildx imagetools create --tag "$IMAGE:latest" "$IMAGE@$DIGEST"`,
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("workflow missing %q", required)
		}
	}
	for _, forbidden := range []string{"ssh ", "ssh-action", "auto-upgrade.sh", "docker compose up"} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("publishing workflow must not deploy: %q", forbidden)
		}
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}
