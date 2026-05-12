package nativecmd

import (
	"os"
	"path/filepath"
	"testing"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/compose"
	"devkit/cli/devctl/internal/config"
	runtimebroker "devkit/cli/devctl/internal/runtime/broker"
)

func TestRepoChecksForUsesExplicitRepoCheckOnly(t *testing.T) {
	checks, err := repoChecksFor(&cmdregistry.Context{}, planArgs{repoCheck: "exit 7"})
	if err != nil {
		t.Fatalf("repoChecksFor: %v", err)
	}
	if len(checks) != 1 || checks[0].Name != "repo-check" || checks[0].Command != "exit 7" {
		t.Fatalf("checks = %#v", checks)
	}
}

func TestRepoChecksForUsesStructuredOverlayChecks(t *testing.T) {
	tmp := t.TempDir()
	overlay := filepath.Join(tmp, "overlays", "dev-all")
	if err := os.MkdirAll(overlay, 0o755); err != nil {
		t.Fatalf("mkdir overlay: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "devkit.yaml"), []byte(`
hooks:
  warm: echo warm
runtime:
  core_check: echo core
readiness:
  repo_checks:
    - name: typecheck
      command: npm test
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	ctx := &cmdregistry.Context{
		Project: "dev-all",
		Paths:   compose.Paths{OverlayPaths: []string{filepath.Join(tmp, "overlays")}},
	}
	checks, err := repoChecksFor(ctx, planArgs{})
	if err != nil {
		t.Fatalf("repoChecksFor: %v", err)
	}
	if len(checks) != 1 || checks[0].Name != "typecheck" || checks[0].Command != "npm test" {
		t.Fatalf("checks = %#v", checks)
	}
}

func TestRepoChecksForFallsBackToWarmAndCoreCheck(t *testing.T) {
	tmp := t.TempDir()
	overlay := filepath.Join(tmp, "overlays", "dev-all")
	if err := os.MkdirAll(overlay, 0o755); err != nil {
		t.Fatalf("mkdir overlay: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "devkit.yaml"), []byte(`
hooks:
  warm: echo warm
runtime:
  core_check: echo core
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	ctx := &cmdregistry.Context{
		Project: "dev-all",
		Paths:   compose.Paths{OverlayPaths: []string{filepath.Join(tmp, "overlays")}},
	}
	checks, err := repoChecksFor(ctx, planArgs{})
	if err != nil {
		t.Fatalf("repoChecksFor: %v", err)
	}
	if len(checks) != 2 || checks[0].Name != "warm-hook" || checks[1].Name != "core-check" {
		t.Fatalf("checks = %#v", checks)
	}
}

func TestParseLifecycleArgs(t *testing.T) {
	allowPulls := false
	parsed, err := parseLifecycleArgs(&cmdregistry.Context{Args: []string{
		"--repo", "devkit",
		"--count", "2",
		"--flake", ".#runtime-test-agent",
		"--broker-socket", "/tmp/broker.sock",
		"--broker-state-root", "/tmp/broker-state",
		"--allow-image", "postgres:15",
		"--no-allow-pulls",
		"--ready",
		"--skip-repo-checks",
		"--format", "json",
	}})
	if err != nil {
		t.Fatalf("parseLifecycleArgs: %v", err)
	}
	if parsed.repo != "devkit" || parsed.count != 2 || parsed.flake != ".#runtime-test-agent" {
		t.Fatalf("parsed = %#v", parsed)
	}
	if parsed.brokerSocket != "/tmp/broker.sock" || parsed.brokerStateRoot != "/tmp/broker-state" {
		t.Fatalf("broker paths = %#v", parsed)
	}
	if len(parsed.brokerAllowImage) != 1 || parsed.brokerAllowImage[0] != "postgres:15" {
		t.Fatalf("allow images = %#v", parsed.brokerAllowImage)
	}
	if parsed.brokerAllowPulls == nil || *parsed.brokerAllowPulls != allowPulls {
		t.Fatalf("allow pulls = %#v", parsed.brokerAllowPulls)
	}
	if !parsed.ready || !parsed.skipRepoChecks || parsed.format != "json" {
		t.Fatalf("parsed = %#v", parsed)
	}
}

func TestLifecycleStatusRequiresExplicitReadiness(t *testing.T) {
	if lifecycleStatusRunsReadiness(lifecycleArgs{}) {
		t.Fatalf("status should be lightweight by default")
	}
	if !lifecycleStatusRunsReadiness(lifecycleArgs{ready: true}) {
		t.Fatalf("status --ready should run readiness")
	}
	if lifecycleStatusRunsReadiness(lifecycleArgs{ready: true, skipReady: true}) {
		t.Fatalf("--skip-ready should override --ready")
	}
}

func TestLifecyclePlanOptionsUsesOverlayNativeRoots(t *testing.T) {
	ctx := &cmdregistry.Context{
		Project: "dev-all",
		Paths:   compose.Paths{Root: "/home/me/dev/devkit"},
	}
	opts := lifecyclePlanOptions(ctx, config.OverlayConfig{
		Native: config.Native{
			WorktreeRoot:          "../native-worktrees",
			StateRoot:             "../native-state",
			WorktreeContainerRoot: "/native-worktrees",
			StateContainerRoot:    "/native-state",
		},
	}, lifecycleArgs{}, "ouroboros-ide", runtimebroker.Config{Socket: "/tmp/broker.sock"})
	if opts.WorktreeRoot != "/home/me/dev/native-worktrees" {
		t.Fatalf("worktree root = %q", opts.WorktreeRoot)
	}
	if opts.StateRoot != "/home/me/dev/native-state" {
		t.Fatalf("state root = %q", opts.StateRoot)
	}
	if opts.WorktreeContainerRoot != "/native-worktrees" || opts.StateContainerRoot != "/native-state" {
		t.Fatalf("container roots = %q %q", opts.WorktreeContainerRoot, opts.StateContainerRoot)
	}
}

func TestLifecyclePlanOptionsCLIOverridesOverlayNativeRoots(t *testing.T) {
	ctx := &cmdregistry.Context{
		Project: "dev-all",
		Paths:   compose.Paths{Root: "/home/me/dev/devkit"},
	}
	opts := lifecyclePlanOptions(ctx, config.OverlayConfig{
		Native: config.Native{
			WorktreeRoot: "../native-worktrees",
			StateRoot:    "../native-state",
		},
	}, lifecycleArgs{
		worktreeRoot:            "/tmp/wt",
		agentStateRoot:          "/tmp/state",
		worktreeContainerRoot:   "/wt",
		agentStateContainerRoot: "/state",
	}, "ouroboros-ide", runtimebroker.Config{Socket: "/tmp/broker.sock"})
	if opts.WorktreeRoot != "/tmp/wt" || opts.StateRoot != "/tmp/state" {
		t.Fatalf("host roots = %q %q", opts.WorktreeRoot, opts.StateRoot)
	}
	if opts.WorktreeContainerRoot != "/wt" || opts.StateContainerRoot != "/state" {
		t.Fatalf("container roots = %q %q", opts.WorktreeContainerRoot, opts.StateContainerRoot)
	}
}

func TestRegisterTopLevelLifecycleAndNativeNamespace(t *testing.T) {
	reg := cmdregistry.New()
	Register(reg)
	for _, name := range []string{"native", "up", "down", "restart", "status", "logs", "scale", "ensure-ready", "exec", "attach"} {
		if _, ok := reg.Lookup(name); !ok {
			t.Fatalf("expected registered command %s", name)
		}
	}
}

func TestParseTopExecArgs(t *testing.T) {
	parsed, err := parseTopExecArgs(&cmdregistry.Context{Args: []string{"2", "--repo", "devkit", "--broker-socket", "/tmp/b.sock", "--", "git", "status"}}, false)
	if err != nil {
		t.Fatalf("parseTopExecArgs: %v", err)
	}
	if parsed.index != 2 || parsed.repo != "devkit" || parsed.brokerSocket != "/tmp/b.sock" {
		t.Fatalf("parsed = %#v", parsed)
	}
	if len(parsed.command) != 2 || parsed.command[0] != "git" || parsed.command[1] != "status" {
		t.Fatalf("command = %#v", parsed.command)
	}
}

func TestTailLines(t *testing.T) {
	got := tailLines("a\nb\nc", 2)
	if len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Fatalf("tail = %#v", got)
	}
}
