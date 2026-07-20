package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/config"
	"devkit/cli/devctl/internal/sshauthority"
)

// TestResolveServiceOverlay ensures overlays can declare a default service name.
func TestResolveServiceOverlay(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// repo root: this file lives under devkit/cli/devctl
	root := filepath.Clean(filepath.Join(cwd, "..", ".."))
	overlays := filepath.Join(root, "overlays")

	paths := []string{overlays}
	if got := resolveService("ouroboros-static-front-end", paths); got != "frontend" {
		t.Fatalf("resolveService(front-end) = %q, want %q", got, "frontend")
	}
	if got := resolveService("codex", paths); got != "dev-agent" {
		t.Fatalf("resolveService(codex) = %q, want %q", got, "dev-agent")
	}
}

func TestApplyOverlayEnvLoadsEnvFiles(t *testing.T) {
	dir := t.TempDir()
	overlayDir := filepath.Join(dir, "overlays", "proj")
	if err := os.MkdirAll(overlayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(overlayDir, "extra.env")
	if err := os.WriteFile(envPath, []byte("FOO=123\nBAR=456\n# comment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FOO", "keep")
	t.Setenv("BAZ", "existing")
	t.Setenv("WORKSPACE_DIR", "")
	cfg := config.OverlayConfig{
		Workspace: "repo",
		Env:       map[string]string{"BAZ": "updated", "QUX": "789"},
		EnvFiles:  []string{"extra.env"},
	}
	applyOverlayEnv(cfg, overlayDir, dir)
	if got := os.Getenv("WORKSPACE_DIR"); got != filepath.Join(overlayDir, "repo") {
		t.Fatalf("workspace_dir=%q", got)
	}
	if got := os.Getenv("FOO"); got != "keep" {
		t.Fatalf("env file should not override existing FOO, got %q", got)
	}
	if got := os.Getenv("BAR"); got != "456" {
		t.Fatalf("BAR not loaded from env file: %q", got)
	}
	if got := os.Getenv("QUX"); got != "789" {
		t.Fatalf("overlay env map not applied: %q", got)
	}
	if got := os.Getenv("BAZ"); got != "existing" {
		t.Fatalf("env map should not override existing BAZ, got %q", got)
	}
}

func TestGitIdentityForRepoCommandUsesIdentityValues(t *testing.T) {
	home := "/workspaces/dev/agent-worktrees/agent2/.devhome-agent2"
	repoPath := "/workspaces/dev/agent-worktrees/agent2/ouroboros-ide"
	executable := filepath.Join(t.TempDir(), "package-ssh")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	authority, err := sshauthority.New(executable)
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := gitIdentityForRepoCommand(
		authority,
		home,
		repoPath,
		"Agent 2 of BayeSartre",
		"agent+2@ouroboros-ai.com",
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"config --worktree user.name 'Agent 2 of BayeSartre'",
		"config --worktree user.email 'agent+2@ouroboros-ai.com'",
		"config --worktree core.sshCommand",
		executable,
		filepath.Join(home, ".ssh", "config"),
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("command missing %q:\n%s", want, cmd)
		}
	}

	if strings.Contains(cmd, "user.name '"+home+"'") || strings.Contains(cmd, "user.email '"+home+"'") {
		t.Fatalf("command used agent home as git identity:\n%s", cmd)
	}
	if strings.Contains(cmd, "ssh -F") {
		t.Fatalf("command used ambient SSH executable:\n%s", cmd)
	}
}

func TestWriteNativeSSHConfigUsesDefaultAndCustomIdentities(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeNativeSSHConfig(home, home, nil); err != nil {
		t.Fatalf("write default config: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(sshDir, "config"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "HostName github.com") || !strings.Contains(text, filepath.Join(home, ".ssh", "id_ed25519")) {
		t.Fatalf("default config missing github host/default key:\n%s", text)
	}

	if err := writeNativeSSHConfig(home, home, []string{"work_key"}); err != nil {
		t.Fatalf("write custom config: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(sshDir, "config"))
	if err != nil {
		t.Fatal(err)
	}
	text = string(data)
	if !strings.Contains(text, filepath.Join(home, ".ssh", "work_key")) || strings.Contains(text, filepath.Join(home, ".ssh", "id_ed25519")) {
		t.Fatalf("custom config did not use only custom key:\n%s", text)
	}
}
