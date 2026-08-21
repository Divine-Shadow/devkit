package broker

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const brokerTestHelperMode = "DEVKIT_BROKER_TEST_HELPER"

func TestMain(m *testing.M) {
	switch os.Getenv(brokerTestHelperMode) {
	case "sleep":
		for {
			time.Sleep(time.Hour)
		}
	case "listen":
		path := strings.TrimPrefix(os.Getenv("BROKER_LISTEN"), "unix://")
		listener, err := net.Listen("unix", path)
		if err != nil {
			os.Exit(2)
		}
		defer listener.Close()
		for {
			conn, err := listener.Accept()
			if err != nil {
				os.Exit(0)
			}
			_ = conn.Close()
		}
	}
	os.Exit(m.Run())
}

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

func TestResolveBinaryRequiresImmutableAbsoluteExecutable(t *testing.T) {
	tmp := t.TempDir()
	brokerBinary := filepath.Join(tmp, "postgres-broker")
	if err := os.WriteFile(brokerBinary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write broker fixture: %v", err)
	}

	binary, err := ResolveBinary(context.Background(), Config{
		DevkitRoot: filepath.Join(tmp, "empty-controller-root", "devkit"),
		Binary:     brokerBinary,
	})
	if err != nil {
		t.Fatalf("ResolveBinary: %v", err)
	}
	if got, want := binary, brokerBinary; got != want {
		t.Fatalf("binary = %q, want %q", got, want)
	}

	if _, err := ResolveBinary(context.Background(), Config{
		DevkitRoot: filepath.Join(tmp, "devkit"),
	}); err == nil || !strings.Contains(err.Error(), "immutable postgres-broker binary is required") {
		t.Fatalf("missing immutable broker authority was not rejected: %v", err)
	}

	if _, err := ResolveBinary(context.Background(), Config{
		Binary: "relative/postgres-broker",
	}); err == nil || !strings.Contains(err.Error(), "must be an absolute path") {
		t.Fatalf("relative broker selector was not rejected: %v", err)
	}

	nonExecutable := filepath.Join(tmp, "not-executable")
	if err := os.WriteFile(nonExecutable, []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveBinary(context.Background(), Config{
		Binary: nonExecutable,
	}); err == nil || !strings.Contains(err.Error(), "is not executable") {
		t.Fatalf("non-executable broker selector was not rejected: %v", err)
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

func TestInspectRejectsReusedLivePID(t *testing.T) {
	tmp := t.TempDir()
	cfg := Config{StateRoot: tmp, Socket: filepath.Join(tmp, "broker.sock")}
	state := State{PID: os.Getpid(), Binary: "/not/postgres-broker", Socket: cfg.Socket}
	if err := writeState(cfg, state); err != nil {
		t.Fatalf("write state: %v", err)
	}

	status, err := Inspect(cfg)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if status.Running {
		t.Fatalf("reused pid was reported as a running broker: %#v", status)
	}
	if !status.StaleState || !strings.Contains(status.Message, "different process") {
		t.Fatalf("reused pid was not classified as stale identity: %#v", status)
	}
}

func TestStartIgnoresReusedPIDAndStartsBroker(t *testing.T) {
	tmp := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	cfg := Config{
		StateRoot:    tmp,
		Socket:       filepath.Join(tmp, "broker.sock"),
		Binary:       executable,
		StartTimeout: 2 * time.Second,
	}
	if err := writeState(cfg, State{PID: os.Getpid(), Binary: "/not/postgres-broker", Socket: cfg.Socket}); err != nil {
		t.Fatalf("write state: %v", err)
	}
	t.Setenv(brokerTestHelperMode, "listen")

	status, err := Start(context.Background(), cfg, false)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !status.Running || !status.SocketExists {
		t.Fatalf("broker was not started after pid reuse: %#v", status)
	}
	if status.PID == os.Getpid() {
		t.Fatalf("reused pid %d remained broker authority", status.PID)
	}
	if _, err := Stop(cfg, false); err != nil {
		t.Fatalf("stop replacement broker: %v", err)
	}
}

func TestStartDoesNotSignalSameBinaryForDifferentBrokerSocket(t *testing.T) {
	tmp := t.TempDir()
	foreignSocket := filepath.Join(tmp, "foreign.sock")
	foreign := startBrokerTestHelper(t, "listen", "BROKER_LISTEN=unix://"+foreignSocket)
	waitForBrokerTestSocket(t, foreignSocket)
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	cfg := Config{
		StateRoot:    filepath.Join(tmp, "state"),
		Socket:       filepath.Join(tmp, "broker.sock"),
		Binary:       executable,
		StartTimeout: 2 * time.Second,
	}
	if err := os.MkdirAll(cfg.StateRoot, 0o700); err != nil {
		t.Fatalf("mkdir state root: %v", err)
	}
	if err := writeState(cfg, State{PID: foreign.Process.Pid, Binary: executable, Socket: cfg.Socket}); err != nil {
		t.Fatalf("write state: %v", err)
	}
	t.Setenv(brokerTestHelperMode, "listen")

	status, err := Start(context.Background(), cfg, false)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !status.Running || !status.SocketExists {
		t.Fatalf("replacement broker is not ready: %#v", status)
	}
	if !processRunning(foreign.Process.Pid) || !socketAcceptsConnections(foreignSocket) {
		t.Fatalf("same-binary foreign broker pid %d was disturbed", foreign.Process.Pid)
	}
	if _, err := Stop(cfg, false); err != nil {
		t.Fatalf("stop replacement broker: %v", err)
	}
}

func TestInspectRejectsManagedProcessWithoutLiveSocket(t *testing.T) {
	tmp := t.TempDir()
	cfg := Config{StateRoot: tmp, Socket: filepath.Join(tmp, "broker.sock")}
	process := startBrokerTestHelper(t, "sleep", "BROKER_LISTEN=unix://"+cfg.Socket)
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	if err := writeState(cfg, State{PID: process.Process.Pid, Binary: executable, Socket: cfg.Socket}); err != nil {
		t.Fatalf("write state: %v", err)
	}

	status, err := Inspect(cfg)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if status.Running {
		t.Fatalf("managed process without a live socket was reported running: %#v", status)
	}
	if !status.StaleState || !strings.Contains(status.Message, "not accepting connections") {
		t.Fatalf("dead socket was not classified as stale: %#v", status)
	}
}

func TestStartReplacesManagedProcessWithDeadSocket(t *testing.T) {
	tmp := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	cfg := Config{
		StateRoot:    tmp,
		Socket:       filepath.Join(tmp, "broker.sock"),
		Binary:       executable,
		StartTimeout: 2 * time.Second,
	}
	stale := startBrokerTestHelper(t, "sleep", "BROKER_LISTEN=unix://"+cfg.Socket)
	if err := writeState(cfg, State{PID: stale.Process.Pid, Binary: executable, Socket: cfg.Socket}); err != nil {
		t.Fatalf("write state: %v", err)
	}
	t.Setenv(brokerTestHelperMode, "listen")

	status, err := Start(context.Background(), cfg, false)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !status.Running || !status.SocketExists {
		t.Fatalf("replacement broker is not ready: %#v", status)
	}
	if status.PID == stale.Process.Pid {
		t.Fatalf("stale broker pid %d was reused", status.PID)
	}
	if processRunning(stale.Process.Pid) {
		t.Fatalf("stale managed broker pid %d remains alive", stale.Process.Pid)
	}
	if _, err := Stop(cfg, false); err != nil {
		t.Fatalf("stop replacement broker: %v", err)
	}
}

func startBrokerTestHelper(t *testing.T, mode string, extraEnv ...string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	cmd := exec.Command(executable)
	cmd.Env = append(os.Environ(), brokerTestHelperMode+"="+mode)
	cmd.Env = append(cmd.Env, extraEnv...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start broker test helper: %v", err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		if processRunning(cmd.Process.Pid) {
			_ = cmd.Process.Kill()
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	})
	return cmd
}

func waitForBrokerTestSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !socketAcceptsConnections(path) {
		if time.Now().After(deadline) {
			t.Fatalf("broker test socket %s did not become ready", path)
		}
		time.Sleep(10 * time.Millisecond)
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
