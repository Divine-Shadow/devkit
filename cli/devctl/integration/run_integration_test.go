//go:build integration
// +build integration

package main

import (
    "bytes"
    "os"
    "os/exec"
    "path/filepath"
    "testing"
)

// This integration test drives `devctl run` end-to-end with a provided image.
// Requirements:
// - DEVKIT_INTEGRATION=1
// - DEVKIT_IT_IMAGE pointing to an image with git + codex installed
// - Docker available
func TestRun_Integration(t *testing.T) {
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
    base := "version: '3.8'\nservices:\n  dev-agent:\n    image: " + image + "\n    command: ['sh','-lc','sleep infinity']\n"
    mustWrite(filepath.Join(root, "kit/compose.yml"), base)
    mustWrite(filepath.Join(root, "kit/compose.dns.yml"), base)
    mustWrite(filepath.Join(root, "kit/compose.envoy.yml"), base)
    mustWrite(filepath.Join(root, "overlays/test/compose.override.yml"), base)
    // files edited by allowlist step
    mustWrite(filepath.Join(root, "kit/proxy/allowlist.txt"), "\n")
    mustWrite(filepath.Join(root, "kit/dns/dnsmasq.conf"), "\n")

    // Build binary
    bin := filepath.Join(t.TempDir(), "devctl")
    build := exec.Command("go", "build", "-trimpath", "-o", bin, "./")
    build.Env = append(os.Environ(), "GO111MODULE=on")
    build.Dir = filepath.Join("..")
    if out, err := build.CombinedOutput(); err != nil {
        t.Fatalf("go build failed: %v\n%s", err, out)
    }

    // Run devctl run with tmux disabled
    var stderr bytes.Buffer
    cmd := exec.Command(bin, "-p", "test", "--no-tmux", "run", "myrepo", "2")
    cmd.Env = append(os.Environ(), "DEVKIT_ROOT="+root)
    cmd.Stderr = &stderr
    if out, err := cmd.Output(); err != nil {
        t.Fatalf("devctl run failed: %v\n%s\n%s", err, out, stderr.String())
    }

    // Now run codex-test for agent 1 and 2
    c1 := exec.Command(bin, "-p", "test", "codex-test", "1", "myrepo")
    c1.Env = append(os.Environ(), "DEVKIT_ROOT="+root)
    if out, err := c1.CombinedOutput(); err != nil {
        t.Fatalf("codex-test agent1 failed: %v\n%s", err, out)
    }
    c2 := exec.Command(bin, "-p", "test", "codex-test", "2", "myrepo")
    c2.Env = append(os.Environ(), "DEVKIT_ROOT="+root)
    if out, err := c2.CombinedOutput(); err != nil {
        t.Fatalf("codex-test agent2 failed: %v\n%s", err, out)
    }

    // Teardown compose
    _ = exec.Command("docker", "compose", "-f", filepath.Join(root, "kit/compose.yml"), "-f", filepath.Join(root, "kit/compose.dns.yml"), "-f", filepath.Join(root, "overlays/test/compose.override.yml"), "down").Run()
}

