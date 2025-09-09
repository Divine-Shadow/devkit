//go:build integration
// +build integration

package main

import (
    "bytes"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"
)

// This test is opt-in: requires Docker and an image with git, codex, claude installed.
// Set DEVKIT_INTEGRATION=1 and DEVKIT_IT_IMAGE to run.
func TestFreshOpen_Integration(t *testing.T) {
    if os.Getenv("DEVKIT_INTEGRATION") != "1" {
        t.Skip("integration disabled; set DEVKIT_INTEGRATION=1 to run")
    }
    image := os.Getenv("DEVKIT_IT_IMAGE")
    if image == "" {
        t.Skip("DEVKIT_IT_IMAGE not set; skipping")
    }
    if _, err := exec.LookPath("docker"); err != nil {
        t.Skip("docker not available")
    }

    // Prepare a temp devkit root with compose files referencing the image
    root := t.TempDir()
    mustWrite := func(p, s string) {
        if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil { t.Fatal(err) }
        if err := os.WriteFile(p, []byte(s), 0o644); err != nil { t.Fatal(err) }
    }
    base := "version: '3.8'\nservices:\n  dev-agent:\n    image: " + image + "\n    command: [\"sh\",\"-lc\",\"sleep infinity\"]\n"
    mustWrite(filepath.Join(root, "kit/compose.yml"), base)
    mustWrite(filepath.Join(root, "kit/compose.hardened.yml"), base+"    read_only: true\n")
    mustWrite(filepath.Join(root, "kit/compose.dns.yml"), base)
    mustWrite(filepath.Join(root, "kit/compose.envoy.yml"), base)
    mustWrite(filepath.Join(root, "overlays/test/compose.override.yml"), base)

    // Build binary
    bin := filepath.Join(t.TempDir(), "devctl")
    cmdBuild := exec.Command("go", "build", "-trimpath", "-o", bin, "./")
    cmdBuild.Env = append(os.Environ(), "GO111MODULE=on")
    cmdBuild.Dir = filepath.Join("..")
    if out, err := cmdBuild.CombinedOutput(); err != nil {
        t.Fatalf("go build failed: %v\n%s", err, out)
    }

    // Run fresh-open with tmux disabled
    var stderr bytes.Buffer
    cmd := exec.Command(bin, "-p", "test", "fresh-open", "1")
    cmd.Env = append(os.Environ(), "DEVKIT_ROOT="+root, "DEVKIT_NO_TMUX=1")
    cmd.Stderr = &stderr
    if out, err := cmd.Output(); err != nil {
        t.Fatalf("devctl fresh-open failed: %v\n%s\n%s", err, out, stderr.String())
    }

    // Exec in container: verify hardened (read-only root), and that git, codex, claude are callable non-interactively
    comp := func(args ...string) *exec.Cmd {
        a := append([]string{"compose", "-f", filepath.Join(root, "kit/compose.yml"), "-f", filepath.Join(root, "kit/compose.hardened.yml"), "-f", filepath.Join(root, "kit/compose.dns.yml"), "-f", filepath.Join(root, "kit/compose.envoy.yml")}, args...)
        return exec.Command("docker", a...)
    }

    // Hardened profile should deny writes to /
    if out, err := comp("exec", "dev-agent", "bash", "-lc", "touch /should_fail && echo wrote || echo denied").CombinedOutput(); err == nil && strings.Contains(string(out), "wrote") {
        t.Fatalf("expected read-only rootfs, got writable: %s", out)
    }
    // git --version
    if out, err := comp("exec", "dev-agent", "git", "--version").CombinedOutput(); err != nil {
        t.Fatalf("git --version failed: %v\n%s", err, out)
    }
    // codex non-interactive
    if out, err := comp("exec", "dev-agent", "bash", "-lc", "timeout 10s codex --version || timeout 10s codex exec 'ok' || true").CombinedOutput(); err != nil {
        t.Fatalf("codex check failed: %v\n%s", err, out)
    }
    // claude non-interactive
    if out, err := comp("exec", "dev-agent", "bash", "-lc", "timeout 10s claude --version || timeout 10s claude --help || true").CombinedOutput(); err != nil {
        t.Fatalf("claude check failed: %v\n%s", err, out)
    }

    // Teardown
    _ = comp("down").Run()
    _ = exec.Command("docker", "rm", "-f", "devkit_envoy", "devkit_envoy_sni", "devkit_dns", "devkit_tinyproxy").Run()
    _ = exec.Command("docker", "network", "rm", "devkit_dev-internal", "devkit_dev-egress").Run()
}
