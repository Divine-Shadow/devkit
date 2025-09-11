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

// This integration test requires an image with codex installed and callable non-interactively.
// Set DEVKIT_INTEGRATION=1 DEVKIT_IT_IMAGE=<image> to run.
func TestCodexTest_Integration(t *testing.T) {
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
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
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

	// Run codex-test to verify it returns ok
	cmd2 := exec.Command(bin, "-p", "test", "codex-test", "1")
	cmd2.Env = append(os.Environ(), "DEVKIT_ROOT="+root)
	if out, err := cmd2.CombinedOutput(); err != nil {
		t.Fatalf("codex-test failed: %v\n%s", err, out)
	}
}
