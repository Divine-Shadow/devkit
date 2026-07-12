package preflight

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/cmdregistry"
)

func TestPreflightTreatsDockerAsOptional(t *testing.T) {
	binDir := t.TempDir()
	writeStub(t, binDir, "nix", 0)
	writeStub(t, binDir, "bwrap", 0)
	writeStub(t, binDir, "tmux", 0)
	t.Setenv("PATH", binDir)

	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	if err := handle(&cmdregistry.Context{}); err != nil {
		t.Fatalf("preflight should not fail when docker is absent: %v", err)
	}
}

func TestPreflightRequiresNativeRuntimeTools(t *testing.T) {
	binDir := t.TempDir()
	writeStub(t, binDir, "docker", 0)
	writeStub(t, binDir, "tmux", 0)
	t.Setenv("PATH", binDir)
	t.Setenv("HOME", t.TempDir())

	err := handle(&cmdregistry.Context{})
	if err == nil || !strings.Contains(err.Error(), "preflight checks failed") {
		t.Fatalf("expected missing native tool failure, got %v", err)
	}
}

func writeStub(t *testing.T, dir string, name string, code int) {
	t.Helper()
	path := filepath.Join(dir, name)
	body := "#!/bin/sh\nexit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}
