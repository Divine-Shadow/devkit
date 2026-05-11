package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRun_DryRun ensures the run command produces expected native invocations
// and does not error when invoked in dry-run mode with a minimal dev-all overlay.
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
	base := "version: '3.8'\nservices:\n  dev-agent:\n    image: alpine:3.18\n    command: ['sh','-lc','sleep 1']\n"
	write(filepath.Join(root, "kit/compose.yml"), base)
	write(filepath.Join(root, "kit/compose.dns.yml"), base)
	write(filepath.Join(root, "overlays/dev-all/compose.override.yml"), base)
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

	t.Setenv("COMPOSE_PROJECT_NAME", "")

	var stderr bytes.Buffer
	cmd := exec.Command(bin, "--dry-run", "--no-tmux", "--no-seed", "-p", "dev-all", "run", "testrepo", "2")
	cmd.Env = append(os.Environ(), "DEVKIT_ROOT="+root)
	cmd.Stderr = &stderr
	cmd.Stdout = &bytes.Buffer{}
	if err := cmd.Run(); err != nil {
		t.Fatalf("run dry-run failed: %v\nstderr=%s", err, stderr.String())
	}
	out := stderr.String()
	// Expect native lifecycle and tmux commands, not Compose-backed agent execs.
	wants := []string{
		" -p dev-all up --repo testrepo --count 2",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Fatalf("missing %q in:\n%s", w, out)
		}
	}
	if strings.Contains(out, "docker compose") || strings.Contains(out, "docker exec") {
		t.Fatalf("run dry-run should not use Docker/Compose for dev-all:\n%s", out)
	}
}
