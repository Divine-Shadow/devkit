package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestFreshOpen_DryRun ensures the fresh-open command assembles expected docker/tmux commands.
func TestFreshOpen_DryRun(t *testing.T) {
	// Prepare a temp devkit root with minimal compose files
	root := t.TempDir()
	mustWrite := func(p, s string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// minimal compose/service layout
	base := `version: "3.8"
services:
  dev-agent:
    image: alpine:3.18
    command: ["sh","-lc","sleep 1"]
`
	mustWrite(filepath.Join(root, "kit/compose.yml"), base)
	mustWrite(filepath.Join(root, "kit/compose.hardened.yml"), base)
	mustWrite(filepath.Join(root, "kit/compose.dns.yml"), base)
	mustWrite(filepath.Join(root, "kit/compose.envoy.yml"), base)
	mustWrite(filepath.Join(root, "overlays/test/compose.override.yml"), base)

	// Build binary
	bin := filepath.Join(t.TempDir(), "devctl")
	cmdBuild := exec.Command("go", "build", "-trimpath", "-o", bin, "./")
	cmdBuild.Dir = filepath.Join("..")
	cmdBuild.Env = append(os.Environ(), "GO111MODULE=on")
	if out, err := cmdBuild.CombinedOutput(); err != nil {
		t.Skipf("go build not available or failed: %v\n%s", err, out)
		return
	}

	// Run fresh-open with dry-run and tmux disabled
	var stderr bytes.Buffer
	cmd := exec.Command(bin, "--dry-run", "-p", "test", "fresh-open", "2")
	cmd.Env = append(os.Environ(), "DEVKIT_ROOT="+root, "DEVKIT_NO_TMUX=1", "DEVKIT_DEBUG=1")
	cmd.Stderr = &stderr
	cmd.Stdout = &bytes.Buffer{}
	if err := cmd.Run(); err != nil {
		t.Fatalf("devctl failed: %v\nstderr=%s", err, stderr.String())
	}
	out := stderr.String()
	// Validate key commands appear
	expects := []string{
		"+ docker compose -f ",
		"compose.yml",
		"compose.hardened.yml",
		"compose.dns.yml",
		"compose.envoy.yml",
		" up -d --scale dev-agent=2",
	}
	for _, e := range expects {
		if !strings.Contains(out, e) {
			t.Fatalf("output missing %q\n%s", e, out)
		}
	}
}
