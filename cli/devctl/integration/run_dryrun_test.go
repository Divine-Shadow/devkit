package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRun_DryRun ensures the legacy Product shortcut refuses before invoking
// another lifecycle or container authority, even in dry-run mode.
func TestRun_DryRun(t *testing.T) {
	root := t.TempDir()
	write := func(p, s string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// files edited by allowlist step
	write(filepath.Join(root, "kit/proxy/allowlist.txt"), "\n")
	write(filepath.Join(root, "kit/dns/dnsmasq.conf"), "\n")

	// Build binary
	bin := filepath.Join(t.TempDir(), "devctl")
	build := exec.Command("go", "build", "-trimpath", "-o", bin, "./")
	build.Env = append(os.Environ(), "GO111MODULE=on")
	build.Dir = filepath.Join("..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("go build failed: %v\n%s", err, out)
	}

	var stderr bytes.Buffer
	cmd := exec.Command(bin, "--dry-run", "--no-tmux", "--no-seed", "-p", "dev-all", "run", "testrepo", "2")
	cmd.Env = append(os.Environ(), "DEVKIT_ROOT="+root)
	cmd.Stderr = &stderr
	cmd.Stdout = &bytes.Buffer{}
	if err := cmd.Run(); err == nil {
		t.Fatalf("run dry-run unexpectedly succeeded\nstderr=%s", stderr.String())
	}
	out := stderr.String()
	if !strings.Contains(out, "raw devctl refuses Product") {
		t.Fatalf("run dry-run omitted Product dispatch refusal:\n%s", out)
	}
	if strings.Contains(out, " -p dev-all up ") ||
		strings.Contains(out, "docker "+"compose") ||
		strings.Contains(out, "docker "+"exec") {
		t.Fatalf("run dry-run started a child lifecycle:\n%s", out)
	}
}
