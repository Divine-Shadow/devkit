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

// Verify integration for a custom overlay named 'test'.
// Requires DEVKIT_IT_IMAGE with git+codex installed; this avoids dependency on the 'codex' overlay.
func TestVerify_Integration(t *testing.T) {
    if os.Getenv("DEVKIT_INTEGRATION") != "1" { t.Skip("integration disabled") }
    image := os.Getenv("DEVKIT_IT_IMAGE")
    if image == "" { t.Skip("DEVKIT_IT_IMAGE not set") }
    if _, err := exec.LookPath("docker"); err != nil { t.Skip("docker not available") }

    root := t.TempDir()
    write := func(p, s string) { os.MkdirAll(filepath.Dir(p), 0o755); os.WriteFile(p, []byte(s), 0o644) }
    base := "version: '3.8'\nservices:\n  dev-agent:\n    image: " + image + "\n    command: ['sh','-lc','sleep infinity']\n"
    write(filepath.Join(root, "kit/compose.yml"), base)
    write(filepath.Join(root, "kit/compose.dns.yml"), base)
    write(filepath.Join(root, "overlays/test/compose.override.yml"), base)
    write(filepath.Join(root, "kit/proxy/allowlist.txt"), "\n")
    write(filepath.Join(root, "kit/dns/dnsmasq.conf"), "\n")

    bin := filepath.Join(t.TempDir(), "devctl")
    build := exec.Command("go", "build", "-trimpath", "-o", bin, "./")
    build.Env = append(os.Environ(), "GO111MODULE=on")
    build.Dir = filepath.Join("..")
    if out, err := build.CombinedOutput(); err != nil { t.Fatalf("go build failed: %v\n%s", err, out) }

    var stderr bytes.Buffer
    cmd := exec.Command(bin, "-p", "test", "verify")
    cmd.Env = append(os.Environ(), "DEVKIT_ROOT="+root)
    cmd.Stderr = &stderr
    if out, err := cmd.Output(); err != nil { t.Fatalf("verify failed: %v\n%s\n%s", err, out, stderr.String()) }

    _ = exec.Command("docker", "compose", "-f", filepath.Join(root, "kit/compose.yml"), "-f", filepath.Join(root, "kit/compose.dns.yml"), "-f", filepath.Join(root, "overlays/test/compose.override.yml"), "down").Run()
}

