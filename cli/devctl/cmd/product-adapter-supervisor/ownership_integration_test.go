//go:build devkitintegration

package main

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestPinnedCodexRejectsSameUIDDecoyListener is the packaged sabotage gate for
// the exact acceptance predicate used by the supervisor. A real pinned Codex
// app-server remains alive while a different process with the same uid owns
// the declared listener. Filesystem ownership and process identity alone must
// not let the pinned process inherit the decoy's socket.
func TestPinnedCodexRejectsSameUIDDecoyListener(t *testing.T) {
	pinnedCodex := os.Getenv("DEVKIT_TEST_PINNED_CODEX")
	if pinnedCodex == "" {
		t.Fatal("DEVKIT_TEST_PINNED_CODEX is required by the packaged integration gate")
	}

	home := t.TempDir()
	codex := exec.Command(pinnedCodex, "app-server", "--stdio")
	codex.Env = []string{
		"HOME=" + home,
		"CODEX_HOME=" + filepath.Join(home, ".codex"),
	}
	codexStdin, err := codex.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer codexStdin.Close()
	codex.Stdout = io.Discard
	codex.Stderr = io.Discard
	if err := codex.Start(); err != nil {
		t.Fatal(err)
	}
	defer stopTestProcess(t, codex)

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := processStartTime(codex.Process.Pid); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pinned Codex app-server did not remain alive")
		}
		time.Sleep(10 * time.Millisecond)
	}

	path := filepath.Join(t.TempDir(), "same-uid-decoy.sock")
	decoy := exec.Command(os.Args[0], "-test.run=^TestUnixSocketOwnerHelper$")
	decoy.Env = append(
		os.Environ(),
		"DEVKIT_TEST_SOCKET_OWNER_HELPER=1",
		"DEVKIT_TEST_SOCKET_OWNER_PATH="+path,
	)
	decoyStdin, err := decoy.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	decoyStdout, err := decoy.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	decoy.Stderr = os.Stderr
	if err := decoy.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = decoyStdin.Close()
		stopTestProcess(t, decoy)
	}()
	ready := bufio.NewScanner(decoyStdout)
	if !ready.Scan() || ready.Text() != "ready" {
		t.Fatalf("same-uid decoy was not ready: %q %v", ready.Text(), ready.Err())
	}
	if codex.Process.Pid == decoy.Process.Pid {
		t.Fatal("test setup did not produce distinct processes")
	}

	codexStart, err := processStartTime(codex.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proveProcessOwnsListeningUnixSocket(
		codex.Process.Pid,
		codexStart,
		path,
		path,
	); err == nil {
		t.Fatal("pinned Codex inherited a same-uid decoy listener")
	}
}

func stopTestProcess(t *testing.T, command *exec.Cmd) {
	t.Helper()
	if command.Process == nil {
		return
	}
	_ = command.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = command.Process.Kill()
		<-done
	}
}
