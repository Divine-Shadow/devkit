package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func buildDevctlForNativeDefaults(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "devctl")
	cmd := exec.Command("go", "build", "-trimpath", "-o", bin, "./")
	cmd.Dir = filepath.Join("..")
	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("go build failed: %v\n%s", err, out)
	}
	return bin
}

func nativeDefaultsRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	baseCompose := "version: '3.8'\nservices:\n  dev-agent:\n    image: alpine:3.18\n"
	write(filepath.Join(root, "kit/compose.yml"), baseCompose)
	write(filepath.Join(root, "kit/compose.dns.yml"), baseCompose)
	write(filepath.Join(root, "overlays/dev-all/compose.override.yml"), baseCompose)
	write(filepath.Join(root, "overlays/dev-all/devkit.yaml"), `
defaults:
  repo: ouroboros-ide
  agents: 2
native:
  worktree_root: ../agent-worktrees
  state_root: ../.devkit/native-agents
  worktree_container_root: /worktrees
  state_container_root: /agent-state
hooks:
  warm: echo warm-native
`)
	return root
}

func legacyComposeRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	baseCompose := "version: '3.8'\nservices:\n  dev-agent:\n    image: alpine:3.18\n"
	write(filepath.Join(root, "kit/compose.yml"), baseCompose)
	write(filepath.Join(root, "kit/compose.dns.yml"), baseCompose)
	write(filepath.Join(root, "overlays/codex/compose.override.yml"), baseCompose)
	write(filepath.Join(root, "overlays/codex/devkit.yaml"), "service: dev-agent\n")
	return root
}

func createNativeAgentWorktree(t *testing.T, root string) {
	t.Helper()
	devRoot := filepath.Clean(filepath.Join(root, ".."))
	worktree := filepath.Join(devRoot, "agent-worktrees", "agent1", "ouroboros-ide")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
}

func fakeBwrapDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "bwrap")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runNativeDefaultDryRun(t *testing.T, bin, root string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"--dry-run", "-p", "dev-all"}, args...)...)
	cmd.Env = append(os.Environ(), "DEVKIT_ROOT="+root, "DEVKIT_NO_TMUX=1")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func runProjectDryRun(t *testing.T, bin, root, project string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"--dry-run", "-p", project}, args...)...)
	cmd.Env = append(os.Environ(),
		"DEVKIT_ROOT="+root,
		"DEVKIT_NO_TMUX=1",
		"DEVKIT_GIT_USER_NAME=Devkit Test",
		"DEVKIT_GIT_USER_EMAIL=devkit-test@example.com",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func assertNoDockerCommand(t *testing.T, output string) {
	t.Helper()
	for _, forbidden := range []string{"+ docker compose", "+ docker exec", "docker exec -it"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("native dev-all dry-run emitted %q:\n%s", forbidden, output)
		}
	}
}

func TestNonDevAllTopLevelAliasesUseLegacyComposeDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := legacyComposeRoot(t)

	for _, args := range [][]string{{"up"}, {"status"}, {"logs", "--tail", "1"}, {"down"}, {"scale", "2"}} {
		out, err := runProjectDryRun(t, bin, root, "codex", args...)
		if err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
		if strings.Contains(out, "native lifecycle currently supports") || strings.Contains(out, "bwrap") {
			t.Fatalf("%v unexpectedly used native path:\n%s", args, out)
		}
		if !strings.Contains(out, "+ docker compose") {
			t.Fatalf("%v did not use legacy Compose:\n%s", args, out)
		}
	}
}

func TestNonDevAllTopLevelExecUsesLegacyComposeDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := legacyComposeRoot(t)

	out, err := runProjectDryRun(t, bin, root, "codex", "exec", "1", "echo", "hi")
	if err != nil {
		t.Fatalf("legacy exec failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "native lifecycle currently supports") || strings.Contains(out, "bwrap") {
		t.Fatalf("legacy exec unexpectedly used native path:\n%s", out)
	}
	if !strings.Contains(out, "+ docker compose") {
		t.Fatalf("legacy exec did not use Compose:\n%s", out)
	}
}

func TestDevAllComposeNamespaceIsRetiredDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := nativeDefaultsRoot(t)

	out, err := runNativeDefaultDryRun(t, bin, root, "compose", "up")
	if err == nil {
		t.Fatalf("dev-all compose unexpectedly succeeded:\n%s", out)
	}
	assertNoDockerCommand(t, out)
	if !strings.Contains(out, "Docker Compose is retired for dev-all") {
		t.Fatalf("unexpected dev-all compose error:\n%s", out)
	}
}

func TestNativeTopLevelExecAndAttachPreserveSandboxExitCode(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	bwrapDir := fakeBwrapDir(t)

	tests := []struct {
		name string
		args []string
	}{
		{name: "exec", args: []string{"-p", "dev-all", "exec", "1", "--repo", "ouroboros-ide", "--flake", ".#runtime-test-agent", "--", "true"}},
		{name: "attach", args: []string{"-p", "dev-all", "attach", "1", "--repo", "ouroboros-ide", "--flake", ".#runtime-test-agent"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := nativeDefaultsRoot(t)
			createNativeAgentWorktree(t, root)
			cmd := exec.Command(bin, tt.args...)
			cmd.Env = append(os.Environ(),
				"DEVKIT_ROOT="+root,
				"DEVKIT_NO_TMUX=1",
				"CODEX_AUTH_JSON="+filepath.Join(root, "missing-auth.json"),
				"PATH="+bwrapDir+string(os.PathListSeparator)+os.Getenv("PATH"),
			)
			out, err := cmd.CombinedOutput()
			exitErr, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("%s returned %T, want exit status 7\n%s", tt.name, err, out)
			}
			if code := exitErr.ExitCode(); code != 7 {
				t.Fatalf("%s exit code = %d, want 7\n%s", tt.name, code, out)
			}
		})
	}
}

func TestGlobalDryRunNativeExecDoesNotPrepareOrRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := nativeDefaultsRoot(t)

	cmd := exec.Command(bin, "--dry-run", "-p", "dev-all", "native", "exec", "--repo", "ouroboros-ide", "--flake", ".#runtime-test-agent", "--", "echo", "hi")
	cmd.Env = append(os.Environ(), "DEVKIT_ROOT="+root, "DEVKIT_NO_TMUX=1")
	outBytes, err := cmd.CombinedOutput()
	out := string(outBytes)
	if err != nil {
		t.Fatalf("global dry-run native exec failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "bwrap") || !strings.Contains(out, "exec") || !strings.Contains(out, "echo") || !strings.Contains(out, "hi") {
		t.Fatalf("global dry-run did not print native command:\n%s", out)
	}
}

func TestDevAllFreshOpenAndResetUseNativeLifecycleDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := nativeDefaultsRoot(t)

	for _, args := range [][]string{{"fresh-open", "2"}, {"reset", "2"}} {
		out, err := runNativeDefaultDryRun(t, bin, root, args...)
		if err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
		assertNoDockerCommand(t, out)
		if !strings.Contains(out, " -p dev-all down --repo ouroboros-ide --count 2") {
			t.Fatalf("%v missing native down:\n%s", args, out)
		}
		if !strings.Contains(out, " -p dev-all up --repo ouroboros-ide --count 2") {
			t.Fatalf("%v missing native up:\n%s", args, out)
		}
	}
}

func TestDevAllCheckAndHookHelpersUseNativeExecDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := nativeDefaultsRoot(t)

	for _, args := range [][]string{{"check-net"}, {"check-codex"}, {"check-sts", "tinyproxy"}, {"warm"}} {
		out, err := runNativeDefaultDryRun(t, bin, root, args...)
		if err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
		assertNoDockerCommand(t, out)
		if !strings.Contains(out, " -p dev-all exec 1 --repo ouroboros-ide -- bash -lc") {
			t.Fatalf("%v missing native exec:\n%s", args, out)
		}
	}
}

func TestDevAllMixedLayoutRefusesComposeFallbackDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := nativeDefaultsRoot(t)
	layout := filepath.Join(root, "mixed-layout.yaml")
	if err := os.WriteFile(layout, []byte(`
session: mixed
windows:
  - index: 1
    project: front
    service: dev-agent
    path: /workspace
`), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runNativeDefaultDryRun(t, bin, root, "layout-apply", "--file", layout)
	if err == nil {
		t.Fatalf("mixed dev-all layout unexpectedly succeeded:\n%s", out)
	}
	assertNoDockerCommand(t, out)
	if !strings.Contains(out, "only supports native dev-all layouts") {
		t.Fatalf("unexpected mixed-layout failure:\n%s", out)
	}
}

func TestLegacyLayoutCannotTargetDevAllDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := legacyComposeRoot(t)
	layout := filepath.Join(root, "legacy-devall-layout.yaml")
	if err := os.WriteFile(layout, []byte(`
session: legacy-devall
windows:
  - index: 1
    project: dev-all
    service: dev-agent
    path: /workspaces/dev/ouroboros-ide
`), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runProjectDryRun(t, bin, root, "codex", "layout-apply", "--file", layout)
	if err == nil {
		t.Fatalf("legacy layout targeting dev-all unexpectedly succeeded:\n%s", out)
	}
	assertNoDockerCommand(t, out)
	if !strings.Contains(out, "legacy layout-apply cannot target dev-all") {
		t.Fatalf("unexpected legacy layout failure:\n%s", out)
	}
}
