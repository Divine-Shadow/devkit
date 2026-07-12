package broker

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
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

func TestEnvIncludesSandboxBrokerSocketAlias(t *testing.T) {
	cfg := Config{
		DevkitRoot: "/home/bayesartre/dev/devkit",
		Socket:     "/home/bayesartre/dev/.devkit/native-broker/broker.sock",
	}
	env := strings.Join(Env(cfg), "\n")
	want := "BROKER_SOCKET_BIND_ALIASES=/workspaces/dev/.devkit/native-broker/broker.sock"
	if !strings.Contains(env, want) {
		t.Fatalf("env missing %q in:\n%s", want, env)
	}
}

func TestNormalizePreservesExplicitSocketBindAliases(t *testing.T) {
	cfg := Normalize(Config{
		DevkitRoot:        "/home/bayesartre/dev/devkit",
		Socket:            "/home/bayesartre/dev/.devkit/native-broker/broker.sock",
		SocketBindAliases: []string{"/custom/broker.sock", "/custom/broker.sock"},
	})
	got := strings.Join(cfg.SocketBindAliases, ",")
	want := "/custom/broker.sock,/workspaces/dev/.devkit/native-broker/broker.sock"
	if got != want {
		t.Fatalf("socket bind aliases = %q, want %q", got, want)
	}
}

func TestResolveBinaryUsesStateLocalNixCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/read-only-host-cache")
	tmp := t.TempDir()
	devkitRoot := filepath.Join(tmp, "devkit")
	stateRoot := filepath.Join(tmp, "state")
	envFile := filepath.Join(tmp, "xdg-cache-home")
	if err := os.MkdirAll(devkitRoot, 0o755); err != nil {
		t.Fatalf("mkdir devkit root: %v", err)
	}
	nix := filepath.Join(tmp, "nix")
	script := "#!/usr/bin/env bash\nprintf '%s' \"${XDG_CACHE_HOME:-}\" > " + strconv.Quote(envFile) + "\nprintf '/nix/store/fake-postgres-broker\\n'\n"
	if err := os.WriteFile(nix, []byte(script), 0o755); err != nil {
		t.Fatalf("write nix stub: %v", err)
	}

	binary, err := ResolveBinary(context.Background(), Config{
		DevkitRoot: devkitRoot,
		StateRoot:  stateRoot,
		Nix:        nix,
	})
	if err != nil {
		t.Fatalf("ResolveBinary: %v", err)
	}
	if got, want := binary, "/nix/store/fake-postgres-broker/bin/postgres-broker"; got != want {
		t.Fatalf("binary = %q, want %q", got, want)
	}
	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if got, want := string(data), filepath.Join(stateRoot, "cache"); got != want {
		t.Fatalf("XDG_CACHE_HOME = %q, want %q", got, want)
	}
	if info, err := os.Stat(filepath.Join(stateRoot, "cache")); err != nil || !info.IsDir() {
		t.Fatalf("cache dir was not created: info=%v err=%v", info, err)
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
