package broker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeDefaults(t *testing.T) {
	cfg := Normalize(Config{DevkitRoot: "/home/user/dev/devkit"})
	if cfg.Socket != DefaultSocket {
		t.Fatalf("socket = %q", cfg.Socket)
	}
	if cfg.Upstream != DefaultUpstream {
		t.Fatalf("upstream = %q", cfg.Upstream)
	}
	if got, want := cfg.StateRoot, "/home/user/dev/.devkit/native-broker"; got != want {
		t.Fatalf("state root = %q, want %q", got, want)
	}
	if len(cfg.AllowedImages) != 1 || cfg.AllowedImages[0] != "postgres:latest" {
		t.Fatalf("allowed images = %#v", cfg.AllowedImages)
	}
}

func TestEnvIncludesExactPolicy(t *testing.T) {
	cfg := Config{
		Socket:        "/tmp/devkit.sock",
		Upstream:      "unix:///tmp/docker.sock",
		AllowedImages: []string{"postgres:15", "minio/minio:latest"},
		AllowPulls:    true,
		LogLevel:      "debug",
	}
	env := strings.Join(Env(cfg), "\n")
	for _, want := range []string{
		"BROKER_LISTEN=unix:///tmp/devkit.sock",
		"BROKER_UPSTREAM=unix:///tmp/docker.sock",
		"BROKER_ALLOWED_IMAGES=postgres:15,minio/minio:latest",
		"BROKER_ALLOW_PULLS=true",
		"BROKER_LOG_LEVEL=debug",
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("env missing %q in:\n%s", want, env)
		}
	}
}

func TestInspectReportsStalePID(t *testing.T) {
	tmp := t.TempDir()
	cfg := Config{StateRoot: tmp, Socket: filepath.Join(tmp, "broker.sock")}
	if err := os.WriteFile(PIDFile(cfg), []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("write pid: %v", err)
	}
	status, err := Inspect(cfg)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if status.Running {
		t.Fatalf("expected not running")
	}
	if !status.StaleState {
		t.Fatalf("expected stale state: %#v", status)
	}
}

func TestStopRefusesUnmanagedProcess(t *testing.T) {
	tmp := t.TempDir()
	cfg := Config{StateRoot: tmp, Socket: filepath.Join(tmp, "broker.sock")}
	state := State{PID: os.Getpid(), Binary: "/not/postgres-broker", Socket: cfg.Socket}
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := writeState(cfg, state); err != nil {
		t.Fatalf("write state: %v", err)
	}
	_, err := Stop(cfg, false)
	if err == nil || !strings.Contains(err.Error(), "refusing to stop unmanaged process") {
		t.Fatalf("expected unmanaged process error, got %v", err)
	}
}
