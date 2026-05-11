package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/config"
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

func TestCodexComposePinsDockerHost(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(cwd, "..", ".."))
	overridePath := filepath.Join(root, "overlays", "codex", "compose.override.yml")
	data, err := os.ReadFile(overridePath)
	if err != nil {
		t.Fatalf("read codex override: %v", err)
	}
	want := []byte("DOCKER_HOST=unix:///broker-run/postgres-broker.sock")
	if !bytes.Contains(data, want) {
		t.Fatalf("codex compose override missing %q", string(want))
	}
}

func TestGitIdentityForRepoCommandUsesIdentityValues(t *testing.T) {
	home := "/workspaces/dev/agent-worktrees/agent2/.devhome-agent2"
	repoPath := "/workspaces/dev/agent-worktrees/agent2/ouroboros-ide"
	cmd := gitIdentityForRepoCommand(
		home,
		repoPath,
		"Agent 2 of BayeSartre",
		"agent+2@ouroboros-ai.com",
	)

	for _, want := range []string{
		"config --worktree user.name 'Agent 2 of BayeSartre'",
		"config --worktree user.email 'agent+2@ouroboros-ai.com'",
		"config --worktree core.sshCommand 'ssh -F /workspaces/dev/agent-worktrees/agent2/.devhome-agent2/.ssh/config'",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("command missing %q:\n%s", want, cmd)
		}
	}

	if strings.Contains(cmd, "user.name '"+home+"'") || strings.Contains(cmd, "user.email '"+home+"'") {
		t.Fatalf("command used agent home as git identity:\n%s", cmd)
	}
}

func TestPlanScaleContainersShrinksHighestIndexes(t *testing.T) {
	containers := []composeServiceContainer{
		{Name: "devkit-ouro8-dev-agent-1", Index: 1},
		{Name: "devkit-ouro8-dev-agent-2", Index: 2},
		{Name: "devkit-ouro8-dev-agent-7", Index: 7},
		{Name: "devkit-ouro8-dev-agent-8", Index: 8},
		{Name: "devkit-ouro8-dev-agent-6", Index: 6},
	}

	remove, remaining, maxIndex := planScaleContainers(containers, 6)

	wantRemove := []string{"devkit-ouro8-dev-agent-7", "devkit-ouro8-dev-agent-8"}
	if strings.Join(remove, ",") != strings.Join(wantRemove, ",") {
		t.Fatalf("remove = %v, want %v", remove, wantRemove)
	}
	if remaining != 3 {
		t.Fatalf("remaining = %d, want 3", remaining)
	}
	if maxIndex != 8 {
		t.Fatalf("maxIndex = %d, want 8", maxIndex)
	}
	if needsComposeAfterScalePlan(remove, remaining, maxIndex, 6) {
		t.Fatalf("shrink with removals should not invoke compose up")
	}
}

func TestNeedsComposeAfterScalePlanAllowsScaleUp(t *testing.T) {
	if !needsComposeAfterScalePlan(nil, 4, 4, 6) {
		t.Fatalf("scale up should invoke compose up")
	}
}

func TestParseComposeServiceContainersIgnoresBadRows(t *testing.T) {
	raw := "devkit-ouro8-dev-agent-3\t3\nbad\tnope\n\t4\n"

	got := parseComposeServiceContainers(raw)

	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %#v", len(got), got)
	}
	if got[0].Name != "devkit-ouro8-dev-agent-3" || got[0].Index != 3 {
		t.Fatalf("unexpected parsed container: %#v", got[0])
	}
}
