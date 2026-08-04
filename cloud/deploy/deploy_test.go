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
		"FROM golang:1.25-alpine AS cloud-build",
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

func read(t *testing.T, path string) string {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}
