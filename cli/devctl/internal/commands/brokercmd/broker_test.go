package brokercmd

import (
	"os"
	"path/filepath"
	"testing"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/compose"
)

func TestParseUsesOverlayBrokerConfig(t *testing.T) {
	tmp := t.TempDir()
	overlay := filepath.Join(tmp, "overlays", "dev-all")
	if err := os.MkdirAll(overlay, 0o755); err != nil {
		t.Fatalf("mkdir overlay: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "devkit.yaml"), []byte(`
broker:
  socket: /tmp/devkit-test.sock
  upstream: unix:///tmp/docker.sock
  allowed_images:
    - postgres:15
    - minio/minio:latest
  allow_pulls: true
  log_level: debug
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	parsed, err := parse(&cmdregistry.Context{
		Project: "dev-all",
		Args:    []string{"status"},
		Paths:   compose.Paths{Root: filepath.Join(tmp, "devkit"), OverlayPaths: []string{filepath.Join(tmp, "overlays")}},
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.cfg.Socket != "/tmp/devkit-test.sock" {
		t.Fatalf("socket = %q", parsed.cfg.Socket)
	}
	if parsed.cfg.Upstream != "unix:///tmp/docker.sock" {
		t.Fatalf("upstream = %q", parsed.cfg.Upstream)
	}
	if got := parsed.cfg.AllowedImages; len(got) != 2 || got[0] != "postgres:15" || got[1] != "minio/minio:latest" {
		t.Fatalf("allowed images = %#v", got)
	}
	if !parsed.cfg.AllowPulls {
		t.Fatalf("allow pulls = false")
	}
	if parsed.cfg.LogLevel != "debug" {
		t.Fatalf("log level = %q", parsed.cfg.LogLevel)
	}
}

func TestParseCLIAllowedImagesOverrideOverlay(t *testing.T) {
	tmp := t.TempDir()
	parsed, err := parse(&cmdregistry.Context{
		Project: "dev-all",
		Args:    []string{"start", "--allow-image", "postgres:16", "--allow-image", "redis:7", "--socket", "/tmp/b.sock"},
		Paths:   compose.Paths{Root: filepath.Join(tmp, "devkit")},
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := parsed.cfg.AllowedImages; len(got) != 2 || got[0] != "postgres:16" || got[1] != "redis:7" {
		t.Fatalf("allowed images = %#v", got)
	}
	if parsed.cfg.Socket != "/tmp/b.sock" {
		t.Fatalf("socket = %q", parsed.cfg.Socket)
	}
}
